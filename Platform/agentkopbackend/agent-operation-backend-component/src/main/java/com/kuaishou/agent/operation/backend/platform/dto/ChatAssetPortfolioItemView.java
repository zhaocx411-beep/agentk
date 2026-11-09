package com.kuaishou.agent.operation.backend.platform.dto;

import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.List;

import lombok.Data;

/**
 * 作品集列表项：按 run / pipeline 执行聚合（同一 roundKey 一条）。
 * <p>
 * 不再「一条资产一行」；中间产物与结果产物分列返回。
 */
@Data
public class ChatAssetPortfolioItemView {

    /**
     * 产出批次键：同一 round / 同一次 pipeline 执行共享。
     * agent_loop → {@code round:{agentkRunId}}；pipeline → {@code pipeline:{workflowExecId}}；
     * api → {@code api:{workflowExecId}}。
     */
    private String roundKey;
    /** 平台 run id（agent_loop）。 */
    private Long runId;
    private String agentkRunId;
    private String workflowExecId;
    /** agent_loop / pipeline / api */
    private String sourceKind;
    /** online / offline */
    private String source;
    private String sourceName;
    /** 智能体名称（aop_agent.name）；与 workflowName 分离。 */
    private String agentName;
    /**
     * Workflow 名称（aop_workflow.name）；仅 session_kind=workflow 时有值。
     */
    private String workflowName;
    /** 该轮代表性 prompt（优先结果产物，否则取中间产物）。 */
    private String promptText;
    private String userId;
    /** 该轮最新产物时间。 */
    private LocalDateTime createdAt;

    /** 封面：优先结果产物第一张，否则中间产物最后一张。 */
    private AssetBrief cover;
    /** 中间产物（如生图、抽帧等过程媒体）。 */
    private List<AssetBrief> intermediateAssets = new ArrayList<>();
    /** 结果产物（如最终成片视频；无视频时取该轮最后一条）。 */
    private List<AssetBrief> resultAssets = new ArrayList<>();

    private SessionSummary session;

    @Data
    public static class AssetBrief {
        private Long assetId;
        private String assetType;
        private String assetUrl;
        private String promptText;
        private Integer agentkEventSeq;
        private LocalDateTime createdAt;
    }

    @Data
    public static class SessionSummary {
        private Long sessionId;
        private String publicId;
        private String agentkSessionId;
        private String title;
        /** agent_loop / workflow / custom */
        private String sessionKind;
        private String effectiveModel;
        private Long agentLoopVersionId;
        private Long workflowVersionId;
        private LocalDateTime sessionCreatedAt;
    }
}
