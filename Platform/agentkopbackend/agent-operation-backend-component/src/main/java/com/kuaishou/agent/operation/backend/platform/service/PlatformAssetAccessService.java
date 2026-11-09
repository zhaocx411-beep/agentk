package com.kuaishou.agent.operation.backend.platform.service;

import java.util.Objects;

import org.springframework.stereotype.Service;

import com.kuaishou.agent.operation.backend.platform.entity.Agent;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.entity.Workflow;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;
import com.kuaishou.agent.operation.backend.platform.support.PlatformAccessException;

/**
 * AgentLoop / Workflow 读写权限（public 可读，owner 可写）。
 */
@Service
public class PlatformAssetAccessService {

    private final AgentVersionService agentVersionService;
    private final WorkflowVersionService workflowVersionService;

    public PlatformAssetAccessService(AgentVersionService agentVersionService,
            WorkflowVersionService workflowVersionService) {
        this.agentVersionService = agentVersionService;
        this.workflowVersionService = workflowVersionService;
    }

    public void assertOwnAgentVersion(String userId, long agentVersionId) {
        AgentVersion av = agentVersionService.findVersion(agentVersionId);
        if (av == null) {
            throw new IllegalArgumentException("agent version not found: " + agentVersionId);
        }
        assertOwnAgentLoop(userId, av.getAgentId());
    }

    public void assertReadableAgentVersion(String userId, long agentVersionId) {
        AgentVersion av = agentVersionService.findVersion(agentVersionId);
        if (av == null) {
            throw new IllegalArgumentException("agent version not found: " + agentVersionId);
        }
        assertReadableAgentLoop(userId, av.getAgentId());
    }

    public void assertOwnWorkflowVersion(String userId, long workflowVersionId) {
        WorkflowVersion wv = workflowVersionService.findVersion(workflowVersionId);
        if (wv == null) {
            throw new IllegalArgumentException("workflow version not found: " + workflowVersionId);
        }
        assertOwnWorkflow(userId, wv.getWorkflowId());
    }

    public void assertReadableWorkflowVersion(String userId, long workflowVersionId) {
        WorkflowVersion wv = workflowVersionService.findVersion(workflowVersionId);
        if (wv == null) {
            throw new IllegalArgumentException("workflow version not found: " + workflowVersionId);
        }
        assertReadableWorkflow(userId, wv.getWorkflowId());
    }

    public void assertOwnAgentLoop(String userId, long agentLoopId) {
        Agent agent = agentVersionService.findAgent(agentLoopId);
        if (agent == null) {
            throw new IllegalArgumentException("agent loop not found: " + agentLoopId);
        }
        if (!Objects.equals(empty(agent.getOwnerUserId()), empty(userId))) {
            throw new PlatformAccessException("not owner of agent loop: " + agentLoopId);
        }
    }

    public void assertReadableAgentLoop(String userId, long agentLoopId) {
        Agent agent = agentVersionService.findAgent(agentLoopId);
        if (agent == null) {
            throw new IllegalArgumentException("agent loop not found: " + agentLoopId);
        }
        if (isPublic(agent.getVisibility())
                || Objects.equals(empty(agent.getOwnerUserId()), empty(userId))) {
            return;
        }
        throw new PlatformAccessException("not allowed to read private agent loop: " + agentLoopId);
    }

    public void assertOwnWorkflow(String userId, long workflowId) {
        Workflow workflow = workflowVersionService.findWorkflow(workflowId);
        if (workflow == null) {
            throw new IllegalArgumentException("workflow not found: " + workflowId);
        }
        if (!Objects.equals(empty(workflow.getOwnerUserId()), empty(userId))) {
            throw new PlatformAccessException("not owner of workflow: " + workflowId);
        }
    }

    public void assertReadableWorkflow(String userId, long workflowId) {
        Workflow workflow = workflowVersionService.findWorkflow(workflowId);
        if (workflow == null) {
            throw new IllegalArgumentException("workflow not found: " + workflowId);
        }
        if (isPublic(workflow.getVisibility())
                || Objects.equals(empty(workflow.getOwnerUserId()), empty(userId))) {
            return;
        }
        throw new PlatformAccessException("not allowed to read private workflow: " + workflowId);
    }

    private static String empty(String s) {
        return s == null ? "" : s;
    }

    private static boolean isPublic(String visibility) {
        return "public".equalsIgnoreCase(empty(visibility).trim());
    }
}
