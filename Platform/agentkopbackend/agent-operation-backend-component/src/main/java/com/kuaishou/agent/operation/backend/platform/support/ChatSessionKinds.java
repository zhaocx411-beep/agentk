package com.kuaishou.agent.operation.backend.platform.support;

import org.springframework.util.StringUtils;

/**
 * 平台会话入口类型：建会话落库 {@code session_kind}，个人/管理历史按此筛选。
 * <p>
 * 与 {@code config_source}（profile 组装来源）不同：本字段表示业务入口是 AgentLoop 还是 Pipeline/Workflow。
 * 对外可用 {@code pipeline}（与前端用语一致），落库统一为 {@link #WORKFLOW}。
 */
public final class ChatSessionKinds {

    public static final String AGENT_LOOP = "agent_loop";
    /** 平台 Workflow / 前端 Pipeline 入口；库内固定写此值 */
    public static final String WORKFLOW = "workflow";
    /** 请求别名，等价 {@link #WORKFLOW} */
    public static final String PIPELINE = "pipeline";
    /** 未 pin 平台版本、仅 raw profile / 实例兜底 */
    public static final String CUSTOM = "custom";

    private ChatSessionKinds() {
    }

    /**
     * 解析入口类型并归一到落库值。显式值优先；{@code pipeline} → {@code workflow}。
     * 未传时：有 agentLoop → agent_loop；仅 workflow pin → workflow；都无 → custom。
     */
    public static String resolve(String explicit, Long agentLoopVersionId, Long workflowVersionId) {
        if (StringUtils.hasText(explicit)) {
            String kind = normalize(explicit.trim());
            if (kind != null) {
                return kind;
            }
            throw new IllegalArgumentException(
                    "sessionKind 仅支持 agent_loop / workflow|pipeline / custom，当前值: " + explicit.trim());
        }
        boolean hasAgent = agentLoopVersionId != null && agentLoopVersionId > 0;
        boolean hasWorkflow = workflowVersionId != null && workflowVersionId > 0;
        if (hasAgent) {
            return AGENT_LOOP;
        }
        if (hasWorkflow) {
            return WORKFLOW;
        }
        return CUSTOM;
    }

    /** 请求/筛选入参归一：pipeline → workflow；非法返回 null。 */
    public static String normalize(String kind) {
        if (!StringUtils.hasText(kind)) {
            return null;
        }
        String k = kind.trim();
        if (AGENT_LOOP.equals(k) || CUSTOM.equals(k) || WORKFLOW.equals(k)) {
            return k;
        }
        if (PIPELINE.equals(k)) {
            return WORKFLOW;
        }
        return null;
    }

    public static boolean isKnown(String kind) {
        return normalize(kind) != null;
    }
}
