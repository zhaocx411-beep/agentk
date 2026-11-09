package com.kuaishou.agent.operation.backend.platform.dto;

import java.time.LocalDateTime;

import lombok.Data;

/**
 * 管理回看：会话列表筛选（body，便于扩展条件）。
 * <p>
 * 版本维度均可空：全空时为管理员全局回看；有值时优先级
 * agentLoopVersionId → workflowVersionId → agentLoopId → workflowId。
 */
@Data
public class AdminChatSessionQuery {
    private Long agentLoopId;
    private Long agentLoopVersionId;
    private Long workflowId;
    private Long workflowVersionId;
    /** agent_loop / workflow / custom，可选 */
    private String sessionKind;
    /** 会话归属用户（端上用户），可选 */
    private String userId;
    /**
     * agent-k session id（{@code ses_xxx}），精确匹配。
     */
    private String sessionId;
    /** creating / active / archived / failed，可选 */
    private String status;
    /** 标题模糊，可选 */
    private String titleKeyword;
    private LocalDateTime createdFrom;
    private LocalDateTime createdTo;
    /** 从 1 开始，默认 1 */
    private Integer page;
    /** 默认 20，上限 100 */
    private Integer pageSize;
}
