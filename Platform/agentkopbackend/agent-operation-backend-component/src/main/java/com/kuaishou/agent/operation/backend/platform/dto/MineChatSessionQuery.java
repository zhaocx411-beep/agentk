package com.kuaishou.agent.operation.backend.platform.dto;

import java.time.LocalDateTime;

import lombok.Data;

/**
 * 个人历史：会话列表筛选（body）。
 * <p>
 * 默认按当前登录用户隔离。超级管理员（role=SUPER_ADMIN）可不传 {@code userId} 查看全部会话，
 * 或传入 {@code userId} 查看指定用户；非超管传入的 {@code userId} 会被忽略。
 * 版本维度可选，有值时须为自己的 AgentLoop / Workflow（超管可跨用户）。
 * 优先级：agentLoopVersionId → workflowVersionId → agentLoopId → workflowId。
 * {@code sessionKind} 可单独筛入口类型（agent_loop / workflow / custom）。
 */
@Data
public class MineChatSessionQuery {
    private Long agentLoopId;
    private Long agentLoopVersionId;
    private Long workflowId;
    private Long workflowVersionId;
    /** agent_loop / workflow|pipeline / custom，可选；pipeline 会归一为 workflow */
    private String sessionKind;
    /** creating / active / archived / failed，可选 */
    private String status;
    private String titleKeyword;
    private LocalDateTime createdFrom;
    private LocalDateTime createdTo;
    /** 从 1 开始，默认 1 */
    private Integer page;
    /** 默认 20，上限 100 */
    private Integer pageSize;
    /**
     * 仅超级管理员生效：指定则只看该用户会话，不传则看全部用户。
     * 非超管忽略此字段，始终只看自己。
     */
    private String userId;
}
