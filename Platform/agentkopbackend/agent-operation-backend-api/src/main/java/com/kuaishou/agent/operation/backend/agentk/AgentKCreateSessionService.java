package com.kuaishou.agent.operation.backend.agentk;

import java.util.Map;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCreateSessionRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionDto;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.entity.Workflow;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;
import com.kuaishou.agent.operation.backend.platform.service.AgentVersionService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.CreateSessionBegin;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.EffectiveSnapshot;
import com.kuaishou.agent.operation.backend.platform.service.WorkflowVersionService;
import com.kuaishou.agent.operation.backend.platform.support.ChatEntrySources;
import com.kuaishou.agent.operation.backend.platform.support.ChatSessionKinds;
import com.kuaishou.agent.operation.backend.platform.support.ChatSessionTitles;
import com.kuaishou.agent.operation.backend.platform.support.OfficialDefaultSessionProfile;
import com.kuaishou.agent.operation.backend.platform.support.PipelineOnlySessionProfile;

/**
 * 建会话编排：供 {@code POST /agentk/sessions} 与官方 quick 共用，避免 Controller 互注入。
 */
@Service
public class AgentKCreateSessionService {

    private static final Logger LOGGER = LoggerFactory.getLogger(AgentKCreateSessionService.class);

    private final AgentKClient agentKClient;
    private final AgentKIdentityResolver identityResolver;
    private final AgentKMessageMapper messageMapper;
    private final ChatPersistService chatPersistService;
    private final AgentVersionService agentVersionService;
    private final WorkflowVersionService workflowVersionService;
    private final ObjectMapper objectMapper;
    private final AgentKCapabilityCacheService capabilityCacheService;

    public AgentKCreateSessionService(AgentKClient agentKClient,
            AgentKIdentityResolver identityResolver,
            AgentKMessageMapper messageMapper,
            ChatPersistService chatPersistService,
            AgentVersionService agentVersionService,
            WorkflowVersionService workflowVersionService,
            ObjectMapper objectMapper,
            AgentKCapabilityCacheService capabilityCacheService) {
        this.agentKClient = agentKClient;
        this.identityResolver = identityResolver;
        this.messageMapper = messageMapper;
        this.chatPersistService = chatPersistService;
        this.agentVersionService = agentVersionService;
        this.workflowVersionService = workflowVersionService;
        this.objectMapper = objectMapper;
        this.capabilityCacheService = capabilityCacheService;
    }

    public Message<AgentKSessionDto> createSession(AgentKCreateSessionRequest createSessionRequest) {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        AgentKCreateSessionRequest sessionRequest =
                createSessionRequest != null ? createSessionRequest : new AgentKCreateSessionRequest();
        sessionRequest.setTitle(ChatSessionTitles.resolveCreateTitle(
                sessionRequest.getTitle(), sessionRequest.getText()));
        LOGGER.info("createSession request userId={} agentLoopVersionId={} workflowVersionId={} title={} hasText={} hasProfile={}",
                identity.getUserId(), sessionRequest.getAgentLoopVersionId(),
                sessionRequest.getWorkflowVersionId(), sessionRequest.getTitle(),
                StringUtils.hasText(sessionRequest.getText()),
                hasProfile(sessionRequest));

        // 默认 Profile / name-only 工具补 capability 都依赖 aop_agentk_capability 缓存
        capabilityCacheService.warmupIfEmpty(identity);

        EffectiveSnapshot effective;
        try {
            effective = applyPublishedAgentLoopVersion(sessionRequest);
            applyPublishedWorkflowVersion(sessionRequest);
            effective.setSessionKind(ChatSessionKinds.resolve(
                    sessionRequest.getSessionKind(),
                    sessionRequest.getAgentLoopVersionId(),
                    sessionRequest.getWorkflowVersionId()));
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.BAD_REQUEST, e.getMessage());
        }
        if (StringUtils.hasText(sessionRequest.getEntrySource())) {
            effective.setEntrySource(sessionRequest.getEntrySource().trim());
        } else if (!StringUtils.hasText(effective.getEntrySource())) {
            effective.setEntrySource(ChatEntrySources.ONLINE);
        }

        Map<String, Object> outbound;
        try {
            outbound = AgentKProfileOutbound.toCreateSessionBody(
                    sessionRequest.getTitle(), sessionRequest.getProfile(), objectMapper, capabilityCacheService);
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.BAD_REQUEST, e.getMessage());
        }
        logCreateSessionOutbound(identity, sessionRequest, outbound, effective);

        CreateSessionBegin begin = null;
        try {
            begin = chatPersistService.beginCreateSession(
                    identity.getUserId(),
                    "",
                    sessionRequest.getTitle(),
                    sessionRequest.getAgentLoopVersionId(),
                    sessionRequest.getWorkflowVersionId(),
                    writeJson(snapshotSpecFromProfile(sessionRequest.getProfile())),
                    writeJson(sessionRequest.getProfile()),
                    effective);
        } catch (Exception e) {
            LOGGER.warn("platform persist beginCreateSession failed", e);
        }

        ResponseEntity<?> upstream = agentKClient.exchangeJson("POST", "/sessions", null, outbound,
                AgentKSessionDto.class, identity);
        Message<AgentKSessionDto> createSessionResponse = messageMapper.toMessage(upstream, AgentKSessionDto.class);

        if (begin != null) {
            try {
                boolean ok = isCreateSessionOk(createSessionResponse);
                chatPersistService.completeCreateSession(begin,
                        ok ? createSessionResponse.getData().getId() : null,
                        effective.getProfileVersion(),
                        ok,
                        upstream.getStatusCodeValue(),
                        ok ? null : createSessionResponse.getMessage());
            } catch (Exception e) {
                LOGGER.warn("platform persist completeCreateSession failed", e);
            }
        }
        LOGGER.info("createSession response userId={} httpStatus={} ok={} agentkSessionId={} platformSessionId={}",
                identity.getUserId(), upstream.getStatusCodeValue(), isOk(createSessionResponse),
                createSessionResponse.getData() == null ? null : createSessionResponse.getData().getId(),
                begin == null || begin.getSession() == null ? null : begin.getSession().getId());
        return createSessionResponse;
    }

    private void logCreateSessionOutbound(AgentKOutboundIdentity identity,
            AgentKCreateSessionRequest sessionRequest, Map<String, Object> outbound,
            EffectiveSnapshot effective) {
        LOGGER.info("createSession outbound userId={} title={} profileKeys={} configSource={} sessionKind={} profileVersion={} toolCount={}",
                identity.getUserId(), sessionRequest.getTitle(),
                outbound.get("profile") instanceof Map ? ((Map<?, ?>) outbound.get("profile")).keySet() : "-",
                effective.getConfigSource(), effective.getSessionKind(), effective.getProfileVersion(),
                sessionRequest.getProfile() != null && sessionRequest.getProfile().path("tools").isArray()
                        ? sessionRequest.getProfile().path("tools").size() : 0);
        if ("official_default_profile".equals(effective.getConfigSource())) {
            LOGGER.warn("【建会话·Profile 兜底】configSource=official_default_profile，"
                    + "当前使用的是 capability 缓存 activity 默认 Profile，不是 aop_agent_version。");
        } else if (sessionRequest.getProfile() != null
                && sessionRequest.getProfile().path("tools").isArray()
                && sessionRequest.getProfile().path("tools").size() == 0
                && sessionRequest.getAgentLoopVersionId() != null
                && !ChatSessionKinds.WORKFLOW.equals(effective.getSessionKind())) {
            LOGGER.warn("【建会话·空 tools】agentLoopVersionId={} 但出站 profile.tools 为空数组。",
                    sessionRequest.getAgentLoopVersionId());
        }
    }

    private static boolean isCreateSessionOk(Message<AgentKSessionDto> message) {
        return message != null
                && message.getCode() == ResultCode.SUCCESS.getCode()
                && message.getData() != null
                && StringUtils.hasText(message.getData().getId());
    }

    private EffectiveSnapshot applyPublishedAgentLoopVersion(AgentKCreateSessionRequest sessionRequest) {
        EffectiveSnapshot effective = new EffectiveSnapshot();
        if (sessionRequest.getAgentLoopVersionId() != null) {
            AgentVersion version = agentVersionService.requirePublished(sessionRequest.getAgentLoopVersionId());
            if (!hasProfile(sessionRequest)) {
                sessionRequest.setProfile(AgentKProfileOutbound.yamlOrJsonToJsonNode(
                        version.getProfileYaml(), objectMapper, capabilityCacheService));
            }
            effective.setProfileVersion(AgentVersionService.resolveProfileVersionLabel(version));
            effective.setConfigSource("agent_version");
            int toolCount = sessionRequest.getProfile() != null
                    && sessionRequest.getProfile().path("tools").isArray()
                    ? sessionRequest.getProfile().path("tools").size() : 0;
            LOGGER.info("【建会话·使用 AgentVersion Profile】agentLoopVersionId={}，versionId={}，tools 数量={}，"
                            + "profileVersionLabel={}",
                    sessionRequest.getAgentLoopVersionId(), version.getId(), toolCount,
                    effective.getProfileVersion());
        } else if (hasProfile(sessionRequest)) {
            sessionRequest.setProfile(objectMapper.valueToTree(
                    AgentKProfileOutbound.toAgentkProfileJson(
                            sessionRequest.getProfile(), objectMapper, capabilityCacheService)));
            effective.setConfigSource("raw_profile");
            String profileText = writeJson(sessionRequest.getProfile());
            effective.setProfileVersion(
                    AgentVersionService.extractProfileVersionLabel(profileText == null ? "" : profileText, ""));
        } else if (sessionRequest.getWorkflowVersionId() != null) {
            sessionRequest.setProfile(PipelineOnlySessionProfile.toJsonNode(objectMapper));
            effective.setConfigSource("pipeline_only_default");
            effective.setProfileVersion(PipelineOnlySessionProfile.PROFILE_VERSION_LABEL);
            LOGGER.info("【建会话·Pipeline-only 默认 Profile】仅 pin workflowVersionId={}，tools=[]，"
                            + "随后 merge 已发布 workflow 的 pipelines。title={}",
                    sessionRequest.getWorkflowVersionId(), sessionRequest.getTitle());
        } else {
            sessionRequest.setProfile(OfficialDefaultSessionProfile.toJsonNode(objectMapper, capabilityCacheService));
            effective.setConfigSource("official_default_profile");
            effective.setProfileVersion(OfficialDefaultSessionProfile.PROFILE_VERSION_LABEL);
            LOGGER.warn("【建会话·Profile 兜底】未指定 agentLoopVersionId 且请求无 profile，"
                            + "改用 capability 缓存 activity 列表作为官方默认 Profile。"
                            + "title={}，该会话落库 profile 不是 aop_agent_version 原文。",
                    sessionRequest.getTitle());
        }
        fillEffectiveFromProfile(effective, sessionRequest.getProfile());
        return effective;
    }

    private void fillEffectiveFromProfile(EffectiveSnapshot effective,
            com.fasterxml.jackson.databind.JsonNode profile) {
        if (profile == null || !profile.isObject()) {
            effective.setModel("");
            return;
        }
        effective.setModel(profile.path("defaultModel").asText(""));
        if (profile.has("tools")) {
            effective.setToolsJson(writeJson(profile.get("tools")));
        }
        if (profile.has("toolModels")) {
            effective.setToolModelsJson(writeJson(profile.get("toolModels")));
        }
    }

    private Map<String, Object> snapshotSpecFromProfile(com.fasterxml.jackson.databind.JsonNode profile) {
        Map<String, Object> spec = new java.util.LinkedHashMap<>();
        if (profile == null || !profile.isObject()) {
            return spec;
        }
        if (profile.hasNonNull("defaultModel") && StringUtils.hasText(profile.get("defaultModel").asText())) {
            spec.put("model", profile.get("defaultModel").asText());
        }
        if (profile.has("tools")) {
            spec.put("tools", objectMapper.convertValue(profile.get("tools"), Object.class));
        }
        if (profile.has("toolModels")) {
            spec.put("toolModels", objectMapper.convertValue(profile.get("toolModels"), Object.class));
        }
        return spec.isEmpty() ? null : spec;
    }

    private void applyPublishedWorkflowVersion(AgentKCreateSessionRequest sessionRequest) {
        if (sessionRequest.getWorkflowVersionId() == null) {
            return;
        }
        WorkflowVersion version = workflowVersionService.requirePublished(sessionRequest.getWorkflowVersionId());
        Workflow workflow = workflowVersionService.findWorkflow(version.getWorkflowId());
        String fallbackName = workflow != null && StringUtils.hasText(workflow.getName())
                ? workflow.getName()
                : "workflow_" + version.getWorkflowId();
        String definitionJson = version.getDefinitionJson();
        sessionRequest.setProfile(AgentKProfileOutbound.mergeWorkflowVersion(
                sessionRequest.getProfile(), fallbackName, definitionJson, objectMapper));
        LOGGER.info("createSession merged workflowVersionId={} workflowKeyFallback={} pipelines={}",
                version.getId(), fallbackName, sessionRequest.getProfile().path("pipelines"));
    }

    private static boolean hasProfile(AgentKCreateSessionRequest sessionRequest) {
        return sessionRequest.getProfile() != null
                && !sessionRequest.getProfile().isNull()
                && !sessionRequest.getProfile().isMissingNode()
                && sessionRequest.getProfile().isObject();
    }

    private String writeJson(Object value) {
        if (value == null) {
            return null;
        }
        try {
            return objectMapper.writeValueAsString(value);
        } catch (Exception e) {
            return null;
        }
    }

    private static boolean isOk(Message<?> message) {
        return message != null && message.getCode() == ResultCode.SUCCESS.getCode();
    }
}
