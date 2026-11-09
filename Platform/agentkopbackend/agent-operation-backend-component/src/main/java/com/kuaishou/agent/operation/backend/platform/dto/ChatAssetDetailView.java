package com.kuaishou.agent.operation.backend.platform.dto;

import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.List;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * 作品集详情：当前资产 + 同轮产物 + 会话实际 profile 配置（非版本号元数据）。
 */
@Data
public class ChatAssetDetailView {

    private Long assetId;
    private String assetType;
    private String assetUrl;
    /** 与该资产绑定的用户提示词（工具/Pipeline 入参或对话 prompt）。 */
    private String promptText;
    private String sourceKind;
    /** online=在线问答 offline=离线批量跑数 */
    private String source;
    private String sourceName;
    /** 智能体名称（aop_agent.name）；与 workflowName 分离。 */
    private String agentName;
    /**
     * Workflow 名称（aop_workflow.name）；仅 session_kind=workflow 时有值。
     */
    private String workflowName;
    private String workflowExecId;
    private String agentkRunId;
    private Integer agentkEventSeq;
    private Long runId;
    private Long messageId;
    /** 作品归属用户（公开展示）。 */
    private String userId;
    private String status;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;

    /** 产出批次键，与列表项 {@link ChatAssetPortfolioItemView#roundKey} 一致。 */
    private String roundKey;

    /** 同一 round / 同一次 pipeline 执行产出的其它媒体（含当前这条）。 */
    private List<SameRoundAssetSummary> sameRoundAssets = new ArrayList<>();

    @Data
    public static class SameRoundAssetSummary {
        private Long assetId;
        private String assetType;
        private String assetUrl;
        private Integer agentkEventSeq;
    }

    /** 所属会话摘要（不含 AgentLoop/Workflow 版本号）。 */
    private SessionContext session;

    /**
     * 建会话时固化的 agent-k Profile 快照：
     * {@code defaultModel} / {@code system} / {@code tools} / {@code toolDefaults} / {@code pipelines} 等。
     */
    private JsonNode profile;

    @Data
    public static class SessionContext {
        private Long sessionId;
        private String publicId;
        private String agentkSessionId;
        private String title;
        /** agent_loop / workflow / custom */
        private String sessionKind;
        private String entrySource;
        private String status;
        private LocalDateTime createdAt;
    }
}
