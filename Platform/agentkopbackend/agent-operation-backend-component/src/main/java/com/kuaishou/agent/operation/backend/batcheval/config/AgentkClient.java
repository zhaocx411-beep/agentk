package com.kuaishou.agent.operation.backend.batcheval.config;

import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.concurrent.Semaphore;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.kuaishou.agent.operation.backend.agentk.AgentKProfileOutbound;
import com.kuaishou.agent.operation.backend.agentk.AgentKToolCapabilityLookup;
import com.kuaishou.agent.operation.backend.agentk.AgentkOutboundException;
import com.kuaishou.agent.operation.backend.agentk.AgentkOutboundGateway;
import com.kuaishou.agent.operation.backend.batcheval.support.LoopUserMessage;
import com.kuaishou.agent.operation.backend.batcheval.support.LoopUserMessage.Attachment;

/**
 * 离线评测 agent-k 客户端：出站经 {@link AgentkOutboundGateway} 复用在线 gRPC 客户端，
 * 本类只负责会话编排（建会话、发消息、轮询 events 交 sink 落库、轮询 pipeline 终态）。
 * <p>
 * 建会话前走 {@link AgentKProfileOutbound}，与在线出站契约一致，
 * 不改动在线 createSession 路径。
 */
@Component
public class AgentkClient {

    private static final Logger logger = LoggerFactory.getLogger(AgentkClient.class);

    private static final String PIPELINE_COMPLETED = "completed";
    private static final String PIPELINE_FAILED = "failed";

    /**
     * 与在线 {@code AgentKPaging.allEventsQuery()} 一致：{@code after=0&limit=2000&all=true}。
     * <p>
     * gRPC 适配层没有可用的数字 seq 游标，只能每次从账本头拉；不带 {@code all=true} 时会按列表缺省
     * 截成最早 500 条，工具调用和 {@code execution.finished} 都可能丢。component 不能依赖 api 模块，
     * 所以这里写死同一条 query，改那边时必须一起改。
     */
    static final String ALL_EVENTS_QUERY = "after=0&limit=2000&all=true";

    private final AgentkOutboundGateway gateway;
    private final AgentkProperties properties;
    private final ObjectMapper objectMapper;
    private final Semaphore outboundSlots;
    private final AgentKToolCapabilityLookup toolCapabilityLookup;

    public AgentkClient(AgentkOutboundGateway gateway, AgentkProperties properties, ObjectMapper objectMapper,
            AgentKToolCapabilityLookup toolCapabilityLookup) {
        this.gateway = gateway;
        this.properties = properties;
        this.objectMapper = objectMapper;
        this.toolCapabilityLookup = toolCapabilityLookup;
        this.outboundSlots = new Semaphore(Math.max(1, properties.getMaxOutboundInFlight()));
    }

    /**
     * @deprecated agent-k 公开 API 已移除 {@code GET /v1/profile}。
     * 工具发现请用 {@link #listCapabilities()}；模型名单走 KConf / 本地兜底。
     */
    @Deprecated
    public JsonNode getProfile() {
        throw new AgentkClientException(
                "GET /v1/profile 已下线；请使用 GET /v1/capabilities 或本地 batch-eval 兜底");
    }

    /**
     * 拉取 Worker 已上报的能力目录（含 tool / pipeline 声明）。
     * <p>
     * 对应 agent-k {@code GET /v1/capabilities}。注意：这是<strong>动态能力</strong>目录，
     * 不含进程内第一方工具（kling_* / job_status 等）；后者由中后台 mock 兜底。
     */
    public JsonNode listCapabilities() {
        return request("GET", "/capabilities", null, null);
    }

    /** 建会话 + 跑一轮 Agent Loop，事件不落库；只勾了 tools 的离线用例走这条。 */
    public RunResult execute(String title, String profileJson, String prompt) {
        return execute(title, profileJson, prompt, EventSink.NOOP);
    }

    /** 同上，事件交给 {@code sink} 落库。 */
    public RunResult execute(String title, String profileJson, String prompt, EventSink sink) {
        String sessionId = createSession(title, profileJson);
        return runLoop(sessionId, prompt, sink);
    }

    /** 只建会话，不触发任何执行；双通道场景由调用方分别发 messages / pipeline-runs。 */
    public String createSession(String title, String profileJson) {
        JsonNode profileNode;
        try {
            // 与在线 AgentKProxyController 出站一致：规范化后再 POST，避免 agent 快照里的
            // capability 等字段被 staging agent-k DisallowUnknownFields 拒绝。
            profileNode = objectMapper.valueToTree(
                    AgentKProfileOutbound.toAgentkProfileJson(profileJson, objectMapper, toolCapabilityLookup));
        } catch (IllegalArgumentException e) {
            throw new AgentkClientException("profile 规范化失败: " + e.getMessage(), e);
        } catch (RuntimeException e) {
            throw new AgentkClientException("profile JSON 非法或无法出站规范化", e);
        }
        ObjectNode sessionBody = objectMapper.createObjectNode();
        sessionBody.put("title", title);
        sessionBody.set("profile", profileNode);
        JsonNode session = request("POST", "/sessions", null, sessionBody);
        return requiredText(session, "id");
    }

    /** 发一条 user 消息并轮询到 execution 终态；事件不落库。 */
    public RunResult runLoop(String sessionId, String prompt) {
        return runLoop(sessionId, prompt, EventSink.NOOP);
    }

    /**
     * 发一条 user 消息并轮询到<strong>本轮</strong> execution 终态。
     * 同一 session 上多轮时账本里会留着上一轮的 {@code execution.finished}，必须按本次
     * {@code executionId} 过滤，否则会立刻当成结束，第二轮 event / asset 都落不下来。
     * 事件交给 {@code sink}（写入侧 upsert 去重）；本类只对未见过的本轮事件做终态判断。
     */
    public RunResult runLoop(String sessionId, String prompt, EventSink sink) {
        return runLoop(sessionId, new LoopUserMessage(prompt, null), sink);
    }

    public RunResult runLoop(String sessionId, LoopUserMessage message, EventSink sink) {
        EventSink eventsSink = sink == null ? EventSink.NOOP : sink;
        boolean useAttachments = message != null && message.hasAttachments();
        logger.info("agent-k POST messages: session={} useAttachments={} attachmentCount={}",
                sessionId, useAttachments, useAttachments ? message.getAttachments().size() : 0);
        JsonNode accepted = postWhenSessionIdle("/sessions/" + sessionId + "/messages",
                messageBody(message));

        String currentExecutionId = firstNonBlank(accepted.path("executionId").asText(null));
        ArrayNode acceptedEvents = userInputEvents(accepted.path("events"));
        long cursor = maxSeq(acceptedEvents);
        Set<String> seenEventIds = new HashSet<>();
        rememberEventIds(acceptedEvents, seenEventIds);
        RunStats stats = new RunStats();
        acceptedEvents.forEach(stats::observe);
        emit(eventsSink, acceptedEvents);

        PollResult pollResult = waitUntilIdle(
                sessionId, cursor, seenEventIds, stats, eventsSink, currentExecutionId);
        // executionId 是本轮执行的标识，与在线侧资产落库口径一致；拿不到就留空，不退回 roundId
        // 顶替——两者语义不同，混存会让按 execution 聚合的口径失真。
        String executionId = firstNonBlank(currentExecutionId, pollResult.lastExecutionId);
        return new RunResult(sessionId, executionId, pollResult.lastRoundId,
                stats.toSummary(objectMapper, sessionId, executionId, pollResult.failureError));
    }

    private JsonNode messageBody(LoopUserMessage message) {
        ObjectNode body = objectMapper.createObjectNode();
        body.put("text", message == null || message.getText() == null ? "" : message.getText());
        if (message == null || !message.hasAttachments()) {
            return body;
        }
        ArrayNode items = body.putArray("attachments");
        for (Attachment attachment : message.getAttachments()) {
            if (attachment == null || !StringUtils.hasText(attachment.getUri())) {
                continue;
            }
            ObjectNode node = items.addObject();
            node.put("uri", attachment.getUri().trim());
            if (StringUtils.hasText(attachment.getType())) {
                node.put("type", attachment.getType().trim());
            }
            if (StringUtils.hasText(attachment.getMimeType())) {
                node.put("mimeType", attachment.getMimeType().trim());
            }
        }
        return body;
    }

    /**
     * 触发一次 pipeline 执行并轮询到终态。
     * <p>
     * agent-k 对 {@code POST /pipeline-runs} 返回 202 + running 快照，执行体异步推进；
     * 这里按 {@code pipelinePollIntervalMillis} 轮询单次执行快照，直到 completed / failed。
     * args 不合 inputSchema 时 agent-k 直接 400，调用方应先本地校验以给出可读提示。
     */
    public PipelineRunResult runPipeline(String sessionId, String pipeline, JsonNode args) {
        ObjectNode body = objectMapper.createObjectNode();
        body.put("pipeline", pipeline);
        body.set("args", args == null ? objectMapper.createObjectNode() : args);
        JsonNode started = postWhenSessionIdle("/sessions/" + sessionId + "/pipeline-runs", body);
        String execId = requiredText(started, "id");

        Instant deadline = Instant.now().plus(Duration.ofSeconds(properties.getRunTimeoutSeconds()));
        JsonNode snapshot = started;
        while (true) {
            String status = snapshot.path("status").asText("");
            if (PIPELINE_COMPLETED.equals(status)) {
                return new PipelineRunResult(execId, pipeline, status, snapshot, null);
            }
            if (PIPELINE_FAILED.equals(status)) {
                String error = firstNonBlank(snapshot.path("error").asText(null),
                        "pipeline " + pipeline + " failed");
                return new PipelineRunResult(execId, pipeline, status, snapshot, error);
            }
            if (Instant.now().isAfter(deadline)) {
                throw new AgentkClientException("等待 agent-k pipeline 超时: " + pipeline
                        + " exec=" + execId);
            }
            sleep(Math.max(1, properties.getPipelinePollIntervalMillis()));
            snapshot = request("GET", "/sessions/" + sessionId + "/pipeline-runs/" + execId, null, null);
        }
    }

    /**
     * 轮询到<strong>本轮</strong> execution 结束。与在线 {@code inferTurnStatus} 对齐：
     * 终态只认 {@code execution.finished}；{@code round.completed}（含 {@code finishReason=tool_calls}）
     * 以及 {@code tool.exec_failed} / {@code error} / {@code job.failed} 都只是过程事件。
     * {@code currentExecutionId} 非空时忽略其它 execution 的终态与产物事件。
     */
    private PollResult waitUntilIdle(String sessionId, long cursor, Set<String> seenEventIds,
            RunStats stats, EventSink sink, String currentExecutionId) {
        Instant deadline = Instant.now().plus(Duration.ofSeconds(properties.getRunTimeoutSeconds()));

        while (Instant.now().isBefore(deadline)) {
            // 与在线 ChatRunFollowUpService.syncOnce 同一条 query。
            // after=0：gRPC 适配层不会把数字 seq 当成 page token；增量靠 eventId 去重。
            // all=true：不要用列表缺省 500 截断账本。
            JsonNode page = request("GET", "/sessions/" + sessionId + "/events", ALL_EVENTS_QUERY, null);
            JsonNode events = filterByExecution(page.path("events"), currentExecutionId);
            emit(sink, events);
            List<JsonNode> incoming = new ArrayList<>();
            if (events.isArray()) {
                for (JsonNode event : events) {
                    if (!alreadySeen(event, cursor, seenEventIds)) {
                        incoming.add(event);
                    }
                }
            }
            incoming.sort(EVENT_CHRONOLOGICAL);
            for (JsonNode event : incoming) {
                cursor = Math.max(cursor, event.path("seq").asLong());
                stats.observe(event);
            }
            if (stats.executionFinished) {
                break;
            }
            sleep(Math.max(1, properties.getPollIntervalMillis()));
        }
        if (!stats.executionFinished) {
            throw new AgentkClientException("等待 agent-k session 超时: " + sessionId
                    + " (pendingTools=" + stats.pendingToolCalls.size()
                    + ", pendingJobs=" + stats.pendingJobs.size()
                    + ", runningRounds=" + stats.runningRounds
                    + ", executionFinished=" + stats.executionFinished + ")");
        }
        return new PollResult(stats.failureError, stats.lastRoundId, stats.lastExecutionId);
    }

    /**
     * 多轮共用 session 时 ListEvents 会带回整本账。只认本轮 execution：
     * 有 executionId 的必须匹配；无 executionId 的过程事件仍收下（user.input 等）；
     * 无 executionId 的 {@code execution.finished} 在已有本轮 id 时丢掉，避免误把上一轮当结束。
     */
    static boolean belongsToExecution(JsonNode event, String executionId) {
        if (event == null || event.isMissingNode() || !StringUtils.hasText(executionId)) {
            return true;
        }
        String eventExec = event.path("executionId").asText("");
        if (!StringUtils.hasText(eventExec)) {
            return !"execution.finished".equals(event.path("kind").asText());
        }
        return executionId.equals(eventExec);
    }

    private JsonNode filterByExecution(JsonNode events, String executionId) {
        if (!StringUtils.hasText(executionId) || events == null || !events.isArray()) {
            return events;
        }
        ArrayNode filtered = objectMapper.createArrayNode();
        for (JsonNode event : events) {
            if (belongsToExecution(event, executionId)) {
                filtered.add(event);
            }
        }
        return filtered;
    }

    private static void emit(EventSink sink, JsonNode events) {
        if (sink == null || events == null || !events.isArray() || events.size() == 0) {
            return;
        }
        try {
            sink.accept(events);
        } catch (RuntimeException e) {
            logger.warn("离线事件 sink 失败 size={}", events.size(), e);
        }
    }

    /**
     * ListEvents 新事件在前，seq 是适配层按页内顺序编的假序号。有 {@code createdAt} 时按时间正序，
     * 避免先看到后发生的事件再看到先发生的事件。
     */
    private static final Comparator<JsonNode> EVENT_CHRONOLOGICAL = (left, right) -> {
        String createdLeft = left.path("createdAt").asText("");
        String createdRight = right.path("createdAt").asText("");
        if (!createdLeft.isEmpty() && !createdRight.isEmpty()) {
            int byTime = createdLeft.compareTo(createdRight);
            if (byTime != 0) {
                return byTime;
            }
        }
        return Long.compare(left.path("seq").asLong(), right.path("seq").asLong());
    };

    /**
     * 边收事件边聚合摘要。用例成败只跟 {@code execution.finished.data.status}：
     * FAILED / CANCELLED 才失败，其余过程事件（工具失败、error、job.failed）只记账，不封装成用例失败。
     */
    public static final class RunStats {
        private int eventCount;
        private int seqFrom = Integer.MAX_VALUE;
        private int seqTo;
        private String createdAt = "";
        private String finishedAt = "";
        private int steps;
        private int inputTokens;
        private int outputTokens;
        private int toolCalls;
        private int toolErrors;
        private int runningRounds;
        private final Set<String> pendingJobs = new HashSet<>();
        private final Set<String> pendingToolCalls = new HashSet<>();
        private boolean executionFinished;
        private String failureError;
        private String lastRoundId = "";
        private String lastExecutionId = "";

        public void observe(JsonNode event) {
            if (event == null || event.isMissingNode()) {
                return;
            }
            eventCount++;
            long seq = event.path("seq").asLong();
            if (seq > 0) {
                seqFrom = Math.min(seqFrom, (int) Math.min(seq, Integer.MAX_VALUE));
                seqTo = Math.max(seqTo, (int) Math.min(seq, Integer.MAX_VALUE));
            }
            String at = event.path("createdAt").asText("");
            if (!at.isEmpty()) {
                if (createdAt.isEmpty()) {
                    createdAt = at;
                }
                finishedAt = at;
            }
            String roundId = event.path("roundId").asText();
            if (!roundId.isEmpty()) {
                lastRoundId = roundId;
            }
            String executionId = event.path("executionId").asText();
            if (!executionId.isEmpty()) {
                lastExecutionId = executionId;
            }
            String kind = event.path("kind").asText();
            JsonNode payload = eventPayload(event);
            switch (kind) {
                case "round.started":
                    runningRounds++;
                    break;
                case "round.completed":
                    runningRounds = Math.max(0, runningRounds - 1);
                    steps++;
                    JsonNode usage = payload.path("usage");
                    inputTokens += usage.path("inputTokens").asInt();
                    outputTokens += usage.path("outputTokens").asInt();
                    break;
                case "execution.finished": {
                    executionFinished = true;
                    String execStatus = payload.path("status").asText("");
                    if (execStatus.contains("FAILED") || execStatus.contains("CANCELLED")) {
                        failureError = firstNonBlank(
                                payload.path("error").asText(null),
                                payload.path("message").asText(null),
                                "agent-k execution finished");
                    }
                    break;
                }
                case "assistant.tool_call":
                    toolCalls++;
                    rememberPendingTool(payload);
                    break;
                case "tool.exec_started":
                    rememberPendingTool(payload);
                    break;
                case "tool.exec_succeeded":
                case "tool.executed": {
                    String toolCallId = payload.path("toolCallId").asText();
                    if (StringUtils.hasText(toolCallId)) {
                        pendingToolCalls.remove(toolCallId);
                    }
                    break;
                }
                case "tool.exec_failed": {
                    String toolCallId = payload.path("toolCallId").asText();
                    if (StringUtils.hasText(toolCallId)) {
                        pendingToolCalls.remove(toolCallId);
                    }
                    toolErrors++;
                    break;
                }
                case "job.submitted": {
                    String jobId = payload.path("jobId").asText();
                    if (StringUtils.hasText(jobId)) {
                        pendingJobs.add(jobId);
                    }
                    break;
                }
                case "job.completed":
                case "job.failed": {
                    String jobId = payload.path("jobId").asText();
                    if (StringUtils.hasText(jobId)) {
                        pendingJobs.remove(jobId);
                    }
                    break;
                }
                default:
                    break;
            }
        }

        private void rememberPendingTool(JsonNode payload) {
            String toolCallId = payload.path("toolCallId").asText();
            if (StringUtils.hasText(toolCallId)) {
                pendingToolCalls.add(toolCallId);
            }
        }

        public String resolveFailure(String pollFailure) {
            return StringUtils.hasText(pollFailure) ? pollFailure : failureError;
        }

        private ObjectNode toSummary(ObjectMapper mapper, String sessionId, String executionId,
                String pollFailure) {
            String failure = resolveFailure(pollFailure);
            ObjectNode summary = mapper.createObjectNode();
            summary.put("sessionId", sessionId);
            summary.put("status", failure == null ? "completed" : "failed");
            if (failure != null) {
                summary.put("error", failure);
            }
            summary.put("eventCount", eventCount);
            if (eventCount > 0 && seqFrom != Integer.MAX_VALUE) {
                summary.put("seqFrom", seqFrom);
                summary.put("seqTo", seqTo);
            }
            if (!createdAt.isEmpty()) {
                summary.put("createdAt", createdAt);
            }
            if (!finishedAt.isEmpty()) {
                summary.put("finishedAt", finishedAt);
            }
            if (!lastRoundId.isEmpty()) {
                summary.put("lastRoundId", lastRoundId);
            }
            if (StringUtils.hasText(executionId)) {
                summary.put("executionId", executionId);
            }
            ObjectNode metrics = summary.putObject("metrics");
            metrics.put("steps", steps);
            metrics.put("inputTokens", inputTokens);
            metrics.put("outputTokens", outputTokens);
            metrics.put("toolCalls", toolCalls);
            metrics.put("toolErrors", toolErrors);
            return summary;
        }
    }

    private static String firstNonBlank(String... values) {
        if (values == null) {
            return null;
        }
        for (String value : values) {
            if (StringUtils.hasText(value)) {
                return value;
            }
        }
        return null;
    }

    private static JsonNode eventPayload(JsonNode event) {
        if (event.has("data")) {
            return event.path("data");
        }
        return event.path("payload");
    }

    private static long maxSeq(JsonNode events) {
        long cursor = 0;
        if (events.isArray()) {
            for (JsonNode event : events) {
                cursor = Math.max(cursor, event.path("seq").asLong());
            }
        }
        return cursor;
    }

    private static void rememberEventIds(JsonNode events, Set<String> seen) {
        if (events == null || !events.isArray()) {
            return;
        }
        for (JsonNode event : events) {
            String eventId = event.path("eventId").asText("");
            if (StringUtils.hasText(eventId)) {
                seen.add(eventId);
            }
        }
    }

    /** 有 eventId 用它去重；没有则退回旧的 seq 水位（单测桩常见）。 */
    private static boolean alreadySeen(JsonNode event, long cursor, Set<String> seenEventIds) {
        String eventId = event.path("eventId").asText("");
        if (StringUtils.hasText(eventId)) {
            return !seenEventIds.add(eventId);
        }
        long seq = event.path("seq").asLong();
        return seq > 0 && seq <= cursor;
    }

    /**
     * 发消息回执里的 events 由出站侧按「最近若干条 + 按 kind 匹配」拼出，可能夹带非本次输入的事件。
     * 只认 user.input 作为轮询起点，其余交给 events 轮询补齐，避免游标越过尚未消费的事件。
     */
    private ArrayNode userInputEvents(JsonNode events) {
        ArrayNode filtered = objectMapper.createArrayNode();
        if (!events.isArray()) {
            return filtered;
        }
        for (JsonNode event : events) {
            if ("user.input".equals(event.path("kind").asText())) {
                filtered.add(event);
            }
        }
        return filtered;
    }

    /**
     * 同一 session 的 Temporal workflow id 是 {@code session/{id}}，上一轮 {@code execution.finished}
     * 落账后 workflow 可能还没关干净，立刻再 StartExecution 会 FAILED_PRECONDITION。
     * 这种拒绝没有真正开跑，可以安全重试；502 仍不能重试（可能已经开跑）。
     */
    private JsonNode postWhenSessionIdle(String path, JsonNode body) {
        Instant deadline = Instant.now().plus(Duration.ofSeconds(Math.max(1, properties.getRunTimeoutSeconds())));
        AgentkClientException last = null;
        int attempt = 0;
        while (!Instant.now().isAfter(deadline)) {
            attempt++;
            acquireOutboundSlot();
            try {
                return gateway.exchange("POST", path, null, body);
            } catch (AgentkOutboundException e) {
                if (!isActiveExecutionConflict(e)) {
                    throw new AgentkClientException(e.getMessage(), e);
                }
                last = new AgentkClientException(e.getMessage(), e);
                logger.info("session 仍有活跃 execution，等待后重试 POST {} attempt={}", path, attempt);
            } finally {
                outboundSlots.release();
            }
            long delay = Math.min(2000L, 200L * (1L << Math.min(attempt - 1, 4)));
            if (Instant.now().plusMillis(delay).isAfter(deadline)) {
                break;
            }
            sleep(delay);
        }
        throw last == null ? new AgentkClientException("agent-k 请求失败") : last;
    }

    static boolean isActiveExecutionConflict(Throwable error) {
        for (Throwable cursor = error; cursor != null; cursor = cursor.getCause()) {
            String message = cursor.getMessage();
            if (message != null && message.contains("already has an active execution")) {
                return true;
            }
        }
        return false;
    }

    private JsonNode request(String method, String path, String query, JsonNode body) {
        // GET 幂等，出站超时可以重试。发消息 / 开 pipeline 会真的再起一轮执行，不能重试。
        // 建会话例外：502/UNAVAILABLE 几乎总是连接没打到 agent-k（冷连接、实例抖动），
        // 不重试就会让 job 里第一条用例必挂；万一响应丢失导致多建一个空会话，比整条用例失败便宜。
        int maxAttempts = retryableRequest(method, path) ? Math.max(1, properties.getMaxRetries() + 1) : 1;
        AgentkClientException last = null;
        for (int attempt = 1; attempt <= maxAttempts; attempt++) {
            acquireOutboundSlot();
            try {
                return gateway.exchange(method, path, query, body);
            } catch (AgentkOutboundException e) {
                if (!e.isRetryable()) {
                    throw new AgentkClientException(e.getMessage(), e);
                }
                last = new AgentkClientException(e.getMessage(), e);
                if (attempt == maxAttempts) {
                    break;
                }
            } finally {
                outboundSlots.release();
            }
            sleep(Math.min(2000L, 200L * (1L << (attempt - 1))));
        }
        throw last == null ? new AgentkClientException("agent-k 请求失败") : last;
    }

    private static boolean retryableRequest(String method, String path) {
        if ("GET".equals(method)) {
            return true;
        }
        return "POST".equals(method) && "/sessions".equals(path);
    }

    /**
     * 先拿到名额再发 RPC：OkHttp 排队会计入 unary 10s 超时，必须排在客户端外面。
     */
    private void acquireOutboundSlot() {
        try {
            outboundSlots.acquire();
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new AgentkClientException("等待 agent-k 出站名额时被中断", e);
        }
    }

    private static String requiredText(JsonNode node, String field) {
        String value = node.path(field).asText();
        if (value.isEmpty()) {
            throw new AgentkClientException("agent-k 响应缺少字段: " + field);
        }
        return value;
    }

    private static void sleep(long millis) {
        try {
            Thread.sleep(millis);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new AgentkClientException("等待 agent-k 时任务被中断", e);
        }
    }

    /**
     * 离线 Loop 事件出口。调用方（{@code OfflineLoopEventSink}）负责落库；
     * 本客户端每轮把当前 ListEvents 整页交出去，与在线 follow-up 重放全账本一致。
     */
    @FunctionalInterface
    public interface EventSink {
        EventSink NOOP = events -> {
        };

        void accept(JsonNode events);
    }

    public static final class RunResult {
        private final String sessionId;
        private final String executionId;
        private final String lastRoundId;
        private final JsonNode summary;

        private RunResult(String sessionId, String executionId, String lastRoundId, JsonNode summary) {
            this.sessionId = sessionId;
            this.executionId = executionId;
            this.lastRoundId = lastRoundId;
            this.summary = summary;
        }

        public String getSessionId() {
            return sessionId;
        }

        /** agent-k execution id（本轮执行），与在线资产落库口径一致。 */
        public String getExecutionId() {
            return executionId;
        }

        /** 最后一轮 roundId。 */
        public String getLastRoundId() {
            return lastRoundId;
        }

        /** 结算摘要，不含事件本体；事件在 {@code aop_session_event}。 */
        public JsonNode getSummary() {
            return summary;
        }
    }

    /** 一次 pipeline 执行的终态快照。 */
    public static final class PipelineRunResult {
        private final String execId;
        private final String pipeline;
        private final String status;
        private final JsonNode snapshot;
        private final String error;

        private PipelineRunResult(String execId, String pipeline, String status, JsonNode snapshot,
                String error) {
            this.execId = execId;
            this.pipeline = pipeline;
            this.status = status;
            this.snapshot = snapshot;
            this.error = error;
        }

        public String getExecId() {
            return execId;
        }

        public String getPipeline() {
            return pipeline;
        }

        /** completed / failed。 */
        public String getStatus() {
            return status;
        }

        /** agent-k PipelineRun 快照，含 args / output。 */
        public JsonNode getSnapshot() {
            return snapshot;
        }

        public JsonNode getOutput() {
            return snapshot == null ? null : snapshot.path("output");
        }

        public boolean isSucceeded() {
            return PIPELINE_COMPLETED.equals(status);
        }

        /** 仅失败时非空。 */
        public String getError() {
            return error;
        }
    }

    private static final class PollResult {
        private final String failureError;
        private final String lastRoundId;
        private final String lastExecutionId;

        private PollResult(String failureError, String lastRoundId, String lastExecutionId) {
            this.failureError = failureError;
            this.lastRoundId = lastRoundId == null ? "" : lastRoundId;
            this.lastExecutionId = lastExecutionId == null ? "" : lastExecutionId;
        }
    }

    public static class AgentkClientException extends RuntimeException {
        public AgentkClientException(String message) {
            super(message);
        }

        public AgentkClientException(String message, Throwable cause) {
            super(message, cause);
        }
    }
}
