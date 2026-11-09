package com.kuaishou.agent.operation.backend.platform.service;

import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.platform.dto.ChatSessionDetailView;
import com.kuaishou.agent.operation.backend.platform.dto.ChatSessionDetailView.VersionBindingView;
import com.kuaishou.agent.operation.backend.platform.entity.Agent;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.entity.Workflow;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;

@Component
public class ChatSessionDetailAssembler {

    private final AgentVersionService agentVersionService;
    private final WorkflowVersionService workflowVersionService;

    public ChatSessionDetailAssembler(AgentVersionService agentVersionService,
            WorkflowVersionService workflowVersionService) {
        this.agentVersionService = agentVersionService;
        this.workflowVersionService = workflowVersionService;
    }

    public ChatSessionDetailView toSessionDetail(ChatSession session) {
        ChatSessionDetailView view = new ChatSessionDetailView();
        view.setId(session.getId());
        view.setPublicId(session.getPublicId());
        view.setUserId(session.getUserId());
        view.setTenantId(session.getTenantId());
        view.setAgentkSessionId(session.getAgentkSessionId());
        view.setTitle(session.getTitle());
        view.setStatus(session.getStatus());
        String lastExecutionId = StringUtils.hasText(session.getLastExecutionId())
                ? session.getLastExecutionId() : session.getLastAgentkRunId();
        view.setLastAgentkRunId(lastExecutionId);
        view.setLastExecutionId(lastExecutionId);
        view.setLastEventSeq(session.getLastEventSeq());
        view.setActiveRunStatus(session.getActiveRunStatus());
        view.setProfileVersion(session.getProfileVersion());
        view.setEffectiveModel(session.getEffectiveModel());
        view.setEffectiveToolsJson(session.getEffectiveToolsJson());
        view.setEffectiveToolModelsJson(session.getEffectiveToolModelsJson());
        view.setConfigSource(session.getConfigSource());
        view.setRejectReason(session.getRejectReason());
        view.setSessionKind(session.getSessionKind());
        view.setCreatedAt(session.getCreatedAt());
        view.setUpdatedAt(session.getUpdatedAt());

        if (session.getAgentVersionId() != null) {
            AgentVersion av = agentVersionService.findVersion(session.getAgentVersionId());
            if (av != null) {
                VersionBindingView binding = new VersionBindingView();
                binding.setVersionId(av.getId());
                binding.setParentId(av.getAgentId());
                binding.setVersion(av.getVersion());
                binding.setStatus(av.getStatus());
                binding.setProfileVersionLabel(AgentVersionService.resolveProfileVersionLabel(av));
                binding.setContentHash(av.getContentHash());
                binding.setPublishedAt(av.getPublishedAt());
                binding.setPublishedBy(av.getPublishedBy());
                Agent agent = agentVersionService.findAgent(av.getAgentId());
                if (agent != null) {
                    binding.setName(agent.getName());
                }
                view.setAgentLoopVersion(binding);
            }
        }
        if (session.getWorkflowVersionId() != null) {
            WorkflowVersion wv = workflowVersionService.findVersion(session.getWorkflowVersionId());
            if (wv != null) {
                VersionBindingView binding = new VersionBindingView();
                binding.setVersionId(wv.getId());
                binding.setParentId(wv.getWorkflowId());
                binding.setVersion(wv.getVersion());
                binding.setStatus(wv.getStatus());
                binding.setContentHash(wv.getContentHash());
                binding.setPublishedAt(wv.getPublishedAt());
                binding.setPublishedBy(wv.getPublishedBy());
                Workflow workflow = workflowVersionService.findWorkflow(wv.getWorkflowId());
                if (workflow != null) {
                    binding.setName(workflow.getName());
                }
                view.setWorkflowVersion(binding);
            }
        }
        return view;
    }
}
