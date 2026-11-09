package com.kuaishou.agent.operation.backend.platform.service;

import java.util.Collections;

import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.AgentKPipelineBindingLookup;
import com.kuaishou.agent.operation.backend.agentk.AgentKProfileOutbound;
import com.kuaishou.agent.operation.backend.agentk.AgentKToolCapabilityLookup;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKPipelineBindingDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionProfileDto.PipelineEntry;
import com.kuaishou.agent.operation.backend.platform.dto.SessionProfileTemplateView;
import com.kuaishou.agent.operation.backend.platform.entity.Workflow;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;
import com.kuaishou.agent.operation.backend.platform.support.OfficialDefaultSessionProfile;
import com.kuaishou.agent.operation.backend.platform.support.PipelineOnlySessionProfile;

/**
 * Session Profile 模板：供前端新建 Agent / 单独跑 Pipeline 预填；建会话链路复用同一套结构。
 */
@Service
public class SessionProfileTemplateService {

    private final ObjectMapper objectMapper;
    private final WorkflowVersionService workflowVersionService;
    private final AgentKPipelineBindingLookup pipelineBindingLookup;
    private final AgentKToolCapabilityLookup toolCapabilityLookup;

    public SessionProfileTemplateService(ObjectMapper objectMapper,
            WorkflowVersionService workflowVersionService,
            AgentKPipelineBindingLookup pipelineBindingLookup,
            AgentKToolCapabilityLookup toolCapabilityLookup) {
        this.objectMapper = objectMapper;
        this.workflowVersionService = workflowVersionService;
        this.pipelineBindingLookup = pipelineBindingLookup;
        this.toolCapabilityLookup = toolCapabilityLookup;
    }

    public SessionProfileTemplateView agentLoopOfficialDefault() {
        SessionProfileTemplateView view = new SessionProfileTemplateView();
        view.setTemplateKey("agent_loop_official_default");
        view.setProfileVersion(OfficialDefaultSessionProfile.PROFILE_VERSION_LABEL);
        view.setDescription("官方 AgentLoop 体验默认 Profile（tools 来自 capability 缓存 activity；pipelines 为空）");
        view.setPersistedOnCreateSession(true);
        view.setProfile(OfficialDefaultSessionProfile.toJsonNode(objectMapper, toolCapabilityLookup));
        return view;
    }

    /**
     * Pipeline-only 模板。可选挂一条已发布 workflow / capability，便于前端直接建会话。
     */
    public SessionProfileTemplateView pipelineOnly(Long workflowVersionId, String agentkId) {
        JsonNode profile = PipelineOnlySessionProfile.toJsonNode(objectMapper);
        if (workflowVersionId != null && workflowVersionId > 0) {
            WorkflowVersion version = workflowVersionService.requirePublished(workflowVersionId);
            Workflow workflow = workflowVersionService.findWorkflow(version.getWorkflowId());
            String fallbackName = workflow != null && StringUtils.hasText(workflow.getName())
                    ? workflow.getName()
                    : "workflow_" + version.getWorkflowId();
            String definitionJson = version.getDefinitionJson();
            profile = AgentKProfileOutbound.mergeWorkflowVersion(
                    profile, fallbackName, definitionJson, objectMapper);
        } else if (StringUtils.hasText(agentkId)) {
            profile = mergeCapabilityPipeline(profile, agentkId.trim());
        }

        SessionProfileTemplateView view = new SessionProfileTemplateView();
        view.setTemplateKey(PipelineOnlySessionProfile.TEMPLATE_KEY);
        view.setProfileVersion(PipelineOnlySessionProfile.PROFILE_VERSION_LABEL);
        view.setDescription("Pipeline 草稿模板（tools=[]；可按 workflowVersionId/agentkId 预填 pipelines）");
        view.setPersistedOnCreateSession(true);
        view.setProfile(profile);
        return view;
    }

    private JsonNode mergeCapabilityPipeline(JsonNode base, String agentkId) {
        AgentKPipelineBindingDto binding = pipelineBindingLookup.pipelineBindings(
                Collections.singletonList(agentkId)).get(agentkId);
        if (binding == null || !StringUtils.hasText(binding.getPipelineRef())
                || !StringUtils.hasText(binding.getPipelineName())) {
            throw new IllegalArgumentException("pipeline capability not found or incomplete: " + agentkId);
        }
        PipelineEntry entry = new PipelineEntry();
        String pipelineRef = binding.getPipelineRef().trim();
        entry.setCapability(pipelineRef.startsWith("pipeline:")
                ? pipelineRef.substring("pipeline:".length()).trim()
                : pipelineRef);
        entry.setName(binding.getPipelineName().trim());
        return PipelineOnlySessionProfile.toJsonNode(objectMapper, Collections.singletonList(entry));
    }
}
