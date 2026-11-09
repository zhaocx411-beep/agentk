package com.kuaishou.agent.operation.backend.platform.dto;

import lombok.Data;

/**
 * 作品集分页查询（body）。
 * <p>
 * 团队/公开作品集 {@code POST .../chat/portfolio/query}：可选传 {@code userId} 筛选；不传则全站。
 * 我的作品集 {@code POST .../chat/my-portfolio/query}：忽略 body 中的 {@code userId}，强制当前登录用户。
 */
@Data
public class ChatAssetPortfolioQuery {

    /** 产出用户 id；仅团队/公开作品集生效，我的作品集由服务端强制覆盖 */
    private String userId;
    /** image / video，可选 */
    private String assetType;
    /** agent_loop / pipeline，可选 */
    private String sourceKind;
    /** online / offline，可选；与 sourceKind 正交 */
    private String source;
    /** 平台 session id，可选 */
    private Long sessionId;
    /** 平台 run id（一轮对话），可选 */
    private Long runId;
    /** agent-k turn/run id（一轮对话），可选 */
    private String agentkRunId;
    /** pipeline 执行 id（一次 pipeline 跑批），可选 */
    private String workflowExecId;
    /** 提示词关键词，模糊匹配 prompt_text */
    private String promptKeyword;
    /**
     * 智能体名称（精确匹配 {@code aop_asset.agent_name}）。
     */
    private String agentName;
    /**
     * Workflow 名称（精确匹配 aop_workflow.name；用于 session_kind=workflow 筛选）。
     */
    private String workflowName;
    /** 从 1 开始，默认 1 */
    private Integer page;
    /** 默认 20，上限 100 */
    private Integer pageSize;
}
