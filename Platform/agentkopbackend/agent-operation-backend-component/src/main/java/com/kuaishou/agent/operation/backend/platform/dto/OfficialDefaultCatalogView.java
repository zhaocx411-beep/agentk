package com.kuaishou.agent.operation.backend.platform.dto;

import java.util.ArrayList;
import java.util.List;

import lombok.Data;

/**
 * 官方示范目录：{@code aop_agent} 与 {@code aop_workflow} 分区、均为数组。
 */
@Data
public class OfficialDefaultCatalogView {

    /** 官方默认 AgentLoop 列表（{@code aop_agent is_default=1}）。 */
    private List<OfficialAgentLoopItem> agentLoops = new ArrayList<>();

    /** 官方默认工作流列表（{@code aop_workflow is_default=1}）。 */
    private List<OfficialWorkflowAgentItem> workflowAgents = new ArrayList<>();

    @Data
    public static class OfficialAgentLoopItem {
        /** 固定 {@code agent_loop}。 */
        private String kind = "agent_loop";
        private Long agentLoopId;
        private String name;
        private String description;
        private Long agentLoopVersionId;
        private Integer version;
        private String profileVersionLabel;
        /** 一键建会话 mode：{@code default_agent}。 */
        private String quickMode = "default_agent";
        /** 快速对话相对路径：由目录接口填 sessions/quick/agent-loops/{id}。 */
        private String quickPath;
    }

    @Data
    public static class OfficialWorkflowAgentItem {
        /** 固定 {@code workflow}。 */
        private String kind = "workflow";
        /** 兼容字段；主路径为空。 */
        private Long agentLoopId;
        /** {@code aop_workflow.id}。 */
        private Long workflowId;
        private String name;
        private String description;
        /** {@code aop_workflow_version.id}。 */
        private Long workflowVersionId;
        private Integer version;
        /**
         * 官方 quick 路径键：优先 {@code aop_workflow.agentk_id}。
         */
        private String workflowKey;
        /** agent-k capability id（可选）。 */
        private String agentkId;
        /** {@code aop_workflow_version.definition_json.capability}；缺失则为空，不拼接 pipeline:agentkId。 */
        private String pipelineRef;
        /** {@code aop_workflow_version.definition_json.name} */
        private String pipelineName;
        /** 默认工具底座 Agent 已发布版本 id（仅展示）。 */
        private Long agentLoopVersionId;
        /** 一键建会话 mode：{@code default_workflow}。 */
        private String quickMode = "default_workflow";
        /** 快速对话相对路径。 */
        private String quickPath;
    }
}
