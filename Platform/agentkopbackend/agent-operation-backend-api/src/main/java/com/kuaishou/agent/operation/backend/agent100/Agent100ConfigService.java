package com.kuaishou.agent.operation.backend.agent100;

import java.util.ArrayList;
import java.util.Collections;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.agentk.AgentKCapabilityCacheService;
import com.kuaishou.agent.operation.backend.agentk.AgentKIdentityResolver;
import com.kuaishou.agent.operation.backend.agentk.AgentKOutboundIdentity;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto.AgentKCapabilityItemDto;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.agent100.Agent100Definition;
import com.kuaishou.agent.operation.backend.platform.agent100.Agent100PipelineDef;
import com.kuaishou.agent.operation.backend.platform.agent100.Agent100Registry;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100ConfigView;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100ConfigView.CapabilityStatusView;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100ConfigView.PipelineView;
import com.kuaishou.agent.operation.backend.platform.agent100.narrato.NarratoDefinition;

/**
 * Agent100 config / readiness（按注册表 {@code agentId} 组装，不绑死某一业务 Agent）。
 */
@Service
public class Agent100ConfigService {

    private final AgentKCapabilityCacheService capabilityCacheService;
    private final AgentKIdentityResolver identityResolver;

    public Agent100ConfigService(AgentKCapabilityCacheService capabilityCacheService,
            AgentKIdentityResolver identityResolver) {
        this.capabilityCacheService = capabilityCacheService;
        this.identityResolver = identityResolver;
    }

    public Message<Agent100ConfigView> config(String agentId) {
        Agent100Definition agent;
        try {
            agent = Agent100Registry.require(agentId);
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.NOT_FOUND, e.getMessage());
        }

        Agent100ConfigView view = new Agent100ConfigView();
        view.setAgentId(agent.getId());
        if (NarratoDefinition.AGENT_ID.equals(agent.getId())) {
            view.setContractVersion(NarratoDefinition.CONTRACT_VERSION);
            view.setTts(new LinkedHashMap<>(NarratoDefinition.ttsCatalog()));
        }

        Map<String, Object> models = new LinkedHashMap<>();
        models.put("defaults", agent.getModelDefaults());
        models.put("options", agent.getModelOptions());
        view.setModels(models);
        view.setVoices(new LinkedHashMap<>(agent.getVoices()));
        view.setFieldOptions(new LinkedHashMap<>(agent.getFieldOptions()));

        List<PipelineView> pipelines = new ArrayList<>();
        for (Agent100PipelineDef pipeline : agent.getPipelines()) {
            PipelineView item = new PipelineView();
            item.setName(pipeline.getName());
            item.setDisplayName(pipeline.getDisplayName());
            item.setCapability(pipeline.getCapability());
            item.setTemporalType(pipeline.getTemporalType());
            item.setContractVersion(pipeline.getContractVersion());
            item.setInputSchema(pipeline.getInputSchema());
            pipelines.add(item);
        }
        view.setPipelines(pipelines);
        view.setMediaError("");

        AgentKOutboundIdentity identity = identityResolver.resolve();
        try {
            AgentKCapabilitiesDto live = capabilityCacheService.fetchLiveCapabilities(identity);
            Set<String> found = new HashSet<>();
            if (live != null && live.getCapabilities() != null) {
                for (AgentKCapabilityItemDto item : live.getCapabilities()) {
                    if (item != null && StringUtils.hasText(item.getId())) {
                        found.add(item.getId().trim());
                    }
                }
            }
            view.setCapabilities(buildCapabilityStatus(agent, found));
            view.setCapabilityError("");
        } catch (Exception e) {
            view.setCapabilities(buildCapabilityStatus(agent, Collections.emptySet()));
            view.setCapabilityError(e.getClass().getSimpleName() + ": AgentK capability query failed");
        }
        return Message.ok(view);
    }

    private static List<CapabilityStatusView> buildCapabilityStatus(Agent100Definition agent,
            Set<String> found) {
        // 管理平台只关心本 Agent 注册的三条 Pipeline 是否 published，不返回 Core deps。
        List<CapabilityStatusView> rows = new ArrayList<>();
        for (String capabilityId : agent.capabilityIds()) {
            rows.add(status(capabilityId, found.contains(capabilityId), true, "pipeline", false));
        }
        return rows;
    }

    private static CapabilityStatusView status(String id, boolean published, boolean required,
            String group, boolean activityOnly) {
        CapabilityStatusView row = new CapabilityStatusView();
        row.setId(id);
        row.setPublished(published);
        row.setRequired(required);
        row.setGroup(group);
        row.setActivityOnly(activityOnly);
        return row;
    }
}
