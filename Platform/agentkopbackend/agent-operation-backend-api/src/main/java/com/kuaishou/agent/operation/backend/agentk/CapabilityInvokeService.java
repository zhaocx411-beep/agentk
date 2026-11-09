package com.kuaishou.agent.operation.backend.agentk;

import java.time.LocalDateTime;
import java.util.Collections;
import java.util.UUID;
import java.util.concurrent.Executor;
import java.util.concurrent.RejectedExecutionException;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto.AgentKCapabilityItemDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionProfileDto.PipelineEntry;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKWorkflowExecDto;
import com.kuaishou.agent.operation.backend.agentk.dto.CapabilityInvokeOutputDto;
import com.kuaishou.agent.operation.backend.agentk.dto.CapabilityInvokeResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.CapabilityRunRequest;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.entity.PipelineExec;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.CreateSessionBegin;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.EffectiveSnapshot;
import com.kuaishou.agent.operation.backend.platform.service.PlatformAdminService;
import com.kuaishou.agent.operation.backend.platform.support.ChatSessionKinds;
import com.kuaishou.agent.operation.backend.platform.support.OfficialDemoAssets;
import com.kuaishou.agent.operation.backend.platform.support.PipelineOnlySessionProfile;
import com.kuaishou.agent.operation.backend.platform.support.PlatformAccessException;
import com.kuaishou.agent.operation.backend.platform.support.PlatformDateTimes;

/**
 * AgentK 瞬态 {@code InvokeCapability}：HTTP 立即返回 {@code inv_*}，后台等 RPC 结束后
 * 把 status / output 写入 {@code aop_pipeline_exec}。不建上游 Session / Execution。
 */
@Service
public class CapabilityInvokeService {

    private static final Logger LOGGER = LoggerFactory.getLogger(CapabilityInvokeService.class);

    private final AgentKCapabilityCacheService capabilityCacheService;
    private final AgentKClient agentKClient;
    private final AgentKIdentityResolver identityResolver;
    private final AgentKMessageMapper messageMapper;
    private final ChatAssetSyncService chatAssetSyncService;
    private final ChatPersistService chatPersistService;
    private final PlatformAdminService platformAdminService;
    private final ObjectMapper objectMapper;
    private final Executor capabilityInvokeExecutor;

    public CapabilityInvokeService(AgentKCapabilityCacheService capabilityCacheService,
            AgentKClient agentKClient,
            AgentKIdentityResolver identityResolver,
            AgentKMessageMapper messageMapper,
            ChatAssetSyncService chatAssetSyncService,
            ChatPersistService chatPersistService,
            PlatformAdminService platformAdminService,
            ObjectMapper objectMapper,
            @Qualifier("capabilityInvokeExecutor") Executor capabilityInvokeExecutor) {
        this.capabilityCacheService = capabilityCacheService;
        this.agentKClient = agentKClient;
        this.identityResolver = identityResolver;
        this.messageMapper = messageMapper;
        this.chatAssetSyncService = chatAssetSyncService;
        this.chatPersistService = chatPersistService;
        this.platformAdminService = platformAdminService;
        this.objectMapper = objectMapper;
        this.capabilityInvokeExecutor = capabilityInvokeExecutor;
    }

    public Message<CapabilityInvokeResponse> invoke(CapabilityRunRequest request) {
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

        JsonNode args = argsOrEmpty(request);
        JsonNode profile = objectMapper.valueToTree(
                PipelineOnlySessionProfile.buildWithPipelines(Collections.singletonList(pipelineFor(item))));
        EffectiveSnapshot effective = new EffectiveSnapshot();
        effective.setSessionKind(ChatSessionKinds.CUSTOM);
        effective.setEntrySource(OfficialDemoAssets.ENTRY_SOURCE_CAPABILITY_LAB);
        effective.setConfigSource("capability_invoke");
        effective.setProfileVersion(PipelineOnlySessionProfile.PROFILE_VERSION_LABEL);
        effective.setModel(PipelineOnlySessionProfile.DEFAULT_MODEL);
        effective.setToolsJson("[]");

        CreateSessionBegin begin;
        try {
            begin = chatPersistService.beginCreateSession(
                    identity.getUserId(),
                    "",
                    resolveTitle(request, item.getId()),
                    null,
                    null,
                    writeJson(args),
                    writeJson(profile),
                    effective);
        } catch (Exception e) {
            LOGGER.warn("capability invoke persist beginCreateSession failed capabilityId={}", id, e);
            return Message.error(ResultCode.RELY_SERVICE_BUSY, "persist invoke session failed");
        }

        String invocationId = newInvocationId();
        String started = PlatformDateTimes.nowOffsetIso();
        try {
            chatPersistService.completeCreateSession(
                    begin,
                    invocationId,
                    effective.getProfileVersion(),
                    true,
                    202,
                    null);
        } catch (Exception e) {
            LOGGER.warn("capability invoke persist completeCreateSession failed capabilityId={}", id, e);
            return Message.error(ResultCode.RELY_SERVICE_BUSY, "persist invoke session failed");
        }

        AgentKWorkflowExecDto running = newExecution(
                invocationId, item.getId().trim(), args, objectMapper.createObjectNode(),
                "running", null, started, null);
        persistExecution(invocationId, running);

        ChatSession platformSession = begin.getSession();
        try {
            capabilityInvokeExecutor.execute(() -> runInvoke(
                    identity, item.getId().trim(), args, invocationId, started));
        } catch (RejectedExecutionException e) {
            persistExecution(invocationId, newExecution(
                    invocationId, item.getId().trim(), args, objectMapper.createObjectNode(),
                    "failed", "invoke queue is full", started, PlatformDateTimes.nowOffsetIso()));
            return Message.error(ResultCode.RELY_SERVICE_BUSY, "invoke queue is full");
        }

        LOGGER.info("capability invoke accepted userId={} capabilityId={} invocationId={} platformSessionId={}",
                identity.getUserId(), id, invocationId,
                platformSession == null ? null : platformSession.getId());
        return Message.ok(toResponse(platformSession == null ? null : platformSession.getId(), running));
    }

    /**
     * 按 {@code inv_*} 读库：running / completed / failed，以及已落库的 output。
     * 普通用户只能看自己的；超管可回看任何人（与 {@code /runs/history} 一致）。
     */
    public Message<CapabilityInvokeResponse> get(String invocationId) {
        if (!StringUtils.hasText(invocationId)) {
            return Message.error(ResultCode.PARAM_REQUIRED, "invocationId is required");
        }
        String id = invocationId.trim();
        AgentKOutboundIdentity identity = identityResolver.resolve();
        PipelineExec row;
        try {
            row = requireReadablePipelineExec(id, identity.getUserId());
        } catch (PlatformAccessException e) {
            return Message.error(ResultCode.FORBIDDEN, e.getMessage());
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.NOT_FOUND, e.getMessage());
        }
        AgentKWorkflowExecDto execution = toExecution(row);
        chatAssetSyncService.enrichPipelineRun(execution);
        return Message.ok(toResponse(row.getSessionId(), execution));
    }

    /**
     * 取最终 output：仅 {@code completed} 返回 200；{@code running} 视为未就绪；{@code failed} 带上 error。
     */
    public Message<CapabilityInvokeResponse> getOutput(String invocationId) {
        Message<CapabilityInvokeResponse> snapshot = get(invocationId);
        if (snapshot.getCode() != ResultCode.SUCCESS.getCode() || snapshot.getData() == null) {
            return snapshot;
        }
        CapabilityInvokeResponse data = snapshot.getData();
        String status = data.getStatus() == null ? "" : data.getStatus().trim();
        if ("running".equalsIgnoreCase(status) || "queued".equalsIgnoreCase(status)) {
            return Message.error(ResultCode.BAD_REQUEST,
                    "output not ready, status=" + status);
        }
        if ("failed".equalsIgnoreCase(status)) {
            String error = data.getExecution() == null ? null : data.getExecution().getError();
            return Message.error(ResultCode.RELY_SERVICE_BUSY,
                    StringUtils.hasText(error) ? error : "invoke failed");
        }
        if (!"completed".equalsIgnoreCase(status)) {
            return Message.error(ResultCode.BAD_REQUEST,
                    "output not ready, status=" + status);
        }
        return snapshot;
    }

    private void runInvoke(AgentKOutboundIdentity identity, String capabilityId, JsonNode args,
            String invocationId, String started) {
        try {
            ResponseEntity<?> upstream = agentKClient.exchangeJson(
                    "POST", "/capabilities/invoke", null,
                    java.util.Map.of("capabilityId", capabilityId, "args", args),
                    CapabilityInvokeOutputDto.class, identity);
            Message<CapabilityInvokeOutputDto> invokeMessage =
                    messageMapper.toMessage(upstream, CapabilityInvokeOutputDto.class);
            boolean ok = invokeMessage.getCode() == ResultCode.SUCCESS.getCode()
                    && invokeMessage.getData() != null;
            JsonNode output = ok ? invokeMessage.getData().getOutput() : objectMapper.createObjectNode();
            String status = ok ? "completed" : "failed";
            String error = ok ? null : invokeMessage.getMessage();
            String finished = PlatformDateTimes.nowOffsetIso();
            AgentKWorkflowExecDto execution = newExecution(
                    invocationId, capabilityId, args, output, status, error, started, finished);
            persistExecution(invocationId, execution);
            if (ok) {
                LOGGER.info("capability invoke done userId={} capabilityId={} invocationId={} assetCount={}",
                        identity.getUserId(), capabilityId, invocationId,
                        execution.getMediaArtifacts() == null ? 0 : execution.getMediaArtifacts().size());
            } else {
                LOGGER.warn("capability invoke failed capabilityId={} invocationId={} code={} msg={}",
                        capabilityId, invocationId, invokeMessage.getCode(), invokeMessage.getMessage());
            }
        } catch (Exception e) {
            LOGGER.warn("capability invoke background failed capabilityId={} invocationId={}",
                    capabilityId, invocationId, e);
            persistExecution(invocationId, newExecution(
                    invocationId, capabilityId, args, objectMapper.createObjectNode(),
                    "failed", e.getMessage(), started, PlatformDateTimes.nowOffsetIso()));
        }
    }

    private PipelineExec requireReadablePipelineExec(String invocationId, String userId) {
        if (platformAdminService.isSuperAdmin(userId)) {
            return chatPersistService.requirePipelineExecByAgentkId(invocationId);
        }
        return chatPersistService.requireOwnedPipelineExecByAgentkId(invocationId, userId);
    }

    private void persistExecution(String invocationId, AgentKWorkflowExecDto execution) {
        chatAssetSyncService.enrichPipelineRun(execution);
        chatAssetSyncService.syncPipelineExecAndAssets(invocationId, execution);
    }

    private AgentKWorkflowExecDto newExecution(String invocationId, String capabilityId, JsonNode args,
            JsonNode output, String status, String error, String started, String finished) {
        AgentKWorkflowExecDto execution = new AgentKWorkflowExecDto();
        execution.setId(invocationId);
        execution.setSessionId(invocationId);
        execution.setName(CapabilityRunService.PIPELINE_NAME);
        execution.setCapabilityId(capabilityId);
        execution.setStatus(status);
        execution.setArgs(args);
        execution.setOutput(output);
        execution.setError(error);
        execution.setCreatedAt(started);
        execution.setFinishedAt(finished);
        return execution;
    }

    private CapabilityInvokeResponse toResponse(Long platformSessionId, AgentKWorkflowExecDto execution) {
        CapabilityInvokeResponse response = new CapabilityInvokeResponse();
        response.setInvocationId(execution.getId());
        response.setPlatformSessionId(platformSessionId);
        response.setCapabilityId(execution.getCapabilityId());
        response.setStatus(execution.getStatus());
        response.setArgs(execution.getArgs());
        response.setOutput(execution.getOutput());
        response.setMediaArtifacts(execution.getMediaArtifacts());
        response.setImageUrls(execution.getImageUrls());
        response.setVideoUrls(execution.getVideoUrls());
        response.setExecution(execution);
        return response;
    }

    private AgentKWorkflowExecDto toExecution(PipelineExec row) {
        AgentKWorkflowExecDto execution = new AgentKWorkflowExecDto();
        if (row == null) {
            return execution;
        }
        execution.setId(row.getAgentkExecId());
        execution.setSessionId(row.getAgentkSessionId());
        execution.setName(row.getPipelineName());
        execution.setCapabilityId(row.getCapabilityId());
        execution.setStatus(row.getStatus());
        execution.setArgs(parseJson(row.getArgsJson()));
        execution.setOutput(parseJson(row.getOutputJson()));
        execution.setError(row.getError());
        LocalDateTime created = row.getCreatedAt() != null ? row.getCreatedAt() : row.getStartedAt();
        LocalDateTime started = PlatformDateTimes.assumeUtcWallIfBefore(row.getStartedAt(), created);
        LocalDateTime finished = PlatformDateTimes.assumeUtcWallIfBefore(row.getFinishedAt(), created);
        execution.setCreatedAt(PlatformDateTimes.format(started != null ? started : created));
        execution.setFinishedAt(PlatformDateTimes.format(finished));
        return execution;
    }

    private JsonNode parseJson(String raw) {
        if (!StringUtils.hasText(raw)) {
            return null;
        }
        try {
            return objectMapper.readTree(raw);
        } catch (Exception e) {
            LOGGER.warn("capability invoke json parse failed: {}", e.toString());
            return null;
        }
    }

    private JsonNode argsOrEmpty(CapabilityRunRequest request) {
        if (request == null || request.getArgs() == null || request.getArgs().isNull()
                || request.getArgs().isMissingNode()) {
            return objectMapper.createObjectNode();
        }
        return request.getArgs();
    }

    private static PipelineEntry pipelineFor(AgentKCapabilityItemDto item) {
        PipelineEntry pipeline = new PipelineEntry();
        pipeline.setCapability(item.getId().trim());
        pipeline.setName(CapabilityRunService.PIPELINE_NAME);
        pipeline.setDisplayName("Invoke " + item.getId().trim());
        return pipeline;
    }

    private static String resolveTitle(CapabilityRunRequest request, String capabilityId) {
        if (request != null && StringUtils.hasText(request.getTitle())) {
            return request.getTitle().trim();
        }
        return "Capability invoke: " + capabilityId;
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

    private static String newInvocationId() {
        return "inv_" + UUID.randomUUID().toString().replace("-", "");
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
}
