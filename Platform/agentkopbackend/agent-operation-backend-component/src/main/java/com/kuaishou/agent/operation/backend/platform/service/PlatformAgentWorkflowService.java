package com.kuaishou.agent.operation.backend.platform.service;

import java.util.List;
import java.util.stream.Collectors;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;

import com.baomidou.mybatisplus.core.metadata.IPage;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;
import com.kuaishou.agent.operation.backend.platform.dto.SaveAgentLoopVersionRequest;
import com.kuaishou.agent.operation.backend.platform.dto.SessionProfileTemplateView;
import com.kuaishou.agent.operation.backend.platform.entity.Agent;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.entity.Workflow;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;
import com.kuaishou.agent.operation.backend.platform.support.PlatformBadRequestException;

/**
 * AgentLoop / Workflow 版本与 Session Profile 模板。
 */
@Service
public class PlatformAgentWorkflowService {

    private static final Logger LOGGER = LoggerFactory.getLogger(PlatformAgentWorkflowService.class);

    private final AgentVersionService agentVersionService;
    private final WorkflowVersionService workflowVersionService;
    private final PlatformAssetAccessService assetAccessService;
    private final SessionProfileTemplateService sessionProfileTemplateService;
    private final ObjectMapper objectMapper;

    public PlatformAgentWorkflowService(AgentVersionService agentVersionService,
            WorkflowVersionService workflowVersionService,
            PlatformAssetAccessService assetAccessService,
            SessionProfileTemplateService sessionProfileTemplateService,
            ObjectMapper objectMapper) {
        this.agentVersionService = agentVersionService;
        this.workflowVersionService = workflowVersionService;
        this.assetAccessService = assetAccessService;
        this.sessionProfileTemplateService = sessionProfileTemplateService;
        this.objectMapper = objectMapper;
    }

    public SessionProfileTemplateView pipelineOnlyProfileTemplate(String userId, Long workflowVersionId,
            String agentkId) {
        if (workflowVersionId != null && workflowVersionId > 0) {
            assetAccessService.assertReadableWorkflowVersion(userId, workflowVersionId);
        }
        return sessionProfileTemplateService.pipelineOnly(workflowVersionId, agentkId);
    }

    public SessionProfileTemplateView agentLoopDefaultProfileTemplate() {
        return sessionProfileTemplateService.agentLoopOfficialDefault();
    }

    public Agent createAgentLoop(String userId, String name, String description, String tenantId) {
        Agent agent = agentVersionService.createAgent(name, description, userId, empty(tenantId));
        LOGGER.info("platform createAgentLoop userId={} agentLoopId={} name={} createdAt={}",
                userId, agent.getId(), agent.getName(), agent.getCreatedAt());
        return agent;
    }

    public PageResult<Agent> listAgentLoops(String userId, Integer pageNum, Integer pageSize) {
        IPage<Agent> page = agentVersionService.pageMine(
                userId,
                pageNum == null ? 1L : pageNum.longValue(),
                pageSize == null ? 20L : pageSize.longValue());
        return PageResult.of(page.getCurrent(), page.getSize(), page.getTotal(), page.getRecords());
    }

    public AgentVersion saveAgentLoopVersion(String userId, long agentLoopId,
            SaveAgentLoopVersionRequest saveAgentLoopVersionRequest) {
        if (saveAgentLoopVersionRequest == null
                || saveAgentLoopVersionRequest.getProfileYaml() == null
                || saveAgentLoopVersionRequest.getProfileYaml().isNull()
                || !saveAgentLoopVersionRequest.getProfileYaml().isObject()) {
            throw new PlatformBadRequestException("profileYaml must be a JSON object");
        }
        String profileJson;
        try {
            profileJson = objectMapper.writeValueAsString(saveAgentLoopVersionRequest.getProfileYaml());
        } catch (Exception e) {
            throw new PlatformBadRequestException("profileYaml serialize failed: " + e.getMessage());
        }
        assetAccessService.assertReadableAgentLoop(userId, agentLoopId);
        AgentVersion version = agentVersionService.saveDraft(agentLoopId, profileJson, userId);
        LOGGER.info("platform saveAgentLoopVersion userId={} agentLoopId={} versionId={} version={} "
                        + "createdBy={} createdAt={} profileVersionLabel={}",
                userId, agentLoopId, version.getId(), version.getVersion(),
                version.getCreatedBy(), version.getCreatedAt(),
                AgentVersionService.resolveProfileVersionLabel(version));
        return version;
    }

    public AgentVersion publishAgentLoopVersion(String userId, long agentLoopVersionId) {
        assetAccessService.assertOwnAgentVersion(userId, agentLoopVersionId);
        AgentVersion version = agentVersionService.publish(agentLoopVersionId, userId);
        LOGGER.info("platform publishAgentLoopVersion userId={} agentLoopVersionId={} version={} "
                        + "publishedBy={} publishedAt={} createdBy={} createdAt={}",
                userId, version.getId(), version.getVersion(),
                version.getPublishedBy(), version.getPublishedAt(),
                version.getCreatedBy(), version.getCreatedAt());
        return version;
    }

    public AgentVersion getAgentLoopVersion(String userId, long agentLoopVersionId) {
        assetAccessService.assertReadableAgentVersion(userId, agentLoopVersionId);
        return agentVersionService.findVersion(agentLoopVersionId);
    }

    public List<AgentVersion> listAgentLoopVersions(String userId, long agentLoopId, String status) {
        assetAccessService.assertReadableAgentLoop(userId, agentLoopId);
        List<AgentVersion> versions = agentVersionService.listVersions(agentLoopId);
        if (status != null && !status.trim().isEmpty()) {
            String want = status.trim().toLowerCase();
            if (!"draft".equals(want) && !"published".equals(want) && !"deprecated".equals(want)) {
                throw new PlatformBadRequestException("status must be draft, published or deprecated");
            }
            versions = versions.stream()
                    .filter(v -> want.equalsIgnoreCase(v.getStatus()))
                    .collect(Collectors.toList());
        }
        return versions;
    }

    public Workflow createWorkflow(String userId, String name, String description, String tenantId) {
        Workflow workflow = workflowVersionService.createWorkflow(name, description, userId, empty(tenantId));
        LOGGER.info("platform createWorkflow userId={} workflowId={} name={} createdAt={}",
                userId, workflow.getId(), workflow.getName(), workflow.getCreatedAt());
        return workflow;
    }

    public List<Workflow> listWorkflows(String userId) {
        return workflowVersionService.listMine(userId);
    }

    public WorkflowVersion saveWorkflowVersion(String userId, long workflowId, JsonNode body) {
        JsonNode definition = extractDefinitionJson(body);
        if (definition == null || definition.isNull() || !definition.isObject()) {
            throw new PlatformBadRequestException(
                    "definitionJson must be a JSON object, e.g. {\"definitionJson\":{\"name\":\"storyboard_film\",\"nodes\":{}}}");
        }
        String definitionText;
        try {
            definitionText = objectMapper.writeValueAsString(definition);
        } catch (Exception e) {
            throw new PlatformBadRequestException("definitionJson serialize failed: " + e.getMessage());
        }
        assetAccessService.assertReadableWorkflow(userId, workflowId);
        WorkflowVersion version = workflowVersionService.saveDraft(workflowId, definitionText, userId);
        LOGGER.info("platform saveWorkflowVersion userId={} workflowId={} versionId={} version={} "
                        + "createdBy={} createdAt={}",
                userId, workflowId, version.getId(), version.getVersion(),
                version.getCreatedBy(), version.getCreatedAt());
        return version;
    }

    public WorkflowVersion publishWorkflowVersion(String userId, long workflowVersionId) {
        WorkflowVersion version = workflowVersionService.publish(workflowVersionId, userId);
        LOGGER.info("platform publishWorkflowVersion userId={} workflowVersionId={} version={} "
                        + "publishedBy={} publishedAt={} createdBy={} createdAt={}",
                userId, version.getId(), version.getVersion(),
                version.getPublishedBy(), version.getPublishedAt(),
                version.getCreatedBy(), version.getCreatedAt());
        return version;
    }

    public List<WorkflowVersion> listWorkflowVersions(String userId, long workflowId) {
        assetAccessService.assertReadableWorkflow(userId, workflowId);
        return workflowVersionService.listVersions(workflowId);
    }

    private JsonNode extractDefinitionJson(JsonNode body) {
        if (body == null || body.isNull() || !body.isObject()) {
            return null;
        }
        if (body.has("definitionJson")) {
            JsonNode nested = body.get("definitionJson");
            if (nested != null && nested.isTextual()) {
                String text = nested.asText();
                if (text == null || text.trim().isEmpty()) {
                    return null;
                }
                try {
                    return objectMapper.readTree(text);
                } catch (Exception e) {
                    throw new IllegalArgumentException("definitionJson string is not valid JSON: " + e.getMessage());
                }
            }
            return nested;
        }
        if (body.has("name") || body.has("nodes") || body.has("workflows")) {
            return body;
        }
        return null;
    }

    private static String empty(String s) {
        return s == null ? "" : s;
    }
}
