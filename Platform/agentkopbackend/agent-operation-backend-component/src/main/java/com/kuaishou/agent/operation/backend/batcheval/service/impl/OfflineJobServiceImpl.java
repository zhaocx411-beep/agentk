package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import java.io.IOException;
import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.UUID;
import java.util.stream.Collectors;

import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.metadata.IPage;
import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.AgentConfigView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetExportResult;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.OfflineAgentOption;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.OfflineJobCellView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.OfflineJobView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.SubmitOfflineJobRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.PagedResult;
import com.kuaishou.agent.operation.backend.batcheval.mapper.BatchEvalMapper;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.Dataset;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;
import com.kuaishou.agent.operation.backend.batcheval.service.OfflineJobService;
import com.kuaishou.agent.operation.backend.batcheval.support.DatasetConversationFields;
import com.kuaishou.agent.operation.backend.batcheval.support.DatasetUsages;
import com.kuaishou.agent.operation.backend.batcheval.support.OfflineJobSessionMode;
import com.kuaishou.agent.operation.backend.batcheval.support.OfflineJobStatuses;
import com.kuaishou.agent.operation.backend.batcheval.support.PageRequests;
import com.kuaishou.agent.operation.backend.platform.entity.Agent;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.entity.RunEvent;
import com.kuaishou.agent.operation.backend.platform.service.AgentVersionService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.service.OfficialDemoService;

@Service
public class OfflineJobServiceImpl implements OfflineJobService {

    private static final String SOURCE_OFFICIAL = "official";
    private static final String SOURCE_MINE = "mine";
    private static final int TRACE_PAGE_SIZE_DEFAULT = 500;
    private static final int TRACE_PAGE_SIZE_MAX = 2000;

    private final BatchEvalMapper mapper;
    private final OfflineJobOrchestrator orchestrator;
    private final ObjectMapper objectMapper;
    private final OfficialDemoService officialDemoService;
    private final AgentVersionService agentVersionService;
    private final ChatPersistService chatPersistService;
    private final OfflineJobPurger purger;

    public OfflineJobServiceImpl(BatchEvalMapper mapper, OfflineJobOrchestrator orchestrator,
            ObjectMapper objectMapper, OfficialDemoService officialDemoService,
            AgentVersionService agentVersionService, ChatPersistService chatPersistService,
            OfflineJobPurger purger) {
        this.mapper = mapper;
        this.orchestrator = orchestrator;
        this.objectMapper = objectMapper;
        this.officialDemoService = officialDemoService;
        this.agentVersionService = agentVersionService;
        this.chatPersistService = chatPersistService;
        this.purger = purger;
    }

    @Override
    public OfflineJobView submit(SubmitOfflineJobRequest request, String creator) {
        if (request == null || request.getDatasetId() <= 0) {
            throw new IllegalArgumentException("datasetId 必须为正整数");
        }
        if (request.getAgentId() <= 0) {
            throw new IllegalArgumentException("agentId 必须为正整数");
        }
        Dataset dataset = mapper.findDataset(request.getDatasetId())
                .orElseThrow(() -> new IllegalArgumentException("数据集不存在: " + request.getDatasetId()));
        if (DatasetUsages.isTemplate(dataset.getUsage())) {
            throw new IllegalArgumentException("模版数据集不能提交离线跑数: " + dataset.getName());
        }
        List<DatasetTask> tasks = mapper.listAllTasks(dataset.getId());
        if (tasks.isEmpty()) {
            throw new IllegalArgumentException("数据集 tasks 不能为空");
        }

        BoundAgent bound = resolveBoundAgent(request.getAgentId(), creator);
        ProfileSnapshot snapshot = snapshotProfile(bound.version.getProfileYaml());
        if (snapshot.tools.isEmpty() && !StringUtils.hasText(snapshot.pipelineName)) {
            throw new IllegalArgumentException("所选 agent 的 profile 未包含 tools 或 pipelines，无法跑数");
        }

        OfflineJob job = new OfflineJob();
        job.setBizId("eval_" + UUID.randomUUID().toString().replace("-", ""));
        job.setDatasetId(dataset.getId());
        job.setAgentId(bound.agent.getId());
        job.setAgentVersionId(bound.version.getId());
        job.setAgentName(bound.agent.getName());
        job.setModel(snapshot.model);
        job.setSystemPrompt(snapshot.systemPrompt);
        job.setTools(new ArrayList<>(snapshot.tools));
        job.setPipelineName(snapshot.pipelineName);
        job.setPipelineRef(snapshot.pipelineRef);
        job.setProfileVersion(resolveProfileVersion(bound.version, snapshot.profileJson));
        job.setProfileYaml(snapshot.profileJson);
        OfflineJobSessionMode sessionMode = OfflineJobSessionMode.fromSubmit(
                request.getSessionMode(), request.getMultiRoundConversation());
        job.setSessionMode(sessionMode.getCode());
        job.setStatus("PENDING");
        job.setTotal(tasks.size());
        job.setCreator(creator);
        job.setCreatedAt(LocalDateTime.now());
        for (DatasetTask task : tasks) {
            OfflineJobCell cell = new OfflineJobCell();
            cell.setTaskKey(task.getTaskKey());
            cell.setPrompt(task.getPrompt());
            DatasetConversationFields.copyToCell(task, cell);
            cell.setStatus("QUEUED");
            job.getCells().add(cell);
        }
        mapper.insertJob(job);
        orchestrator.start(job.getBizId(), sessionMode);
        return toView(job);
    }

    @Override
    public OfflineJobView get(String bizId) {
        OfflineJob job = requireVisibleJob(bizId);
        orchestrator.reconcileIfNeeded(job);
        return toView(requireVisibleJob(bizId));
    }

    @Override
    public PagedResult<List<OfflineJobView>> list(int current, int pageSize) {
        int normalizedCurrent = PageRequests.normalizeCurrent(current);
        int normalizedPageSize = PageRequests.normalizePageSize(pageSize);
        List<OfflineJobView> records = mapper
                .listJobs(PageRequests.offset(normalizedCurrent, normalizedPageSize), normalizedPageSize)
                .stream()
                .map(job -> {
                    if ("RUNNING".equals(job.getStatus()) || "PENDING".equals(job.getStatus())) {
                        mapper.findJob(job.getBizId()).ifPresent(orchestrator::reconcileIfNeeded);
                        return toView(mapper.findJob(job.getBizId()).orElse(job));
                    }
                    return toView(job);
                })
                .collect(Collectors.toList());
        return PagedResult.of(records, mapper.countJobs(), normalizedCurrent, normalizedPageSize);
    }

    @Override
    public List<OfflineJobCellView> listCells(String bizId) {
        OfflineJob job = requireVisibleJob(bizId);
        orchestrator.reconcileIfNeeded(job);
        job = requireVisibleJob(bizId);
        return job.getCells().stream()
                .map(this::toCellView)
                .collect(Collectors.toList());
    }

    @Override
    public JsonNode getTrace(String bizId, String taskKey, int current, int pageSize) {
        OfflineJobCell cell = findCell(requireVisibleJob(bizId), taskKey);
        int page = PageRequests.normalizeCurrent(current);
        int size = normalizeTracePageSize(pageSize);
        ObjectNode root = objectMapper.createObjectNode();
        ObjectNode summary = readTraceSummary(bizId, taskKey);
        JsonNode legacyEvents = summary.path("events");
        boolean hasSummary = summary.size() > 0;
        root.setAll(summary);
        // 事件已单独落 aop_session_event，摘要里的 events 只有老数据才有
        root.remove("events");
        root.put("taskKey", cell.getTaskKey());
        root.put("sessionId", cell.getAgentkSessionId() == null ? "" : cell.getAgentkSessionId());

        IPage<RunEvent> events = chatPersistService
                .pageSessionEventsInWriteOrder(cell.getAgentkSessionId(), cell.getAgentkRunId(), page, size);
        if (events.getTotal() > 0) {
            ArrayNode array = root.putArray("events");
            for (RunEvent event : events.getRecords()) {
                array.add(toEventNode(event));
            }
            root.put("eventsFrom", "session_event");
            root.set("page", pageNode(page, size, events.getTotal()));
            return root;
        }
        if (legacyEvents.isArray()) {
            // B5 之前的快照把全量事件塞在 trace_json 里，历史任务仍要能看
            root.set("events", slice(legacyEvents, page, size));
            root.put("eventsFrom", "trace_snapshot");
            root.set("page", pageNode(page, size, legacyEvents.size()));
            return root;
        }
        if (!hasSummary) {
            throw new IllegalArgumentException("trace 尚未生成: " + taskKey);
        }
        root.putArray("events");
        root.put("eventsFrom", "session_event");
        root.set("page", pageNode(page, size, 0));
        return root;
    }

    private ObjectNode readTraceSummary(String bizId, String taskKey) {
        String raw = mapper.findCellTraceJson(bizId, taskKey).orElse(null);
        if (raw == null || raw.trim().isEmpty()) {
            return objectMapper.createObjectNode();
        }
        try {
            JsonNode parsed = objectMapper.readTree(raw);
            return parsed == null || !parsed.isObject()
                    ? objectMapper.createObjectNode() : (ObjectNode) parsed;
        } catch (IOException e) {
            throw new IllegalStateException("trace 快照损坏: " + taskKey, e);
        }
    }

    /**
     * 事件行还原成 agent-k 原始事件的形状，前端查看器不用改。
     * <p>
     * 顺序由查询侧的自增 id 保证；这里回带的 {@code seq} 只是当时拉到的页内序号，不可用于排序。
     */
    private ObjectNode toEventNode(RunEvent event) {
        ObjectNode node = objectMapper.createObjectNode();
        node.put("id", event.getId());
        putIfPresent(node, "eventId", event.getAgentkEventId());
        if (event.getSeq() != null) {
            node.put("seq", event.getSeq());
        }
        putIfPresent(node, "kind", event.getKind());
        putIfPresent(node, "roundId", event.getRoundId());
        putIfPresent(node, "executionId", event.getAgentkRunId());
        putIfPresent(node, "createdAt", event.getAgentkCreatedAt());
        node.set("data", readPayload(event.getPayloadJson()));
        return node;
    }

    private JsonNode readPayload(String payloadJson) {
        if (payloadJson == null || payloadJson.trim().isEmpty()) {
            return objectMapper.createObjectNode();
        }
        try {
            JsonNode parsed = objectMapper.readTree(payloadJson);
            return parsed == null ? objectMapper.createObjectNode() : parsed;
        } catch (IOException e) {
            // 单条 payload 坏了不该让整页 trace 打不开
            return objectMapper.createObjectNode().put("raw", payloadJson);
        }
    }

    private ArrayNode slice(JsonNode events, int page, int size) {
        ArrayNode array = objectMapper.createArrayNode();
        int from = (page - 1) * size;
        for (int i = from; i < Math.min(from + size, events.size()); i++) {
            array.add(events.get(i));
        }
        return array;
    }

    private ObjectNode pageNode(int current, int pageSize, long total) {
        ObjectNode node = objectMapper.createObjectNode();
        node.put("current", current);
        node.put("pageSize", pageSize);
        node.put("total", total);
        node.put("pages", pageSize <= 0 ? 0 : (total + pageSize - 1) / pageSize);
        return node;
    }

    private static void putIfPresent(ObjectNode node, String field, String value) {
        if (StringUtils.hasText(value)) {
            node.put(field, value);
        }
    }

    /** trace 事件页比列表页大：一次回合能出几十条事件，页太小前端要翻很多次。 */
    private static int normalizeTracePageSize(int pageSize) {
        if (pageSize <= 0) {
            return TRACE_PAGE_SIZE_DEFAULT;
        }
        return Math.min(pageSize, TRACE_PAGE_SIZE_MAX);
    }

    /**
     * 导出要带完整中间过程：按写入顺序把该用例在事件表里的行全部取出。
     * 表里没有时回退旧 {@code trace_json.events}。
     */
    private ExportEvents loadExportEvents(String bizId, OfflineJobCell cell) {
        ArrayNode array = objectMapper.createArrayNode();
        if (cell != null && StringUtils.hasText(cell.getAgentkSessionId())) {
            long page = 1;
            long fetched = 0;
            while (true) {
                IPage<RunEvent> batch = chatPersistService.pageSessionEventsInWriteOrder(
                        cell.getAgentkSessionId(), cell.getAgentkRunId(), page, TRACE_PAGE_SIZE_MAX);
                if (batch == null) {
                    break;
                }
                long total = batch.getTotal();
                List<RunEvent> records = batch.getRecords();
                if (records == null || records.isEmpty()) {
                    break;
                }
                for (RunEvent event : records) {
                    array.add(toEventNode(event));
                }
                fetched += records.size();
                if (fetched >= total) {
                    break;
                }
                page++;
            }
            if (array.size() > 0) {
                return new ExportEvents(array, "session_event");
            }
        }
        JsonNode legacy = readTraceSummary(bizId, cell == null ? "" : cell.getTaskKey()).path("events");
        if (legacy.isArray() && legacy.size() > 0) {
            return new ExportEvents(legacy, "trace_snapshot");
        }
        return new ExportEvents(array, "session_event");
    }

    private static final class ExportEvents {
        private final JsonNode events;
        private final String from;

        private ExportEvents(JsonNode events, String from) {
            this.events = events;
            this.from = from;
        }
    }

    @Override
    public DatasetExportResult export(String bizId) {
        OfflineJob job = requireVisibleJob(bizId);
        orchestrator.reconcileIfNeeded(job);
        job = requireVisibleJob(bizId);
        DatasetExportResult result = new DatasetExportResult();
        result.setFormat("json");
        Map<String, ExportEvents> eventsByTask = new LinkedHashMap<>();
        for (OfflineJobCell cell : job.getCells()) {
            eventsByTask.put(cell.getTaskKey(), loadExportEvents(bizId, cell));
        }
        result.setBody(OfflineJobResultExporter.writeJson(job, new OfflineJobResultExporter.CellEvents() {
            @Override
            public JsonNode events(OfflineJobCell cell) {
                ExportEvents loaded = eventsByTask.get(cell.getTaskKey());
                return loaded == null ? objectMapper.createArrayNode() : loaded.events;
            }

            @Override
            public String eventsFrom(OfflineJobCell cell) {
                ExportEvents loaded = eventsByTask.get(cell.getTaskKey());
                return loaded == null ? "session_event" : loaded.from;
            }
        }));
        result.setFilename(sanitizeFilenamePart(job.getBizId()) + "-results.json");
        result.setContentType("application/json;charset=UTF-8");
        return result;
    }

    @Override
    public AgentConfigView getAgentConfig(String userId) {
        AgentConfigView view = new AgentConfigView();
        LinkedHashMap<Long, OfflineAgentOption> byId = new LinkedHashMap<>();
        for (Agent agent : officialDemoService.listDefaultAgents()) {
            toOption(agent, SOURCE_OFFICIAL).ifPresent(option -> byId.put(option.getAgentId(), option));
        }
        if (StringUtils.hasText(userId)) {
            for (Agent agent : agentVersionService.listMine(userId.trim())) {
                if (!"active".equals(agent.getStatus())) {
                    continue;
                }
                // 官方已在列表中的不重复加入
                if (byId.containsKey(agent.getId())) {
                    continue;
                }
                toOption(agent, SOURCE_MINE).ifPresent(option -> byId.put(option.getAgentId(), option));
            }
        }
        view.setAgents(new ArrayList<>(byId.values()));
        view.setSessionModes(OfflineJobSessionMode.views());
        view.setDefaultSessionMode(OfflineJobSessionMode.PER_TASK.getCode());
        return view;
    }

    /**
     * 删除：校验状态 → 标记 {@code DELETING} → 清理。
     * <p>
     * 不再用一个方法级事务同步删完所有子表：一个 job 的事件行能到几万条大 JSON，
     * 长事务持锁、binlog 膨胀，接口容易超时。改成标记后按批删，事件少的小 job 当场清完，
     * 多的转后台（{@link OfflineJobPurger}）续跑，标记本身就是可续跑的进度。
     */
    @Override
    public void delete(String bizId) {
        OfflineJob job = requireJob(bizId);
        if (OfflineJobStatuses.isDeleting(job.getStatus())) {
            // 已经在清理队列里，重复点删除按幂等处理
            purger.schedule(bizId);
            return;
        }
        if (OfflineJobStatuses.RUNNING.equals(job.getStatus())) {
            throw new IllegalArgumentException("任务执行中，无法删除");
        }
        if (!OfflineJobStatuses.isFinished(job.getStatus())) {
            throw new IllegalArgumentException("任务尚未完成，无法删除");
        }
        List<String> sessionIds = OfflineJobPurger.agentkSessionIds(job);
        boolean inline = purger.canPurgeInline(sessionIds);
        if (!mapper.markJobStatus(bizId, OfflineJobStatuses.DELETING)) {
            throw new IllegalArgumentException("离线任务不存在: " + bizId);
        }
        if (inline) {
            purger.purge(bizId);
            return;
        }
        purger.schedule(bizId);
    }

    private Optional<OfflineAgentOption> toOption(Agent agent, String source) {
        if (agent == null || agent.getId() == null) {
            return Optional.empty();
        }
        Optional<AgentVersion> published = officialDemoService.findPublishedVersion(agent);
        if (!published.isPresent()) {
            return Optional.empty();
        }
        AgentVersion version = published.get();
        OfflineAgentOption option = new OfflineAgentOption();
        option.setAgentId(agent.getId());
        option.setName(agent.getName());
        option.setSource(source);
        option.setAgentVersionId(version.getId() == null ? 0L : version.getId());
        return Optional.of(option);
    }

    private BoundAgent resolveBoundAgent(long agentId, String creator) {
        Agent agent = agentVersionService.findAgent(agentId);
        if (agent == null) {
            throw new IllegalArgumentException("agent 不存在: " + agentId);
        }
        if (!"active".equals(agent.getStatus())) {
            throw new IllegalArgumentException("agent 未启用: " + agentId);
        }
        boolean official = Boolean.TRUE.equals(agent.getIsDefault());
        boolean mine = StringUtils.hasText(creator)
                && creator.trim().equals(agent.getOwnerUserId());
        if (!official && !mine) {
            throw new IllegalArgumentException("无权使用该 agent（仅官方或本人 agent）: " + agentId);
        }
        AgentVersion version = officialDemoService.findPublishedVersion(agent)
                .orElseThrow(() -> new IllegalArgumentException(
                        "agent 没有已发布版本，无法跑数: " + agentId));
        return new BoundAgent(agent, version);
    }

    private ProfileSnapshot snapshotProfile(String profileYaml) {
        if (!StringUtils.hasText(profileYaml)) {
            throw new IllegalArgumentException("agent 已发布版本缺少 profile");
        }
        String profileJson = profileYaml.trim();
        JsonNode root;
        try {
            root = objectMapper.readTree(profileJson);
        } catch (IOException e) {
            throw new IllegalArgumentException("agent profile 不是合法 JSON", e);
        }
        if (root == null || !root.isObject()) {
            throw new IllegalArgumentException("agent profile 必须是 JSON object");
        }
        // 规范化为紧凑 JSON 快照，避免库内空白差异
        try {
            profileJson = objectMapper.writeValueAsString(root);
        } catch (IOException e) {
            throw new IllegalStateException("序列化 profile 快照失败", e);
        }

        String model = root.path("defaultModel").asText("");
        String systemPrompt = root.path("system").asText("");
        List<String> tools = new ArrayList<>();
        JsonNode toolsNode = root.path("tools");
        if (toolsNode.isArray()) {
            for (JsonNode tool : toolsNode) {
                String name = tool.path("name").asText("").trim();
                if (StringUtils.hasText(name)) {
                    tools.add(name);
                }
            }
        }
        String pipelineName = null;
        String pipelineRef = null;
        JsonNode pipelines = root.path("pipelines");
        if (pipelines.isArray() && pipelines.size() > 0) {
            JsonNode first = pipelines.get(0);
            pipelineName = blankToNull(first.path("name").asText(null));
            pipelineRef = blankToNull(first.path("ref").asText(null));
        }
        return new ProfileSnapshot(profileJson, model, systemPrompt, tools, pipelineName, pipelineRef);
    }

    private static String resolveProfileVersion(AgentVersion version, String profileJson) {
        if (StringUtils.hasText(version.getContentHash())) {
            String hash = version.getContentHash().trim();
            return hash.length() <= 12 ? hash : hash.substring(0, 12);
        }
        return AgentVersionService.resolveProfileVersionLabel(version);
    }

    private static String sanitizeFilenamePart(String raw) {
        if (raw == null || raw.trim().isEmpty()) {
            return "offline-job";
        }
        return raw.trim().replaceAll("[^A-Za-z0-9._\\u4e00-\\u9fa5-]", "_");
    }

    private OfflineJob requireJob(String bizId) {
        if (bizId == null || bizId.trim().isEmpty()) {
            throw new IllegalArgumentException("bizId 不能为空");
        }
        return mapper.findJob(bizId)
                .orElseThrow(() -> new IllegalArgumentException("离线任务不存在: " + bizId));
    }

    /** 已标记删除的任务对读接口一律当作不存在，别让用户点了删除还查得到。 */
    private OfflineJob requireVisibleJob(String bizId) {
        OfflineJob job = requireJob(bizId);
        if (OfflineJobStatuses.isDeleting(job.getStatus())) {
            throw new IllegalArgumentException("离线任务不存在: " + bizId);
        }
        return job;
    }

    private static OfflineJobCell findCell(OfflineJob job, String taskKey) {
        for (OfflineJobCell cell : job.getCells()) {
            if (cell.getTaskKey().equals(taskKey)) {
                return cell;
            }
        }
        throw new IllegalArgumentException("离线任务单元不存在: " + taskKey);
    }

    private static OfflineJobView toView(OfflineJob job) {
        OfflineJobView view = new OfflineJobView();
        view.setBizId(job.getBizId());
        view.setDatasetId(job.getDatasetId());
        view.setAgentId(job.getAgentId());
        view.setAgentVersionId(job.getAgentVersionId());
        view.setAgentName(job.getAgentName());
        view.setModel(job.getModel());
        view.setSystemPrompt(job.getSystemPrompt());
        view.setTools(job.getTools() == null ? new ArrayList<>() : new ArrayList<>(job.getTools()));
        view.setPipeline(job.getPipelineName());
        view.setSessionMode(job.getSessionMode() == null
                ? OfflineJobSessionMode.PER_TASK.getCode() : job.getSessionMode());
        view.setStatus(job.getStatus());
        view.setTotal(job.getTotal());
        view.setErrors(job.getErrors());
        view.setCreator(job.getCreator());
        view.setCreatedAt(job.getCreatedAt());
        view.setStartedAt(job.getStartedAt());
        view.setFinishedAt(job.getFinishedAt());
        return view;
    }

    private OfflineJobCellView toCellView(OfflineJobCell cell) {
        OfflineJobCellView view = new OfflineJobCellView();
        view.setTaskKey(cell.getTaskKey());
        view.setPrompt(cell.getPrompt());
        view.setConversationKey(cell.getConversationKey());
        view.setTurnIndex(cell.getTurnIndex());
        view.setStatus(cell.getStatus());
        view.setAgentkSessionId(cell.getAgentkSessionId());
        view.setAgentkRunId(cell.getAgentkRunId());
        view.setPipelineExecId(cell.getPipelineExecId());
        view.setMetricsJson(cell.getMetricsJson());
        view.setOutputUris(readOutputUris(cell.getOutputUris()));
        view.setError(cell.getError());
        view.setStartedAt(cell.getStartedAt());
        view.setFinishedAt(cell.getFinishedAt());
        return view;
    }

    private List<String> readOutputUris(String json) {
        if (json == null || json.trim().isEmpty()) {
            return new ArrayList<>();
        }
        try {
            return objectMapper.readValue(json, new TypeReference<List<String>>() {
            });
        } catch (IOException e) {
            return new ArrayList<>();
        }
    }

    private static String blankToNull(String value) {
        return StringUtils.hasText(value) ? value.trim() : null;
    }

    private static final class BoundAgent {
        private final Agent agent;
        private final AgentVersion version;

        private BoundAgent(Agent agent, AgentVersion version) {
            this.agent = agent;
            this.version = version;
        }
    }

    private static final class ProfileSnapshot {
        private final String profileJson;
        private final String model;
        private final String systemPrompt;
        private final List<String> tools;
        private final String pipelineName;
        private final String pipelineRef;

        private ProfileSnapshot(String profileJson, String model, String systemPrompt,
                List<String> tools, String pipelineName, String pipelineRef) {
            this.profileJson = profileJson;
            this.model = model == null ? "" : model;
            this.systemPrompt = systemPrompt;
            this.tools = tools;
            this.pipelineName = pipelineName;
            this.pipelineRef = pipelineRef;
        }
    }
}
