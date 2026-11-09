package com.kuaishou.agent.operation.backend.platform.service;

import java.util.List;
import java.util.Optional;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityViewDto;
import com.kuaishou.agent.operation.backend.platform.dto.OfficialDefaultCatalogView;
import com.kuaishou.agent.operation.backend.platform.dto.OfficialToolPlazaView;
import com.kuaishou.agent.operation.backend.platform.entity.Agent;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.entity.Workflow;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;

/**
 * 官方示范资产（is_default=1）只读查询。
 */
public interface OfficialDemoService {

    List<Agent> listDefaultAgents();

    Optional<AgentVersion> findPublishedVersion(Agent agent);

    /**
     * 按 agentLoopId 或名称定位默认 Agent 的已发布版本；
     * 都未传时取列表中第一个有发布版本的。
     */
    Optional<AgentVersion> findDefaultPublishedAgentVersion(Long agentLoopId, String agentLoopName);

    List<Workflow> listDefaultWorkflows();

    Optional<WorkflowVersion> findPublishedVersion(Workflow workflow);

    /** 按 workflow 主键或 definition_json.capability / definition_json.ref / agentk_id / 展示名定位默认工作流的已发布版本。 */
    Optional<WorkflowVersion> findDefaultPublishedWorkflowVersion(Long workflowId, String workflowKey);

    OfficialDefaultCatalogView defaultCatalog();

    /**
     * 工具广场：传入实时 ListCapabilities 的 activity 列表；空列表返回 empty。
     * {@code loadError} 非空时仍返回空列表（Controller 对实时拉取失败改为 600）。
     */
    OfficialToolPlazaView toolPlaza(List<AgentKCapabilityViewDto> capabilities, String loadError);
}
