package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.agentk.AgentKMediaArtifactExtractor;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMediaArtifactDto;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkClient;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.SyncedAsset;
import com.kuaishou.agent.operation.backend.platform.support.ChatAssetSources;
import com.kuaishou.agent.operation.backend.platform.support.ChatEntrySources;

/**
 * 离线 Loop 事件的落地口：逐条写 {@code aop_session_event}（复用在线 {@code persistRunEvent}，
 * 只是把 {@code entry_source} 标成 {@code offline_batch}），并顺路把媒体产物攒出来。
 * <p>
 * 事件不再进内存，产物却只能从事件里认，所以提取必须发生在事件流过的时候。
 */
@Component
public class OfflineLoopEventSink {

    private static final Logger logger = LoggerFactory.getLogger(OfflineLoopEventSink.class);

    /** 只有这几类事件会带出媒体产物，与在线资产投影口径一致。 */
    private static final String KIND_ASSISTANT_TEXT = "assistant.text";
    private static final String KIND_TOOL_SUCCEEDED = "tool.exec_succeeded";
    private static final String KIND_JOB_COMPLETED = "job.completed";

    private final ChatPersistService chatPersistService;

    public OfflineLoopEventSink(ChatPersistService chatPersistService) {
        this.chatPersistService = chatPersistService;
    }

    /** 每条用例开一个：状态（已见 URL、已攒产物）按用例隔离。 */
    public CellStream open(String agentkSessionId, String promptText, String agentName) {
        return new CellStream(chatPersistService, agentkSessionId, promptText, agentName);
    }

    /**
     * 单条用例的事件流。
     * <p>
     * 只被跑该用例的那个线程写入，结算线程在 {@code Future.get()} 之后才读，
     * 借 Future 的可见性保证，不需要额外同步。
     */
    public static final class CellStream implements AgentkClient.EventSink {

        private final ChatPersistService chatPersistService;
        private final String agentkSessionId;
        private final String promptText;
        private final String agentName;
        /** 整页重复投递靠它幂等：同一 URL 只留一份产物。 */
        private final LinkedHashSet<String> seenUrls = new LinkedHashSet<>();
        private final List<SyncedAsset> assets = new ArrayList<>();
        private int persistFailures;

        private CellStream(ChatPersistService chatPersistService, String agentkSessionId,
                String promptText, String agentName) {
            this.chatPersistService = chatPersistService;
            this.agentkSessionId = agentkSessionId;
            this.promptText = promptText;
            this.agentName = agentName;
        }

        @Override
        public void accept(JsonNode events) {
            if (events == null || !events.isArray()) {
                return;
            }
            for (JsonNode event : events) {
                persist(event);
                collectArtifacts(event);
            }
        }

        /** 单条写失败只记数：漏一条事件不该让几十分钟的视频用例判失败。 */
        private void persist(JsonNode event) {
            String kind = event.path("kind").asText();
            if (!StringUtils.hasText(kind)) {
                return;
            }
            String executionId = event.path("executionId").asText(null);
            try {
                chatPersistService.persistRunEvent(agentkSessionId, executionId,
                        event.path("seq").asInt(), kind, payloadOf(event),
                        event.path("createdAt").asText(null), event.path("roundId").asText(null),
                        event.path("eventId").asText(null), executionId,
                        ChatEntrySources.OFFLINE_BATCH);
            } catch (RuntimeException e) {
                persistFailures++;
                logger.warn("离线事件落库失败 session={} kind={} seq={} 累计失败={}",
                        agentkSessionId, kind, event.path("seq").asInt(), persistFailures, e);
            }
        }

        private void collectArtifacts(JsonNode event) {
            String kind = event.path("kind").asText();
            if (!KIND_ASSISTANT_TEXT.equals(kind) && !KIND_TOOL_SUCCEEDED.equals(kind)
                    && !KIND_JOB_COMPLETED.equals(kind)) {
                return;
            }
            Integer seq = event.has("seq") ? event.path("seq").asInt() : null;
            for (AgentKMediaArtifactDto artifact : AgentKMediaArtifactExtractor.extract(payloadOf(event))) {
                if (!seenUrls.add(artifact.getUrl())) {
                    continue;
                }
                SyncedAsset asset = new SyncedAsset();
                asset.setSeq(seq);
                asset.setRoundId(event.path("roundId").asText(null));
                asset.setAssetType(artifact.getType());
                asset.setAssetUrl(artifact.getUrl());
                asset.setPromptText(promptText);
                asset.setSourceKind("agent_loop");
                asset.setSourceName(kind);
                asset.setSource(ChatAssetSources.OFFLINE);
                if (StringUtils.hasText(agentName)) {
                    asset.setAgentName(agentName.trim());
                }
                assets.add(asset);
            }
        }

        /** 按发现顺序、已按 URL 去重的 Loop 产物。 */
        public List<SyncedAsset> assets() {
            return assets;
        }

        public int getPersistFailures() {
            return persistFailures;
        }

        private static JsonNode payloadOf(JsonNode event) {
            return event.has("data") ? event.path("data") : event.path("payload");
        }
    }
}
