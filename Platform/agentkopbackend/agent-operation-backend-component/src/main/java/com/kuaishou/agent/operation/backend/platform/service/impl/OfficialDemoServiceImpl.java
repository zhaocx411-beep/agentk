package com.kuaishou.agent.operation.backend.platform.service.impl;

import java.util.List;
import java.util.Optional;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityViewDto;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.AgentMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.AgentVersionMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.WorkflowMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.WorkflowVersionMapper;
import com.kuaishou.agent.operation.backend.platform.dto.OfficialDefaultCatalogView;
import com.kuaishou.agent.operation.backend.platform.dto.OfficialDefaultCatalogView.OfficialAgentLoopItem;
import com.kuaishou.agent.operation.backend.platform.dto.OfficialDefaultCatalogView.OfficialWorkflowAgentItem;
import com.kuaishou.agent.operation.backend.platform.dto.OfficialToolPlazaView;
import com.kuaishou.agent.operation.backend.platform.entity.Agent;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.entity.Workflow;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;
import com.kuaishou.agent.operation.backend.platform.service.AgentVersionService;
import com.kuaishou.agent.operation.backend.platform.service.OfficialDemoService;
import com.kuaishou.agent.operation.backend.platform.support.ChatSessionKinds;
import com.kuaishou.agent.operation.backend.platform.support.OfficialDemoAssets;
import com.kuaishou.agent.operation.backend.platform.support.WorkflowPipelineRefs;

/**
 * 官方示范资产查询：仅读 is_default=1 的 active 行。
 */
@Service
public class OfficialDemoServiceImpl implements OfficialDemoService {

    private static final Logger LOGGER = LoggerFactory.getLogger(OfficialDemoServiceImpl.class);

    private final AgentMapper agentMapper;
    private final AgentVersionMapper agentVersionMapper;
    private final WorkflowMapper workflowMapper;
    private final WorkflowVersionMapper workflowVersionMapper;
    private final ObjectMapper objectMapper;

    public OfficialDemoServiceImpl(AgentMapper agentMapper, AgentVersionMapper agentVersionMapper,
            WorkflowMapper workflowMapper, WorkflowVersionMapper workflowVersionMapper,
            ObjectMapper objectMapper) {
        this.agentMapper = agentMapper;
        this.agentVersionMapper = agentVersionMapper;
        this.workflowMapper = workflowMapper;
        this.workflowVersionMapper = workflowVersionMapper;
        this.objectMapper = objectMapper;
    }

    @Override
    public List<Agent> listDefaultAgents() {
        return agentMapper.selectList(new LambdaQueryWrapper<Agent>()
                .eq(Agent::getIsDefault, true)
                .eq(Agent::getStatus, "active")
                .orderByAsc(Agent::getId));
    }

    @Override
    public Optional<AgentVersion> findPublishedVersion(Agent agent) {
        if (agent == null || agent.getId() == null) {
            return Optional.empty();
        }
        Integer latest = agent.getLatestVersion();
        if (latest != null && latest > 0) {
            AgentVersion byLatest = agentVersionMapper.selectOne(new LambdaQueryWrapper<AgentVersion>()
                    .eq(AgentVersion::getAgentId, agent.getId())
                    .eq(AgentVersion::getVersion, latest)
                    .eq(AgentVersion::getStatus, "published")
                    .last("LIMIT 1"));
            if (byLatest != null) {
                return Optional.of(byLatest);
            }
        }
        return Optional.ofNullable(agentVersionMapper.selectOne(new LambdaQueryWrapper<AgentVersion>()
                .eq(AgentVersion::getAgentId, agent.getId())
                .eq(AgentVersion::getStatus, "published")
                .orderByDesc(AgentVersion::getVersion)
                .last("LIMIT 1")));
    }

    @Override
    public Optional<AgentVersion> findDefaultPublishedAgentVersion(Long agentLoopId, String agentLoopName) {
        if (agentLoopId != null && agentLoopId > 0) {
            Agent agent = agentMapper.selectById(agentLoopId);
            if (agent == null || !Boolean.TRUE.equals(agent.getIsDefault())) {
                return Optional.empty();
            }
            return findPublishedVersion(agent);
        }
        if (StringUtils.hasText(agentLoopName)) {
            String name = agentLoopName.trim();
            for (Agent agent : listDefaultAgents()) {
                if (name.equals(agent.getName())) {
                    return findPublishedVersion(agent);
                }
            }
            return Optional.empty();
        }
        for (Agent agent : listDefaultAgents()) {
            Optional<AgentVersion> version = findPublishedVersion(agent);
            if (version.isPresent()) {
                return version;
            }
        }
        return Optional.empty();
    }

    @Override
    public List<Workflow> listDefaultWorkflows() {
        return workflowMapper.selectList(new LambdaQueryWrapper<Workflow>()
                .eq(Workflow::getIsDefault, true)
                .eq(Workflow::getStatus, "active")
                .orderByAsc(Workflow::getId));
    }

    @Override
    public Optional<WorkflowVersion> findPublishedVersion(Workflow workflow) {
        if (workflow == null || workflow.getId() == null) {
            return Optional.empty();
        }
        Integer latest = workflow.getLatestVersion();
        if (latest != null && latest > 0) {
            WorkflowVersion byLatest = workflowVersionMapper.selectOne(new LambdaQueryWrapper<WorkflowVersion>()
                    .eq(WorkflowVersion::getWorkflowId, workflow.getId())
                    .eq(WorkflowVersion::getVersion, latest)
                    .eq(WorkflowVersion::getStatus, "published")
                    .last("LIMIT 1"));
            if (byLatest != null) {
                return Optional.of(byLatest);
            }
        }
        return Optional.ofNullable(workflowVersionMapper.selectOne(new LambdaQueryWrapper<WorkflowVersion>()
                .eq(WorkflowVersion::getWorkflowId, workflow.getId())
                .eq(WorkflowVersion::getStatus, "published")
                .orderByDesc(WorkflowVersion::getVersion)
                .last("LIMIT 1")));
    }

    @Override
    public Optional<WorkflowVersion> findDefaultPublishedWorkflowVersion(Long workflowId, String workflowKey) {
        if (workflowId != null && workflowId > 0) {
            Workflow wf = workflowMapper.selectById(workflowId);
            if (wf == null || !Boolean.TRUE.equals(wf.getIsDefault())) {
                return Optional.empty();
            }
            return findPublishedVersion(wf);
        }
        if (!StringUtils.hasText(workflowKey)) {
            return Optional.empty();
        }
        String key = workflowKey.trim();
        for (Workflow wf : listDefaultWorkflows()) {
            Optional<WorkflowVersion> published = findPublishedVersion(wf);
            if (!published.isPresent()) {
                continue;
            }
            WorkflowVersion version = published.get();
            if (WorkflowPipelineRefs.matchesWorkflowKey(key, version.getDefinitionJson(),
                    wf.getAgentkId(), wf.getName(), objectMapper)) {
                return published;
            }
        }
        return Optional.empty();
    }

    @Override
    public OfficialDefaultCatalogView defaultCatalog() {
        OfficialDefaultCatalogView view = new OfficialDefaultCatalogView();

        Long defaultAgentLoopVersionId = null;
        for (Agent agent : listDefaultAgents()) {
            AgentVersion agentVersion = findPublishedVersion(agent).orElse(null);
            if (agentVersion == null) {
                continue;
            }
            OfficialAgentLoopItem item = new OfficialAgentLoopItem();
            item.setKind(ChatSessionKinds.AGENT_LOOP);
            item.setAgentLoopId(agent.getId());
            item.setName(agent.getName());
            item.setDescription(agent.getDescription());
            item.setAgentLoopVersionId(agentVersion.getId());
            item.setVersion(agentVersion.getVersion());
            item.setProfileVersionLabel(AgentVersionService.resolveProfileVersionLabel(agentVersion));
            item.setQuickMode(OfficialDemoAssets.MODE_DEFAULT_AGENT);
            item.setQuickPath("sessions/quick/agent-loops/" + agent.getId());
            view.getAgentLoops().add(item);
            if (defaultAgentLoopVersionId == null) {
                defaultAgentLoopVersionId = agentVersion.getId();
            }
        }

        List<Workflow> workflows = listDefaultWorkflows();
        for (Workflow workflow : workflows) {
            WorkflowVersion version = findPublishedVersion(workflow).orElse(null);
            if (version == null) {
                continue;
            }
            OfficialWorkflowAgentItem item = new OfficialWorkflowAgentItem();
            item.setKind(ChatSessionKinds.WORKFLOW);
            item.setWorkflowId(workflow.getId());
            item.setName(workflow.getName());
            item.setDescription(workflow.getDescription());
            item.setWorkflowVersionId(version.getId());
            item.setVersion(version.getVersion());
            fillWorkflowAgentFromDefinition(item, workflow, version);
            if (!StringUtils.hasText(item.getWorkflowKey())) {
                item.setWorkflowKey(workflow.getName());
            }
            item.setAgentLoopVersionId(defaultAgentLoopVersionId);
            item.setQuickMode(OfficialDemoAssets.MODE_DEFAULT_WORKFLOW);
            item.setQuickPath("sessions/quick/workflows/" + encodeWorkflowKeyForPath(item.getWorkflowKey()));
            view.getWorkflowAgents().add(item);
        }

        if (view.getAgentLoops().isEmpty() && view.getWorkflowAgents().isEmpty()) {
            throw new IllegalArgumentException(
                    "官方默认资产未配置，请先执行 docs/sql/aop_platform_default_assets_seed.sql");
        }
        return view;
    }

    @Override
    public OfficialToolPlazaView toolPlaza(List<AgentKCapabilityViewDto> capabilities, String loadError) {
        if (loadError != null) {
            LOGGER.warn("official tools: load capabilities failed, return empty plaza: {}", loadError);
            return OfficialToolPlazaView.empty();
        }
        OfficialToolPlazaView fromCaps = OfficialToolPlazaView.fromActivityCapabilities(capabilities);
        if (fromCaps.getTotal() > 0) {
            return fromCaps;
        }
        LOGGER.info("official tools: no capabilities from agent-k/db, return empty plaza");
        return OfficialToolPlazaView.empty();
    }

    private void fillWorkflowAgentFromDefinition(OfficialWorkflowAgentItem item, Workflow workflow,
            WorkflowVersion version) {
        String agentkId = emptyToNull(workflow.getAgentkId());
        if (agentkId != null) {
            item.setAgentkId(agentkId);
            item.setWorkflowKey(agentkId);
        }

        String definitionJson = version.getDefinitionJson();
        String capability = WorkflowPipelineRefs.extractCapability(definitionJson, objectMapper);
        if (StringUtils.hasText(capability)) {
            item.setPipelineRef(capability);
        }
        String definitionName = extractDefinitionName(definitionJson);
        if (StringUtils.hasText(definitionName)) {
            item.setPipelineName(definitionName);
        }
        if (!StringUtils.hasText(item.getWorkflowKey())) {
            item.setWorkflowKey(WorkflowPipelineRefs.resolveCatalogWorkflowKey(
                    definitionJson, workflow.getAgentkId(), workflow.getName(), objectMapper));
        }
        if (!StringUtils.hasText(item.getPipelineName())) {
            item.setPipelineName(workflow.getName());
        }
        if (!StringUtils.hasText(item.getAgentkId()) && StringUtils.hasText(item.getPipelineRef())) {
            String pipelineRef = item.getPipelineRef();
            item.setAgentkId(pipelineRef.startsWith("pipeline:")
                    ? pipelineRef.substring("pipeline:".length())
                    : pipelineRef);
        }
    }

    private String extractDefinitionName(String definitionJson) {
        if (!StringUtils.hasText(definitionJson)) {
            return null;
        }
        try {
            JsonNode root = objectMapper.readTree(definitionJson);
            String name = root.path("name").asText("").trim();
            return StringUtils.hasText(name) ? name : null;
        } catch (Exception e) {
            LOGGER.warn("parse workflow definition_json.name failed: {}", e.getMessage());
            return null;
        }
    }

    private static String emptyToNull(String value) {
        return StringUtils.hasText(value) ? value.trim() : null;
    }

    private static String encodeWorkflowKeyForPath(String workflowKey) {
        if (!StringUtils.hasText(workflowKey)) {
            return "";
        }
        try {
            return java.net.URLEncoder.encode(workflowKey.trim(), "UTF-8")
                    .replace("+", "%20");
        } catch (java.io.UnsupportedEncodingException e) {
            return workflowKey.trim();
        }
    }
}
