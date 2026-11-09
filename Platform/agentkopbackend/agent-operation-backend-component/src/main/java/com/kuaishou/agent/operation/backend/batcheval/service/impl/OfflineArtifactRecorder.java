package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Optional;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.AgentKMediaArtifactExtractor;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMediaArtifactDto;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkClient.PipelineRunResult;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.CreateSessionBegin;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.SyncedAsset;
import com.kuaishou.agent.operation.backend.platform.support.ChatAssetSources;
import com.kuaishou.agent.operation.backend.platform.support.ChatSessionKinds;

/**
 * 把离线跑数的产物并进作品体系。
 * <p>
 * {@code ChatPersistService.upsertAssets} 以 {@code aop_chat_session} 为锚：离线会话是
 * 中后台直接建到 agent-k 的，平台侧没有对应行，产物就会被静默丢弃。所以先登记一条轻量
 * session（{@code user_id = offline_job.creator}，并绑定 {@code agent_version_id}），
 * 资产再按 {@code source=offline} 写入且冗余 {@code agent_name}，作品页即可按作者与
 * 智能体名筛出离线产物（与在线 session→agent 绑定路径一致）。
 * <p>
 * 记录失败一律只告警：跑数结果已落 batcheval 表，作品同步不该反过来把用例判失败。
 */
@Component
public class OfflineArtifactRecorder {

    private static final Logger logger = LoggerFactory.getLogger(OfflineArtifactRecorder.class);
    /**
     * sessionKind 不在这里写死。和在线建会话一样交给
     * {@link ChatSessionKinds#resolve}：绑了 agent 版本就是 {@code agent_loop}。
     * 离线身份只用 {@code entrySource} 区分。
     */
    private static final String ENTRY_SOURCE = "offline_batch";

    private final ChatPersistService chatPersistService;
    private final ObjectMapper objectMapper;

    public OfflineArtifactRecorder(ChatPersistService chatPersistService, ObjectMapper objectMapper) {
        this.chatPersistService = chatPersistService;
        this.objectMapper = objectMapper;
    }

    /**
     * 建会话后立刻登记，保证后续事件与产物能找到平台会话锚点。
     * 登记失败返回 {@code false}，调用方应中止该用例——白跑一场却写不进账本。
     */
    public boolean registerSession(OfflineJob job, OfflineJobCell cell, String agentkSessionId) {
        if (!StringUtils.hasText(agentkSessionId)) {
            return false;
        }
        try {
            Optional<ChatSession> existing = chatPersistService.findByAgentkSessionId(agentkSessionId);
            if (existing.isPresent()) {
                return true;
            }
            Long agentVersionId = job.getAgentVersionId() != null && job.getAgentVersionId() > 0
                    ? job.getAgentVersionId() : null;
            ChatPersistService.EffectiveSnapshot effective = new ChatPersistService.EffectiveSnapshot();
            effective.setProfileVersion(job.getProfileVersion());
            effective.setModel(job.getModel());
            effective.setToolsJson(toolsJson(job));
            // 与在线 AgentKCreateSessionService 同一套规则，不写死 custom。
            effective.setSessionKind(ChatSessionKinds.resolve(null, agentVersionId, null));
            effective.setEntrySource(ENTRY_SOURCE);
            effective.setConfigSource("offline_batch_eval");
            CreateSessionBegin begin = chatPersistService.beginCreateSession(
                    job.getCreator(), null, job.getBizId() + "/" + cell.getTaskKey(),
                    agentVersionId, null, null, job.getProfileYaml(), effective);
            chatPersistService.completeCreateSession(begin, agentkSessionId, job.getProfileVersion(),
                    true, 200, null);
            return true;
        } catch (RuntimeException e) {
            logger.warn("登记离线会话失败，本用例产物不会进作品页: {}/{} session={}",
                    job.getBizId(), cell.getTaskKey(), agentkSessionId, e);
            return false;
        }
    }

    /** MyBatis hydrate 后 toolsJson 会被清掉，这里从 tools 列表回写。 */
    private String toolsJson(OfflineJob job) {
        if (job == null) {
            return null;
        }
        if (StringUtils.hasText(job.getToolsJson())) {
            return job.getToolsJson();
        }
        if (job.getTools() == null || job.getTools().isEmpty()) {
            return "[]";
        }
        try {
            return objectMapper.writeValueAsString(job.getTools());
        } catch (JsonProcessingException e) {
            return null;
        }
    }

    /**
     * 把 Loop sink 已抽出的产物与 pipeline 输出写入 {@code aop_chat_asset}。
     *
     * @return 去重后的产物 URL，按发现顺序，供 cell.output_uris 展示
     */
    public List<String> record(OfflineJob job, OfflineJobCell cell, String agentkSessionId,
            List<SyncedAsset> loopAssets, PipelineRunResult pipelineResult) {
        LinkedHashSet<String> urls = new LinkedHashSet<>();
        String agentName = job == null ? null : job.getAgentName();
        List<SyncedAsset> loop = loopAssets == null ? Collections.emptyList() : loopAssets;
        for (SyncedAsset asset : loop) {
            if (asset != null && StringUtils.hasText(asset.getAssetUrl())) {
                urls.add(asset.getAssetUrl());
            }
        }
        List<SyncedAsset> pipelineAssets = fromPipeline(pipelineResult, cell.getPrompt(), agentName, urls);
        try {
            if (!loop.isEmpty()) {
                // cell.agentkRunId 存的是 agent-k executionId，与在线资产落库同口径。
                chatPersistService.upsertAssets(agentkSessionId, cell.getAgentkRunId(), loop);
            }
            if (!pipelineAssets.isEmpty()) {
                chatPersistService.upsertAssets(agentkSessionId, null, pipelineAssets);
            }
        } catch (RuntimeException e) {
            String bizId = job == null ? "unknown" : job.getBizId();
            logger.warn("离线产物写入作品失败: {}/{} session={}",
                    bizId, cell.getTaskKey(), agentkSessionId, e);
        }
        return new ArrayList<>(urls);
    }

    private static List<SyncedAsset> fromPipeline(PipelineRunResult result, String promptText, String agentName,
            LinkedHashSet<String> urls) {
        List<SyncedAsset> assets = new ArrayList<>();
        if (result == null || !result.isSucceeded()) {
            return assets;
        }
        for (AgentKMediaArtifactDto artifact : AgentKMediaArtifactExtractor.extract(result.getOutput())) {
            if (!urls.add(artifact.getUrl())) {
                continue;
            }
            SyncedAsset asset = new SyncedAsset();
            asset.setAssetType(artifact.getType());
            asset.setAssetUrl(artifact.getUrl());
            asset.setPromptText(promptText);
            asset.setSourceKind("pipeline");
            asset.setSourceName(result.getPipeline());
            asset.setWorkflowExecId(result.getExecId());
            asset.setSource(ChatAssetSources.OFFLINE);
            if (StringUtils.hasText(agentName)) {
                asset.setAgentName(agentName.trim());
            }
            assets.add(asset);
        }
        return assets;
    }
}
