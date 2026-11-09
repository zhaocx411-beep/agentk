package com.kuaishou.agent.operation.backend.agentk;

import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListWorkflowRunsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMediaArtifactDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKWorkflowExecDto;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.entity.PipelineExec;
import com.kuaishou.agent.operation.backend.platform.service.BlobStoreOfficeUrlService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.SyncedAsset;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.SyncedPipelineExec;
import com.kuaishou.agent.operation.backend.platform.support.OfficialDemoAssets;

/**
 * 从 agent-k pipeline-runs / 会话 events 投影：
 * <ul>
 *   <li>{@code aop_pipeline_exec}：单独执行的完整 args / output / status</li>
 *   <li>{@code aop_chat_asset}：成功产出的 image/video；
 *       Capability Lab（{@code capability_run} / {@code entry_source=capability_lab}）
 *       的 {@code source_kind=capability}，其余 pipeline 仍为 {@code pipeline}</li>
 * </ul>
 */
@Service
public class ChatAssetSyncService {

    private static final Logger LOGGER = LoggerFactory.getLogger(ChatAssetSyncService.class);

    private final AgentKClient agentKClient;
    private final ChatPersistService chatPersistService;
    private final ChatRunFollowUpService chatRunFollowUpService;
    private final BlobStoreOfficeUrlService officeUrlService;

    public ChatAssetSyncService(AgentKClient agentKClient, ChatPersistService chatPersistService,
            ChatRunFollowUpService chatRunFollowUpService, BlobStoreOfficeUrlService officeUrlService) {
        this.agentKClient = agentKClient;
        this.chatPersistService = chatPersistService;
        this.chatRunFollowUpService = chatRunFollowUpService;
        this.officeUrlService = officeUrlService;
    }

    public Map<String, Object> backfill(String source, String scope, int sessionLimit, int sessionOffset,
            String agentkSessionId, AgentKOutboundIdentity identity) {
        String normalizedSource = normalizeSource(source);
        boolean allSessions = "all".equalsIgnoreCase(scope);
        Map<String, Object> result = new LinkedHashMap<>();
        int pipelineSynced = 0;
        int agentLoopSynced = 0;
        int sessionCount = 0;

        if (StringUtils.hasText(agentkSessionId)) {
            String sessionId = agentkSessionId.trim();
            sessionCount = 1;
            if (includePipeline(normalizedSource)) {
                pipelineSynced = syncPipelineRuns(sessionId, identity);
            }
            if (includeAgentLoop(normalizedSource)) {
                agentLoopSynced = chatRunFollowUpService.backfillAssetsFromEvents(sessionId, identity);
            }
        } else {
            List<ChatSession> sessions = allSessions
                    ? chatPersistService.listSessionsForGlobalAssetBackfill(sessionLimit, sessionOffset)
                    : chatPersistService.listSessionsForAssetBackfill(
                            identity.getUserId(), sessionLimit, sessionOffset);
            sessionCount = sessions.size();
            for (ChatSession session : sessions) {
                if (session == null || !StringUtils.hasText(session.getAgentkSessionId())) {
                    continue;
                }
                String sessionId = session.getAgentkSessionId().trim();
                if (includePipeline(normalizedSource)) {
                    pipelineSynced += syncPipelineRuns(sessionId, identity);
                }
                if (includeAgentLoop(normalizedSource)) {
                    agentLoopSynced += chatRunFollowUpService.backfillAssetsFromEvents(sessionId, identity);
                }
            }
        }

        result.put("sessionCount", sessionCount);
        result.put("limit", sessionLimit);
        result.put("offset", sessionOffset);
        result.put("nextOffset", sessionOffset + sessionCount);
        result.put("hasMore", sessionCount >= sessionLimit);
        result.put("pipelineSyncedAssetCount", pipelineSynced);
        result.put("agentLoopSyncedAssetCount", agentLoopSynced);
        result.put("syncedAssetCount", pipelineSynced + agentLoopSynced);
        result.put("source", normalizedSource);
        result.put("scope", allSessions ? "all" : "mine");
        return result;
    }

    public int syncPipelineRuns(String agentkSessionId, AgentKOutboundIdentity identity) {
        if (!StringUtils.hasText(agentkSessionId)) {
            return 0;
        }
        ResponseEntity<?> upstream = agentKClient.exchangeJson("GET",
                "/sessions/" + agentkSessionId.trim() + "/pipeline-runs",
                null, null, AgentKListWorkflowRunsResponse.class, identity);
        if (!upstream.getStatusCode().is2xxSuccessful()
                || !(upstream.getBody() instanceof AgentKListWorkflowRunsResponse)) {
            LOGGER.warn("sync pipeline assets skipped sessionId={} httpStatus={}",
                    agentkSessionId, upstream.getStatusCodeValue());
            return 0;
        }
        AgentKListWorkflowRunsResponse body = (AgentKListWorkflowRunsResponse) upstream.getBody();
        return syncPipelineExecAndAssetsBatch(agentkSessionId, body.getPipelineRuns());
    }

    /**
     * 投影一次 pipeline 执行记录（始终写 exec）+ 成功媒体资产。
     *
     * @return 本次新写入/更新关联的媒体资产条数
     */
    public int syncPipelineExecAndAssets(String agentkSessionId, AgentKWorkflowExecDto run) {
        if (run == null) {
            return 0;
        }
        return syncPipelineExecAndAssetsBatch(agentkSessionId, Collections.singletonList(run));
    }

    /**
     * 同一会话下多条 pipeline-run：每条仍按原规则写 exec / 跳过失败媒体，
     * 智能体名和 upsertAssets 按 session 只做一次。
     */
    public int syncPipelineExecAndAssetsBatch(String agentkSessionId, List<AgentKWorkflowExecDto> runs) {
        if (runs == null || runs.isEmpty()) {
            return 0;
        }
        Map<String, List<SyncedAsset>> assetsBySession = new LinkedHashMap<>();
        Map<String, SessionAssetMeta> sessionMetaById = new HashMap<>();
        for (AgentKWorkflowExecDto run : runs) {
            persistPipelineExec(agentkSessionId, run);
            List<SyncedAsset> assets = collectPublishablePipelineAssets(
                    agentkSessionId, run, sessionMetaById);
            if (assets.isEmpty()) {
                continue;
            }
            String sessionId = pipelineAssetSessionId(agentkSessionId, run);
            List<SyncedAsset> bucket = assetsBySession.get(sessionId);
            if (bucket == null) {
                bucket = new ArrayList<>();
                assetsBySession.put(sessionId, bucket);
            }
            bucket.addAll(assets);
        }
        int synced = 0;
        for (Map.Entry<String, List<SyncedAsset>> entry : assetsBySession.entrySet()) {
            List<SyncedAsset> assets = entry.getValue();
            if (assets == null || assets.isEmpty()) {
                continue;
            }
            chatPersistService.upsertAssets(entry.getKey(), null, assets);
            synced += assets.size();
        }
        return synced;
    }

    /** 落库/更新 {@code aop_pipeline_exec}（含失败执行；无媒体也会记）。 */
    public PipelineExec persistPipelineExec(String agentkSessionId, AgentKWorkflowExecDto run) {
        if (run == null || !StringUtils.hasText(run.getId())) {
            return null;
        }
        String sessionId = StringUtils.hasText(run.getSessionId()) ? run.getSessionId() : agentkSessionId;
        SyncedPipelineExec snapshot = new SyncedPipelineExec();
        snapshot.setAgentkExecId(run.getId());
        snapshot.setPipelineName(run.getName());
        snapshot.setCapabilityId(run.getCapabilityId());
        snapshot.setStatus(run.getStatus());
        snapshot.setArgs(run.getArgs());
        snapshot.setOutput(run.getOutput());
        snapshot.setProfileVersion(run.getProfileVersion());
        snapshot.setError(run.getError());
        snapshot.setStartedAt(run.getCreatedAt());
        snapshot.setFinishedAt(run.getFinishedAt());
        try {
            return chatPersistService.upsertPipelineExec(sessionId, snapshot);
        } catch (Exception e) {
            LOGGER.warn("persist pipeline exec failed sessionId={} execId={} reason={}",
                    sessionId, run.getId(), e.toString());
            return null;
        }
    }

    public int backfillPipelineRunsForUser(String userId, int limit, AgentKOutboundIdentity identity) {
        List<ChatSession> sessions = chatPersistService.listSessionsForAssetBackfill(userId, limit, 0);
        int total = 0;
        for (ChatSession session : sessions) {
            if (session == null || !StringUtils.hasText(session.getAgentkSessionId())) {
                continue;
            }
            total += syncPipelineRuns(session.getAgentkSessionId(), identity);
        }
        return total;
    }

    public int syncPipelineRun(String agentkSessionId, AgentKWorkflowExecDto run) {
        Map<String, SessionAssetMeta> sessionMetaById = new HashMap<>();
        List<SyncedAsset> assets = collectPublishablePipelineAssets(
                agentkSessionId, run, sessionMetaById);
        if (assets.isEmpty()) {
            return 0;
        }
        chatPersistService.upsertAssets(pipelineAssetSessionId(agentkSessionId, run), null, assets);
        return assets.size();
    }

    private List<SyncedAsset> collectPublishablePipelineAssets(String agentkSessionId,
            AgentKWorkflowExecDto run, Map<String, SessionAssetMeta> sessionMetaById) {
        if (run == null || isFailedPipelineStatus(run.getStatus())) {
            return Collections.emptyList();
        }
        enrichPipelineRun(run);
        if (run.getMediaArtifacts() == null || run.getMediaArtifacts().isEmpty()) {
            return Collections.emptyList();
        }
        String sessionId = pipelineAssetSessionId(agentkSessionId, run);
        String promptText = promptTextFromArgs(run.getArgs());
        SessionAssetMeta sessionMeta = sessionMetaById.get(sessionId);
        if (sessionMeta == null && !sessionMetaById.containsKey(sessionId)) {
            sessionMeta = resolveSessionAssetMeta(sessionId);
            sessionMetaById.put(sessionId, sessionMeta);
        }
        String agentName = sessionMeta == null ? "" : sessionMeta.agentName;
        String sourceKind = isCapabilityAsset(run, sessionMeta) ? "capability" : "pipeline";
        // pipeline 产物：agent_name 落 workflow 名（aop_workflow.name），便于作品集按名筛选。
        if ("pipeline".equals(sourceKind) && sessionMeta != null
                && StringUtils.hasText(sessionMeta.workflowName)) {
            agentName = sessionMeta.workflowName;
        }
        List<SyncedAsset> assets = new ArrayList<>();
        for (AgentKMediaArtifactDto artifact : run.getMediaArtifacts()) {
            if (artifact == null || !AgentKMediaArtifactExtractor.isPublishableType(artifact.getType())
                    || !StringUtils.hasText(artifact.getUrl())) {
                continue;
            }
            SyncedAsset asset = new SyncedAsset();
            asset.setAssetType(artifact.getType());
            asset.setAssetUrl(artifact.getUrl());
            asset.setPromptText(promptText);
            asset.setSourceKind(sourceKind);
            asset.setSourceName(StringUtils.hasText(run.getCapabilityId())
                    ? run.getCapabilityId().trim()
                    : run.getName());
            asset.setWorkflowExecId(run.getId());
            asset.setAgentName(agentName);
            assets.add(asset);
        }
        return assets;
    }

    private static String pipelineAssetSessionId(String agentkSessionId, AgentKWorkflowExecDto run) {
        if (run != null && StringUtils.hasText(run.getSessionId())) {
            return run.getSessionId();
        }
        return agentkSessionId;
    }

    public void enrichPipelineRun(AgentKWorkflowExecDto run) {
        if (run == null) {
            return;
        }
        List<AgentKMediaArtifactDto> artifacts = AgentKMediaArtifactExtractor.extract(run.getOutput());
        officeUrlService.rewriteArtifacts(artifacts);
        run.setMediaArtifacts(artifacts);
        run.setImageUrls(AgentKMediaArtifactExtractor.urlsOfType(artifacts, "image"));
        run.setVideoUrls(AgentKMediaArtifactExtractor.urlsOfType(artifacts, "video"));
    }

    static String promptTextFromArgs(com.fasterxml.jackson.databind.JsonNode args) {
        if (args == null || args.isNull() || args.isMissingNode()) {
            return "";
        }
        String[] promptFields = {"prompt", "text", "query", "instruction", "description", "videoPrompt",
                "video_prompt", "imagePrompt", "topic", "title"};
        for (String field : promptFields) {
            com.fasterxml.jackson.databind.JsonNode value = args.get(field);
            if (value != null && value.isTextual() && StringUtils.hasText(value.asText())) {
                return value.asText();
            }
        }
        return args.isTextual() ? args.asText() : "";
    }

    private SessionAssetMeta resolveSessionAssetMeta(String agentkSessionId) {
        if (!StringUtils.hasText(agentkSessionId)) {
            return SessionAssetMeta.EMPTY;
        }
        return chatPersistService.findByAgentkSessionId(agentkSessionId.trim())
                .map(session -> new SessionAssetMeta(
                        emptyToBlank(chatPersistService.resolveSessionAgentName(session)),
                        emptyToBlank(chatPersistService.resolveSessionWorkflowName(session)),
                        OfficialDemoAssets.ENTRY_SOURCE_CAPABILITY_LAB.equals(session.getEntrySource())))
                .orElse(SessionAssetMeta.EMPTY);
    }

    static boolean isCapabilityAsset(AgentKWorkflowExecDto run, SessionAssetMeta sessionMeta) {
        if (run != null && CapabilityRunService.PIPELINE_NAME.equals(trimToNull(run.getName()))) {
            return true;
        }
        return sessionMeta != null && sessionMeta.capabilityLab;
    }

    private static String emptyToBlank(String value) {
        return StringUtils.hasText(value) ? value : "";
    }

    private static String trimToNull(String value) {
        if (!StringUtils.hasText(value)) {
            return null;
        }
        return value.trim();
    }

    static final class SessionAssetMeta {
        static final SessionAssetMeta EMPTY = new SessionAssetMeta("", "", false);

        final String agentName;
        /** session_kind=workflow 时来自 {@code aop_workflow.name}。 */
        final String workflowName;
        final boolean capabilityLab;

        SessionAssetMeta(String agentName, String workflowName, boolean capabilityLab) {
            this.agentName = agentName == null ? "" : agentName;
            this.workflowName = workflowName == null ? "" : workflowName;
            this.capabilityLab = capabilityLab;
        }
    }

    static boolean isFailedPipelineStatus(String status) {
        if (!StringUtils.hasText(status)) {
            return false;
        }
        String normalized = status.trim().toLowerCase(Locale.ROOT);
        return normalized.contains("fail") || normalized.contains("error") || normalized.contains("cancel");
    }

    private static String normalizeSource(String source) {
        if (!StringUtils.hasText(source)) {
            return "all";
        }
        String normalized = source.trim().toLowerCase(Locale.ROOT);
        if ("pipeline".equals(normalized) || "agent_loop".equals(normalized) || "all".equals(normalized)) {
            return normalized;
        }
        return "all";
    }

    private static boolean includePipeline(String source) {
        return "all".equals(source) || "pipeline".equals(source);
    }

    private static boolean includeAgentLoop(String source) {
        return "all".equals(source) || "agent_loop".equals(source);
    }
}
