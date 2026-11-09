package com.kuaishou.agent.operation.backend.platform.service;

import java.util.List;

import com.kuaishou.agent.operation.backend.platform.entity.Workflow;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;

/**
 * Workflow 版本：创建 → 草稿定义 → 发布。
 */
public interface WorkflowVersionService {

    Workflow createWorkflow(String name, String description, String userId, String tenantId);

    WorkflowVersion saveDraft(long workflowId, String definitionJson, String userId);

    WorkflowVersion publish(long workflowVersionId);

    WorkflowVersion publish(long workflowVersionId, String userId);

    WorkflowVersion requirePublished(long workflowVersionId);

    Workflow findWorkflow(long workflowId);

    WorkflowVersion findVersion(long workflowVersionId);

    List<Workflow> listMine(String userId);

    List<WorkflowVersion> listVersions(long workflowId);
}
