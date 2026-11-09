package com.kuaishou.agent.operation.backend.agentk;

import java.time.LocalDateTime;
import java.util.Collections;
import java.util.List;
import java.util.stream.Collectors;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.metadata.IPage;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto.AgentKCapabilityItemDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCreateSessionRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionProfileDto.PipelineEntry;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKStartWorkflowRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKWorkflowExecDto;
import com.kuaishou.agent.operation.backend.agentk.dto.CapabilityRunHistoryItem;
import com.kuaishou.agent.operation.backend.agentk.dto.CapabilityRunRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.CapabilityRunResponse;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;
import com.kuaishou.agent.operation.backend.platform.entity.PipelineExec;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.service.PlatformAdminService;
import com.kuaishou.agent.operation.backend.platform.support.ChatSessionKinds;
import com.kuaishou.agent.operation.backend.platform.support.OfficialDemoAssets;
import com.kuaishou.agent.operation.backend.platform.support.PipelineOnlySessionProfile;
import com.kuaishou.agent.operation.backend.platform.support.PlatformDateTimes;

/**
 * Capability 单次执行：按 agent-k capabilityId 启动，按 sessionId/executionId 查询与取消。
 */
@Service
public class CapabilityRunService {

    private static final Logger LOGGER = LoggerFactory.getLogger(CapabilityRunService.class);
    static final String PIPELINE_NAME = "capability_run";

    private final AgentKCapabilityCacheService capabilityCacheService;
    private final AgentKCreateSessionService createSessionService;
    private final AgentKClient agentKClient;
    private final AgentKIdentityResolver identityResolver;
    private final AgentKMessageMapper messageMapper;
    private final ChatAssetSyncService chatAssetSyncService;
    private final ChatPersistService chatPersistService;
    private final PlatformAdminService platformAdminService;
    private final ObjectMapper objectMapper;

    public CapabilityRunService(AgentKCapabilityCacheService capabilityCacheService,
            AgentKCreateSessionService createSessionService,
            AgentKClient agentKClient,
            AgentKIdentityResolver identityResolver,
            AgentKMessageMapper messageMapper,
            ChatAssetSyncService chatAssetSyncService,
            ChatPersistService chatPersistService,
            PlatformAdminService platformAdminService,
            ObjectMapper objectMapper) {
        this.capabilityCacheService = capabilityCacheService;
        this.createSessionService = createSessionService;
        this.agentKClient = agentKClient;
        this.identityResolver = identityResolver;
        this.messageMapper = messageMapper;
        this.chatAssetSyncService = chatAssetSyncService;
        this.chatPersistService = chatPersistService;
        this.platformAdminService = platformAdminService;
        this.objectMapper = objectMapper;
    }

    public Message<CapabilityRunResponse> run(CapabilityRunRequest request) {
        String capabilityId = request == null ? null : request.getCapabilityId();
        if (!StringUtils.hasText(capabilityId)) {
            return Message.error(ResultCode.PARAM_REQUIRED, "capabilityId is required");
        }
        String id = capabilityId.trim();
        AgentKOutboundIdentity identity = identityResolver.resolve();
        AgentKCapabilitiesDto live;
        try {
            live = capabilityCacheService.fetchLiveCapabilities(identity);
        } catch (IllegalStateException e) {
            return Message.error(ResultCode.RELY_SERVICE_BUSY, e.getMessage());
        }
        AgentKCapabilityItemDto item = findCapability(live, id);
        if (item == null) {
            return Message.error(ResultCode.NOT_FOUND, "capability not found: " + id);
        }

        PipelineEntry pipeline = new PipelineEntry();
        pipeline.setCapability(item.getId().trim());
        pipeline.setName(PIPELINE_NAME);
        pipeline.setDisplayName("Test " + item.getId().trim());

        AgentKCreateSessionRequest sessionRequest = new AgentKCreateSessionRequest();
        sessionRequest.setTitle(resolveTitle(request, item.getId()));
        sessionRequest.setProfile(objectMapper.valueToTree(
                PipelineOnlySessionProfile.buildWithPipelines(Collections.singletonList(pipeline))));
        sessionRequest.setSessionKind(ChatSessionKinds.CUSTOM);
        sessionRequest.setEntrySource(OfficialDemoAssets.ENTRY_SOURCE_CAPABILITY_LAB);

        Message<AgentKSessionDto> created = createSessionService.createSession(sessionRequest);
        if (created == null || created.getCode() != ResultCode.SUCCESS.getCode()
                || created.getData() == null || !StringUtils.hasText(created.getData().getId())) {
            return Message.error(ResultCode.RELY_SERVICE_BUSY,
                    created == null || !StringUtils.hasText(created.getMessage())
                            ? "create session failed"
                            : created.getMessage());
        }
        String sessionId = created.getData().getId().trim();

        AgentKStartWorkflowRequest start = new AgentKStartWorkflowRequest();
        start.setPipeline(PIPELINE_NAME);
        start.setArgs(argsOrEmpty(request));
        ResponseEntity<?> upstream = agentKClient.exchangeJson("POST",
                "/sessions/" + sessionId + "/pipeline-runs", null, start,
                AgentKWorkflowExecDto.class, identity);
        Message<AgentKWorkflowExecDto> execMessage = messageMapper.toMessage(upstream, AgentKWorkflowExecDto.class);
        if (execMessage.getCode() != ResultCode.SUCCESS.getCode() || execMessage.getData() == null) {
            LOGGER.warn("capability run start failed capabilityId={} sessionId={} code={} msg={}",
                    id, sessionId, execMessage.getCode(), execMessage.getMessage());
            return AgentKMessageMapper.keepMappedError(execMessage, "start capability execution failed");
        }
        AgentKWorkflowExecDto run = execMessage.getData();
        fillPipelineRunFromRequest(run, start);
        if (!StringUtils.hasText(run.getCapabilityId())) {
            run.setCapabilityId(id);
        }
        chatAssetSyncService.enrichPipelineRun(run);
        chatAssetSyncService.syncPipelineExecAndAssets(sessionId, run);

        CapabilityRunResponse response = new CapabilityRunResponse();
        response.setSessionId(sessionId);
        response.setExecutionId(run.getId());
        response.setCapabilityId(item.getId().trim());
        response.setPipeline(PIPELINE_NAME);
        response.setStatus(run.getStatus());
        response.setExecution(run);
        return Message.ok(response);
    }

    /**
     * 按 agent-k sessionId / executionId 回溯一次执行的 args、output、status。
     */
    public Message<CapabilityRunResponse> get(String sessionId, String executionId) {
        if (!StringUtils.hasText(sessionId) || !StringUtils.hasText(executionId)) {
            return Message.error(ResultCode.PARAM_REQUIRED, "sessionId and executionId are required");
        }
        String sid = sessionId.trim();
        String eid = executionId.trim();
        AgentKOutboundIdentity identity = identityResolver.resolve();
        ResponseEntity<?> upstream = agentKClient.exchangeJson("GET",
                "/sessions/" + sid + "/pipeline-runs/" + eid, null, null,
                AgentKWorkflowExecDto.class, identity);
        Message<AgentKWorkflowExecDto> execMessage = messageMapper.toMessage(upstream, AgentKWorkflowExecDto.class);
        if (execMessage.getCode() != ResultCode.SUCCESS.getCode() || execMessage.getData() == null) {
            return AgentKMessageMapper.keepMappedError(execMessage, "get capability execution failed");
        }
        AgentKWorkflowExecDto run = execMessage.getData();
        chatAssetSyncService.enrichPipelineRun(run);
        chatAssetSyncService.syncPipelineExecAndAssets(sid, run);
        CapabilityRunResponse response = new CapabilityRunResponse();
        response.setSessionId(sid);
        response.setExecutionId(run.getId());
        response.setCapabilityId(run.getCapabilityId());
        response.setPipeline(run.getName());
        response.setStatus(run.getStatus());
        response.setExecution(run);
        return Message.ok(response);
    }

    /**
     * 按人、按工具看 Lab 入参/出参。普通用户只看自己；超管可传 {@code userId} 看指定人，不传则看全部。
     */
    public Message<PageResult<CapabilityRunHistoryItem>> listHistory(String capabilityId, String userId,
            String status, Integer page, Integer pageSize) {
        String ownerFilter;
        try {
            ownerFilter = resolveHistoryOwnerFilter(userId);
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.FORBIDDEN, e.getMessage());
        }
        long pageNo = page == null || page <= 0 ? 1 : page;
        long size = pageSize == null || pageSize <= 0 ? 20 : pageSize;
        IPage<PipelineExec> result = chatPersistService.pageCapabilityLabExecs(
                StringUtils.hasText(capabilityId) ? capabilityId.trim() : null,
                ownerFilter,
                StringUtils.hasText(status) ? status.trim() : null,
                pageNo, size);
        List<CapabilityRunHistoryItem> items = result.getRecords() == null
                ? Collections.emptyList()
                : result.getRecords().stream().map(this::toHistoryItem).collect(Collectors.toList());
        return Message.ok(PageResult.of(result.getCurrent(), result.getSize(), result.getTotal(), items));
    }

    /** 当前可见范围内出现过的工具 ID，给「按工具」分组。 */
    public Message<List<String>> listHistoryTools(String userId) {
        String ownerFilter;
        try {
            ownerFilter = resolveHistoryOwnerFilter(userId);
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.FORBIDDEN, e.getMessage());
        }
        return Message.ok(chatPersistService.listCapabilityLabCapabilityIds(ownerFilter));
    }

    /**
     * 取消该 Lab session 上正在跑的执行。只接受 agent-k {@code ses_*}，不接受平台数字主键。
     */
    public Message<Void> cancel(String sessionId) {
        if (!StringUtils.hasText(sessionId)) {
            return Message.error(ResultCode.PARAM_REQUIRED, "sessionId is required");
        }
        String sid = sessionId.trim();
        if (!sid.startsWith("ses_")) {
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE,
                    "sessionId must be agent-k id (ses_*), not a platform primary key");
        }
        AgentKOutboundIdentity identity = identityResolver.resolve();
        ResponseEntity<?> upstream = agentKClient.exchangeJson("POST",
                "/sessions/" + sid + "/cancel", null, Collections.emptyMap(),
                Void.class, identity);
        return messageMapper.toMessage(upstream, Void.class);
    }

    private JsonNode argsOrEmpty(CapabilityRunRequest request) {
        if (request == null || request.getArgs() == null || request.getArgs().isNull()
                || request.getArgs().isMissingNode()) {
            return objectMapper.createObjectNode();
        }
        return request.getArgs();
    }

    private static String resolveTitle(CapabilityRunRequest request, String capabilityId) {
        if (request != null && StringUtils.hasText(request.getTitle())) {
            return request.getTitle().trim();
        }
        return "Capability: " + capabilityId;
    }

    private static AgentKCapabilityItemDto findCapability(AgentKCapabilitiesDto live, String capabilityId) {
        if (live == null || live.getCapabilities() == null) {
            return null;
        }
        for (AgentKCapabilityItemDto item : live.getCapabilities()) {
            if (item != null && capabilityId.equals(item.getId())) {
                return item;
            }
        }
        return null;
    }

    /**
     * 普通用户固定看自己；超管 {@code requestedUserId} 空=全部，非空=指定用户。
     */
    private String resolveHistoryOwnerFilter(String requestedUserId) {
        String self = identityResolver.resolve().getUserId();
        if (!StringUtils.hasText(self)) {
            throw new IllegalArgumentException("login required");
        }
        if (platformAdminService.isSuperAdmin(self)) {
            return StringUtils.hasText(requestedUserId) ? requestedUserId.trim() : null;
        }
        if (StringUtils.hasText(requestedUserId) && !self.equals(requestedUserId.trim())) {
            throw new IllegalArgumentException("cannot view other users");
        }
        return self;
    }

    private CapabilityRunHistoryItem toHistoryItem(PipelineExec row) {
        CapabilityRunHistoryItem item = new CapabilityRunHistoryItem();
        if (row == null) {
            return item;
        }
        item.setUserId(row.getUserId());
        item.setCapabilityId(row.getCapabilityId());
        item.setSessionId(row.getAgentkSessionId());
        item.setExecutionId(row.getAgentkExecId());
        item.setStatus(row.getStatus());
        item.setArgs(parseJson(row.getArgsJson()));
        item.setOutput(parseJson(row.getOutputJson()));
        item.setError(row.getError());
        LocalDateTime created = row.getCreatedAt() != null ? row.getCreatedAt() : row.getStartedAt();
        item.setCreatedAt(created);
        item.setFinishedAt(PlatformDateTimes.assumeUtcWallIfBefore(row.getFinishedAt(), created));
        return item;
    }

    private JsonNode parseJson(String raw) {
        if (!StringUtils.hasText(raw)) {
            return null;
        }
        try {
            return objectMapper.readTree(raw);
        } catch (Exception e) {
            LOGGER.warn("capability history json parse failed: {}", e.toString());
            return null;
        }
    }

    private static void fillPipelineRunFromRequest(AgentKWorkflowExecDto run, AgentKStartWorkflowRequest request) {
        if (run == null || request == null) {
            return;
        }
        if (run.getArgs() == null || run.getArgs().isNull() || run.getArgs().isMissingNode()) {
            run.setArgs(request.getArgs());
        }
        if (!StringUtils.hasText(run.getName()) && StringUtils.hasText(request.getPipeline())) {
            run.setName(request.getPipeline().trim());
        }
    }
}
