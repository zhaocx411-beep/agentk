package com.kuaishou.agent.operation.backend.agentk;

import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.atomic.AtomicInteger;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMediaArtifactDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse;
import com.kuaishou.agent.operation.backend.platform.service.BlobStoreOfficeUrlService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.SyncedAsset;

/**
 * 闭环回写：发消息后异步轮询 agent-k 会话 events，完整写入平台事件账本。
 * {@code aop_session_message} 只记录用户主动提交，不再从事件流投影 assistant/user 消息。
 * <p>
 * 合并两路逻辑：保留平台事件落库（persistRunEvent / updateStreamCursor），
 * 出站改为新 API 的 {@code /sessions/{id}/events}（不再调用已删除的 {@code /runs/*}、{@code /items}）。
 */
@Component
public class ChatRunFollowUpService {

    private static final Logger LOGGER = LoggerFactory.getLogger(ChatRunFollowUpService.class);

    private static final long POLL_INTERVAL_MS = 1000L;
    private static final long DEFAULT_TIMEOUT_MS = 180_000L;

    private final AgentKClient agentKClient;
    private final ChatPersistService chatPersistService;
    private final AgentKProperties properties;
    private final BlobStoreOfficeUrlService officeUrlService;
    private final ExecutorService executor;
    private final Set<String> inflight = ConcurrentHashMap.newKeySet();

    public ChatRunFollowUpService(AgentKClient agentKClient, ChatPersistService chatPersistService,
            AgentKProperties properties, BlobStoreOfficeUrlService officeUrlService) {
        this.agentKClient = agentKClient;
        this.chatPersistService = chatPersistService;
        this.properties = properties;
        this.officeUrlService = officeUrlService;
        this.executor = Executors.newCachedThreadPool(new ThreadFactory() {
            private final AtomicInteger seq = new AtomicInteger();

            @Override
            public Thread newThread(Runnable r) {
                Thread t = new Thread(r, "chat-run-followup-" + seq.incrementAndGet());
                t.setDaemon(true);
                return t;
            }
        });
    }

    /** 发消息成功后调度：轮询 session events 直到终态并 sync messages。 */
    public void scheduleAfterPostMessage(String agentkSessionId, String agentkRunId,
            AgentKOutboundIdentity identity) {
        if (!StringUtils.hasText(agentkSessionId) || !StringUtils.hasText(agentkRunId) || identity == null) {
            return;
        }
        if (!inflight.add(agentkRunId)) {
            LOGGER.debug("follow-up already running runId={}", agentkRunId);
            return;
        }
        executor.execute(() -> {
            try {
                pollUntilTerminal(agentkSessionId, agentkRunId, identity);
            } finally {
                inflight.remove(agentkRunId);
            }
        });
    }

    /** SSE / events 拉取后立即同步一次。 */
    public void syncAfterStream(String agentkSessionId, String agentkRunId, AgentKOutboundIdentity identity) {
        if (!StringUtils.hasText(agentkSessionId) || identity == null) {
            return;
        }
        try {
            String runId = agentkRunId;
            if (!StringUtils.hasText(runId)) {
                runId = chatPersistService.findByAgentkSessionId(agentkSessionId)
                        .map(s -> s.getLastAgentkRunId())
                        .orElse(null);
            }
            syncOnce(agentkSessionId, runId, identity);
        } catch (Exception e) {
            LOGGER.warn("syncAfterStream failed sessionId={}", agentkSessionId, e);
        }
    }

    private void pollUntilTerminal(String agentkSessionId, String agentkRunId, AgentKOutboundIdentity identity) {
        long deadline = System.currentTimeMillis() + followUpTimeoutMs();
        while (System.currentTimeMillis() < deadline) {
            try {
                SyncSnapshot snap = syncOnce(agentkSessionId, agentkRunId, identity);
                if (snap != null && isTerminal(snap.runStatus)) {
                    LOGGER.info("follow-up done sessionId={} runId={} status={}",
                            agentkSessionId, agentkRunId, snap.runStatus);
                    return;
                }
            } catch (Exception e) {
                LOGGER.warn("follow-up poll failed sessionId={} runId={}", agentkSessionId, agentkRunId, e);
            }
            try {
                Thread.sleep(POLL_INTERVAL_MS);
            } catch (InterruptedException ie) {
                Thread.currentThread().interrupt();
                return;
            }
        }
        LOGGER.warn("follow-up timeout sessionId={} runId={}", agentkSessionId, agentkRunId);
        try {
            syncOnce(agentkSessionId, agentkRunId, identity);
        } catch (Exception e) {
            LOGGER.warn("follow-up final sync failed sessionId={} runId={}", agentkSessionId, agentkRunId, e);
        }
    }

    private SyncSnapshot syncOnce(String agentkSessionId, String agentkRunId, AgentKOutboundIdentity identity) {
        ResponseEntity<?> eventsResp = agentKClient.exchangeJson("GET",
                "/sessions/" + agentkSessionId + "/events",
                AgentKPaging.allEventsQuery(),
                null, AgentKListEventsResponse.class, identity);
        if (!eventsResp.getStatusCode().is2xxSuccessful()
                || !(eventsResp.getBody() instanceof AgentKListEventsResponse)) {
            return null;
        }
        AgentKListEventsResponse eventsBody = (AgentKListEventsResponse) eventsResp.getBody();
        List<AgentKListEventsResponse.AgentKEventDto> events =
                eventsBody.getEvents() == null ? Collections.emptyList() : eventsBody.getEvents();
        String runStatus = inferTurnStatus(events);
        int maxSeq = 0;
        for (AgentKListEventsResponse.AgentKEventDto event : events) {
            Integer eventSeq = toIntSeqOrNull(event.getSeq());
            if (eventSeq != null && eventSeq > maxSeq) {
                maxSeq = eventSeq;
            }
            if (StringUtils.hasText(event.getKind())) {
                try {
                    String eventExecutionId = StringUtils.hasText(event.getExecutionId())
                            ? event.getExecutionId() : agentkRunId;
                    chatPersistService.persistRunEvent(agentkSessionId, eventExecutionId,
                            eventSeq == null ? 0 : eventSeq,
                            event.getKind(), event.getData(), event.getCreatedAt(),
                            event.getRoundId(), event.getEventId(), event.getExecutionId());
                } catch (Exception e) {
                    LOGGER.warn("persistRunEvent failed sessionId={} kind={} seq={}",
                            agentkSessionId, event.getKind(), event.getSeq(), e);
                }
            }
        }
        chatPersistService.updateStreamCursor(agentkSessionId, agentkRunId, maxSeq, runStatus);
        persistAssetsImmediately(agentkSessionId, events);

        SyncSnapshot snap = new SyncSnapshot();
        snap.runStatus = runStatus;
        return snap;
    }

    /**
     * 历史回填：按 round 投影 events 中的 image/video；跳过平台已标记 failed 的 run。
     */
    public int backfillAssetsFromEvents(String agentkSessionId, AgentKOutboundIdentity identity) {
        if (!StringUtils.hasText(agentkSessionId) || identity == null) {
            return 0;
        }
        ResponseEntity<?> eventsResp = agentKClient.exchangeJson("GET",
                "/sessions/" + agentkSessionId.trim() + "/events",
                AgentKPaging.allEventsQuery(),
                null, AgentKListEventsResponse.class, identity);
        if (!eventsResp.getStatusCode().is2xxSuccessful()
                || !(eventsResp.getBody() instanceof AgentKListEventsResponse)) {
            LOGGER.warn("backfill agent_loop assets skipped sessionId={} httpStatus={}",
                    agentkSessionId, eventsResp.getStatusCodeValue());
            return 0;
        }
        AgentKListEventsResponse eventsBody = (AgentKListEventsResponse) eventsResp.getBody();
        List<AgentKListEventsResponse.AgentKEventDto> events =
                eventsBody.getEvents() == null ? Collections.emptyList() : eventsBody.getEvents();
        return persistAssetsImmediately(agentkSessionId.trim(), events);
    }

    /** 事件里一旦出现可发布媒体就写 {@code aop_asset}，不等 round 终态。 */
    private int persistAssetsImmediately(String agentkSessionId,
            List<AgentKListEventsResponse.AgentKEventDto> events) {
        Map<String, List<SyncedAsset>> assetsByRun = collectAssetsByRun(events);
        Set<String> failedRunIds = chatPersistService.listFailedAgentkRunIds(assetsByRun.keySet());
        int synced = 0;
        for (Map.Entry<String, List<SyncedAsset>> entry : assetsByRun.entrySet()) {
            List<SyncedAsset> assets = entry.getValue();
            if (assets == null || assets.isEmpty()) {
                continue;
            }
            String eventRunId = entry.getKey();
            if (StringUtils.hasText(eventRunId) && failedRunIds.contains(eventRunId.trim())) {
                continue;
            }
            chatPersistService.upsertAssets(agentkSessionId, eventRunId, assets);
            synced += assets.size();
        }
        return synced;
    }

    private Map<String, List<SyncedAsset>> collectAssetsByRun(List<AgentKListEventsResponse.AgentKEventDto> events) {
        Map<String, List<SyncedAsset>> assetsByRun = new LinkedHashMap<>();
        Map<String, String> toolCallPrompts = ChatAssetCreationPrompt.indexToolCallPrompts(events);
        String currentRunId = "";
        String lastUserPrompt = "";
        for (AgentKListEventsResponse.AgentKEventDto event : events) {
            if (event == null || event.getData() == null) {
                continue;
            }
            if ("round.started".equals(event.getKind()) || "run.started".equals(event.getKind())) {
                String runId = eventRunId(event);
                if (StringUtils.hasText(runId)) {
                    currentRunId = runId.trim();
                }
                continue;
            }
            if ("user.input".equals(event.getKind())) {
                lastUserPrompt = event.getData().path("text").asText("");
                continue;
            }
            if ("assistant.text".equals(event.getKind())
                    || "tool.exec_succeeded".equals(event.getKind())
                    || "job.completed".equals(event.getKind())) {
                String promptText = ChatAssetCreationPrompt.resolve(
                        event.getData(), toolCallPrompts, lastUserPrompt);
                List<SyncedAsset> assets = toSyncedAssets(event, promptText);
                if (assets.isEmpty()) {
                    continue;
                }
                String bucketKey = eventRunId(event);
                if (!StringUtils.hasText(bucketKey)) {
                    bucketKey = StringUtils.hasText(currentRunId) ? currentRunId : "";
                }
                assetsByRun.computeIfAbsent(bucketKey, ignored -> new ArrayList<>()).addAll(assets);
            }
        }
        return assetsByRun;
    }

    private static String eventRunId(AgentKListEventsResponse.AgentKEventDto event) {
        if (event == null) {
            return "";
        }
        if (StringUtils.hasText(event.getExecutionId())) {
            return event.getExecutionId().trim();
        }
        if (event.getData() == null) {
            return "";
        }
        String runId = event.getData().path("runId").asText("");
        if (StringUtils.hasText(runId)) {
            return runId;
        }
        return event.getData().path("run_id").asText("");
    }

    private List<SyncedAsset> toSyncedAssets(AgentKListEventsResponse.AgentKEventDto event, String promptText) {
        List<AgentKMediaArtifactDto> artifacts = AgentKMediaArtifactExtractor.extract(event.getData());
        if (artifacts.isEmpty()) {
            return Collections.emptyList();
        }
        List<SyncedAsset> assets = new ArrayList<>();
        for (AgentKMediaArtifactDto artifact : artifacts) {
            if (artifact == null || !AgentKMediaArtifactExtractor.isPublishableType(artifact.getType())
                    || !StringUtils.hasText(artifact.getUrl())) {
                continue;
            }
            SyncedAsset asset = new SyncedAsset();
            asset.setSeq(toIntSeqOrNull(event.getSeq()));
            asset.setAssetType(artifact.getType());
            asset.setAssetUrl(officeUrlService.toOfficeUrl(artifact.getUrl()));
            asset.setPromptText(promptText);
            asset.setSourceKind("agent_loop");
            asset.setSourceName(event.getKind());
            asset.setRoundId(event.getRoundId());
            assets.add(asset);
        }
        return assets;
    }

    /*
     * 旧闭环（01710caa）：轮询 /runs/{runId}/events，终态后再拉 /items。整段注释保留，勿直接删。
     *
     * private SyncSnapshot syncOnce(String agentkSessionId, String agentkRunId, AgentKOutboundIdentity identity) {
     *     ResponseEntity<?> eventsResp = agentKClient.exchangeJson("GET", "/runs/" + agentkRunId + "/events",
     *             "after=0", null, AgentKListEventsResponse.class, identity);
     *     if (!eventsResp.getStatusCode().is2xxSuccessful()
     *             || !(eventsResp.getBody() instanceof AgentKListEventsResponse)) {
     *         return null;
     *     }
     *     AgentKListEventsResponse eventsBody = (AgentKListEventsResponse) eventsResp.getBody();
     *     AgentKRunDto run = eventsBody.getRun();
     *     String sessionId = agentkSessionId;
     *     String runStatus = null;
     *     int maxSeq = 0;
     *     if (run != null) {
     *         if (StringUtils.hasText(run.getSessionId())) {
     *             sessionId = run.getSessionId();
     *         }
     *         runStatus = run.getStatus();
     *     }
     *     if (eventsBody.getEvents() != null) {
     *         for (AgentKListEventsResponse.AgentKEventDto event : eventsBody.getEvents()) {
     *             if (event.getSeq() != null && event.getSeq() > maxSeq) {
     *                 maxSeq = event.getSeq().intValue();
     *             }
     *             if (StringUtils.hasText(event.getKind()) && shouldPersistEvent(event.getKind())) {
     *                 chatPersistService.persistRunEvent(sessionId, agentkRunId,
     *                         event.getSeq() == null ? 0 : event.getSeq().intValue(),
     *                         event.getKind(), event.getPayload(), event.getCreatedAt());
     *             }
     *         }
     *     }
     *     if (StringUtils.hasText(sessionId)) {
     *         chatPersistService.updateStreamCursor(sessionId, agentkRunId, maxSeq, runStatus);
     *     }
     *     if (StringUtils.hasText(sessionId) && isTerminal(runStatus)) {
     *         syncItems(sessionId, agentkRunId, identity);
     *     }
     *     SyncSnapshot snap = new SyncSnapshot();
     *     snap.sessionId = sessionId;
     *     snap.runStatus = runStatus;
     *     return snap;
     * }
     *
     * private void syncItems(String agentkSessionId, String agentkRunId, AgentKOutboundIdentity identity) {
     *     ResponseEntity<?> itemsResp = agentKClient.exchangeJson("GET",
     *             "/sessions/" + agentkSessionId + "/items", "after=0&limit=1000", null,
     *             AgentKListItemsResponse.class, identity);
     *     if (!itemsResp.getStatusCode().is2xxSuccessful()
     *             || !(itemsResp.getBody() instanceof AgentKListItemsResponse)) {
     *         return;
     *     }
     *     AgentKListItemsResponse body = (AgentKListItemsResponse) itemsResp.getBody();
     *     if (body.getItems() == null || body.getItems().isEmpty()) {
     *         return;
     *     }
     *     List<SyncedItem> synced = new ArrayList<>();
     *     for (AgentKItemDto item : body.getItems()) {
     *         if (item == null || item.getSeq() == null) {
     *             continue;
     *         }
     *         SyncedItem row = new SyncedItem();
     *         row.setSeq(item.getSeq().intValue());
     *         row.setRole(item.getRole());
     *         row.setContentJson(writeJson(item.getParts()));
     *         synced.add(row);
     *     }
     *     chatPersistService.syncItemsFromAgentk(agentkSessionId, agentkRunId, synced);
     * }
     */

    /** 供 Proxy / Regression 共用的 execution 终态推断。round/tool 结束不代表整个 execution 结束。 */
    public static String inferTurnStatus(List<AgentKListEventsResponse.AgentKEventDto> events) {
        if (events == null || events.isEmpty()) {
            return "running";
        }
        for (AgentKListEventsResponse.AgentKEventDto event : events) {
            if (event == null || !"execution.finished".equals(event.getKind())) {
                continue;
            }
            String status = event.getData() == null ? "" : event.getData().path("status").asText("");
            if (status.contains("FAILED") || status.contains("CANCELLED")) {
                return "failed";
            }
            return "completed";
        }
        return "running";
    }

    private long followUpTimeoutMs() {
        int jsonTimeout = properties.getJsonReadTimeoutMs();
        return jsonTimeout > 0 ? Math.max(jsonTimeout, DEFAULT_TIMEOUT_MS) : DEFAULT_TIMEOUT_MS;
    }

    private static boolean isTerminal(String status) {
        return "completed".equals(status) || "failed".equals(status);
    }

    /** Long seq 转 int；溢出则记日志并返回 null，避免 intValue 截断。 */
    public static Integer toIntSeqOrNull(Long seq) {
        if (seq == null) {
            return null;
        }
        try {
            return Math.toIntExact(seq);
        } catch (ArithmeticException e) {
            LOGGER.warn("event seq overflow seq={}", seq);
            return null;
        }
    }

    private static final class SyncSnapshot {
        private String runStatus;
    }
}
