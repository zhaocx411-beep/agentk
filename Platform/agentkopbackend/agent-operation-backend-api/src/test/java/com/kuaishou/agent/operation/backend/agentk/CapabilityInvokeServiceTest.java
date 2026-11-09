package com.kuaishou.agent.operation.backend.agentk;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyBoolean;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.ArgumentMatchers.isNull;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import java.time.LocalDateTime;
import java.util.concurrent.atomic.AtomicReference;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto.AgentKCapabilityItemDto;
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
import com.kuaishou.agent.operation.backend.platform.service.PlatformAdminService;
import com.kuaishou.agent.operation.backend.platform.support.OfficialDemoAssets;
import com.kuaishou.agent.operation.backend.platform.support.PlatformAccessException;

class CapabilityInvokeServiceTest {

    private final ObjectMapper objectMapper = new ObjectMapper();
    private AgentKCapabilityCacheService cacheService;
    private AgentKClient agentKClient;
    private AgentKIdentityResolver identityResolver;
    private ChatAssetSyncService chatAssetSyncService;
    private ChatPersistService chatPersistService;
    private PlatformAdminService platformAdminService;
    private CapabilityInvokeService service;

    @BeforeEach
    void setUp() {
        cacheService = mock(AgentKCapabilityCacheService.class);
        agentKClient = mock(AgentKClient.class);
        identityResolver = mock(AgentKIdentityResolver.class);
        chatAssetSyncService = mock(ChatAssetSyncService.class);
        chatPersistService = mock(ChatPersistService.class);
        platformAdminService = mock(PlatformAdminService.class);
        service = new CapabilityInvokeService(
                cacheService, agentKClient, identityResolver, new AgentKMessageMapper(),
                chatAssetSyncService, chatPersistService, platformAdminService, objectMapper, Runnable::run);
        when(identityResolver.resolve()).thenReturn(new AgentKOutboundIdentity("wb_fuxingyi", "wb_fuxingyi", "付"));
        when(platformAdminService.isSuperAdmin(anyString())).thenReturn(false);
    }

    @Test
    void invokeRequiresCapabilityId() {
        Message<CapabilityInvokeResponse> response = service.invoke(new CapabilityRunRequest());
        assertThat(response.getCode()).isEqualTo(ResultCode.PARAM_REQUIRED.getCode());
    }

    @Test
    void invokeReturnsRunningImmediatelyAndPersistsOutputAfterUpstream() throws Exception {
        stubLiveCapability("agentk.generate_image");
        stubSessionPersist(48L);

        CapabilityInvokeOutputDto output = new CapabilityInvokeOutputDto();
        output.setCapabilityId("agentk.generate_image");
        ObjectNode result = objectMapper.createObjectNode();
        result.put("imageUrl", "https://cdn.example/out.png");
        output.setOutput(result);
        doReturn(ResponseEntity.ok(output)).when(agentKClient).exchangeJson(
                eq("POST"), eq("/capabilities/invoke"), isNull(), any(),
                eq(CapabilityInvokeOutputDto.class), any());
        doAnswer(invocation -> {
            AgentKWorkflowExecDto run = invocation.getArgument(1);
            if ("completed".equals(run.getStatus())) {
                run.setImageUrls(java.util.Collections.singletonList("https://cdn.example/out.png"));
            }
            return 0;
        }).when(chatAssetSyncService).syncPipelineExecAndAssets(anyString(), any());

        CapabilityRunRequest request = new CapabilityRunRequest();
        request.setCapabilityId("agentk.generate_image");
        request.setArgs(objectMapper.readTree("{\"prompt\":\"cat\"}"));

        Message<CapabilityInvokeResponse> response = service.invoke(request);

        assertThat(response.getCode()).isEqualTo(ResultCode.SUCCESS.getCode());
        assertThat(response.getData().getPlatformSessionId()).isEqualTo(48L);
        assertThat(response.getData().getInvocationId()).startsWith("inv_");
        assertThat(response.getData().getCapabilityId()).isEqualTo("agentk.generate_image");
        assertThat(response.getData().getStatus()).isEqualTo("running");

        ArgumentCaptor<String> sessionId = ArgumentCaptor.forClass(String.class);
        ArgumentCaptor<AgentKWorkflowExecDto> run = ArgumentCaptor.forClass(AgentKWorkflowExecDto.class);
        verify(chatAssetSyncService, times(2)).syncPipelineExecAndAssets(sessionId.capture(), run.capture());
        assertThat(sessionId.getAllValues()).allMatch(id -> id.startsWith("inv_"));
        assertThat(run.getAllValues().get(0).getStatus()).isEqualTo("running");
        assertThat(run.getAllValues().get(1).getStatus()).isEqualTo("completed");
        assertThat(run.getAllValues().get(1).getName()).isEqualTo(CapabilityRunService.PIPELINE_NAME);
        assertThat(run.getAllValues().get(1).getOutput().path("imageUrl").asText())
                .isEqualTo("https://cdn.example/out.png");
        assertThat(run.getAllValues().get(0).getCreatedAt()).contains("+08:00");
        assertThat(run.getAllValues().get(1).getFinishedAt()).contains("+08:00");
        verify(chatPersistService).completeCreateSession(any(), eq(sessionId.getValue()),
                anyString(), eq(true), eq(202), isNull());
        ArgumentCaptor<ChatPersistService.EffectiveSnapshot> snapshot =
                ArgumentCaptor.forClass(ChatPersistService.EffectiveSnapshot.class);
        verify(chatPersistService).beginCreateSession(eq("wb_fuxingyi"), eq(""), anyString(),
                isNull(), isNull(), any(), any(), snapshot.capture());
        assertThat(snapshot.getValue().getEntrySource())
                .isEqualTo(OfficialDemoAssets.ENTRY_SOURCE_CAPABILITY_LAB);
    }

    @Test
    void invokeDoesNotWaitForUpstreamWhenExecutorIsDeferred() {
        stubLiveCapability("agentk.generate_image");
        stubSessionPersist(1L);
        AtomicReference<Runnable> pending = new AtomicReference<>();
        service = new CapabilityInvokeService(
                cacheService, agentKClient, identityResolver, new AgentKMessageMapper(),
                chatAssetSyncService, chatPersistService, platformAdminService, objectMapper, pending::set);

        CapabilityRunRequest request = new CapabilityRunRequest();
        request.setCapabilityId("agentk.generate_image");
        Message<CapabilityInvokeResponse> response = service.invoke(request);

        assertThat(response.getCode()).isEqualTo(ResultCode.SUCCESS.getCode());
        assertThat(response.getData().getStatus()).isEqualTo("running");
        assertThat(pending.get()).isNotNull();
        verify(agentKClient, never()).exchangeJson(anyString(), anyString(), any(), any(), any(), any());
        verify(chatAssetSyncService, times(1)).syncPipelineExecAndAssets(anyString(), any());
    }

    @Test
    void invokePersistsFailedExecWhenUpstreamFails() {
        stubLiveCapability("agentk.generate_image");
        stubSessionPersist(1L);
        doReturn(ResponseEntity.status(HttpStatus.BAD_GATEWAY).body(error("get session missing")))
                .when(agentKClient).exchangeJson(eq("POST"), eq("/capabilities/invoke"), isNull(), any(),
                        eq(CapabilityInvokeOutputDto.class), any());

        CapabilityRunRequest request = new CapabilityRunRequest();
        request.setCapabilityId("agentk.generate_image");
        Message<CapabilityInvokeResponse> response = service.invoke(request);

        assertThat(response.getCode()).isEqualTo(ResultCode.SUCCESS.getCode());
        assertThat(response.getData().getStatus()).isEqualTo("running");
        ArgumentCaptor<AgentKWorkflowExecDto> run = ArgumentCaptor.forClass(AgentKWorkflowExecDto.class);
        verify(chatAssetSyncService, times(2)).syncPipelineExecAndAssets(anyString(), run.capture());
        assertThat(run.getAllValues().get(1).getStatus()).isEqualTo("failed");
        assertThat(run.getAllValues().get(1).getError()).contains("get session missing");
    }

    @Test
    void invokeAcceptsInvalidArgumentAndRecordsFailedStatus() {
        stubLiveCapability("agentk.omni_video");
        stubSessionPersist(2L);
        doReturn(ResponseEntity.status(HttpStatus.BAD_REQUEST)
                .body(error("INVALID_ARGUMENT: missing referImages")))
                .when(agentKClient).exchangeJson(eq("POST"), eq("/capabilities/invoke"), isNull(), any(),
                        eq(CapabilityInvokeOutputDto.class), any());

        CapabilityRunRequest request = new CapabilityRunRequest();
        request.setCapabilityId("agentk.omni_video");
        Message<CapabilityInvokeResponse> response = service.invoke(request);

        assertThat(response.getCode()).isEqualTo(ResultCode.SUCCESS.getCode());
        assertThat(response.getData().getStatus()).isEqualTo("running");
        ArgumentCaptor<AgentKWorkflowExecDto> run = ArgumentCaptor.forClass(AgentKWorkflowExecDto.class);
        verify(chatAssetSyncService, times(2)).syncPipelineExecAndAssets(anyString(), run.capture());
        assertThat(run.getAllValues().get(1).getStatus()).isEqualTo("failed");
        assertThat(run.getAllValues().get(1).getError()).contains("INVALID_ARGUMENT");
        assertThat(run.getAllValues().get(1).getError()).contains("referImages");
    }

    @Test
    void getReturnsPersistedOutput() {
        PipelineExec row = new PipelineExec();
        row.setSessionId(48L);
        row.setAgentkSessionId("inv_abc");
        row.setAgentkExecId("inv_abc");
        row.setPipelineName(CapabilityRunService.PIPELINE_NAME);
        row.setCapabilityId("agentk.generate_image");
        row.setStatus("completed");
        row.setArgsJson("{\"prompt\":\"cat\"}");
        row.setOutputJson("{\"imageUrl\":\"https://cdn.example/out.png\"}");
        when(chatPersistService.requireOwnedPipelineExecByAgentkId("inv_abc", "wb_fuxingyi")).thenReturn(row);
        doAnswer(invocation -> {
            AgentKWorkflowExecDto run = invocation.getArgument(0);
            run.setImageUrls(java.util.Collections.singletonList("https://cdn.example/out.png"));
            return null;
        }).when(chatAssetSyncService).enrichPipelineRun(any());

        Message<CapabilityInvokeResponse> response = service.get("inv_abc");

        assertThat(response.getCode()).isEqualTo(ResultCode.SUCCESS.getCode());
        assertThat(response.getData().getInvocationId()).isEqualTo("inv_abc");
        assertThat(response.getData().getPlatformSessionId()).isEqualTo(48L);
        assertThat(response.getData().getStatus()).isEqualTo("completed");
        assertThat(response.getData().getOutput().path("imageUrl").asText())
                .isEqualTo("https://cdn.example/out.png");
        assertThat(response.getData().getImageUrls()).containsExactly("https://cdn.example/out.png");
    }

    @Test
    void getConvertsLegacyUtcWallClockTimesToShanghai() {
        PipelineExec row = new PipelineExec();
        row.setSessionId(48L);
        row.setAgentkSessionId("inv_abc");
        row.setAgentkExecId("inv_abc");
        row.setStatus("failed");
        row.setCapabilityId("agentk.create_voice_embedding");
        row.setCreatedAt(LocalDateTime.of(2026, 9, 8, 14, 13, 24));
        row.setStartedAt(LocalDateTime.of(2026, 9, 8, 6, 13, 24));
        row.setFinishedAt(LocalDateTime.of(2026, 9, 8, 6, 13, 26));
        when(chatPersistService.requireOwnedPipelineExecByAgentkId("inv_abc", "wb_fuxingyi")).thenReturn(row);

        Message<CapabilityInvokeResponse> response = service.get("inv_abc");

        assertThat(response.getCode()).isEqualTo(ResultCode.SUCCESS.getCode());
        assertThat(response.getData().getExecution().getCreatedAt()).isEqualTo("2026-09-08 14:13:24");
        assertThat(response.getData().getExecution().getFinishedAt()).isEqualTo("2026-09-08 14:13:26");
    }

    @Test
    void getOutputRequiresCompletedStatus() {
        PipelineExec running = new PipelineExec();
        running.setSessionId(1L);
        running.setAgentkExecId("inv_run");
        running.setAgentkSessionId("inv_run");
        running.setStatus("running");
        running.setCapabilityId("agentk.generate_image");
        when(chatPersistService.requireOwnedPipelineExecByAgentkId("inv_run", "wb_fuxingyi")).thenReturn(running);

        Message<CapabilityInvokeResponse> notReady = service.getOutput("inv_run");
        assertThat(notReady.getCode()).isEqualTo(ResultCode.BAD_REQUEST.getCode());
        assertThat(notReady.getMessage()).contains("output not ready");
    }

    @Test
    void getOutputReturnsFailedUpstreamError() {
        PipelineExec failed = new PipelineExec();
        failed.setSessionId(1L);
        failed.setAgentkExecId("inv_fail");
        failed.setAgentkSessionId("inv_fail");
        failed.setStatus("failed");
        failed.setError("INVALID_ARGUMENT: missing referImages");
        when(chatPersistService.requireOwnedPipelineExecByAgentkId("inv_fail", "wb_fuxingyi")).thenReturn(failed);

        Message<CapabilityInvokeResponse> response = service.getOutput("inv_fail");
        assertThat(response.getCode()).isEqualTo(ResultCode.RELY_SERVICE_BUSY.getCode());
        assertThat(response.getMessage()).contains("referImages");
    }

    @Test
    void getOutputReturnsCompletedPayload() {
        PipelineExec row = new PipelineExec();
        row.setSessionId(48L);
        row.setAgentkSessionId("inv_abc");
        row.setAgentkExecId("inv_abc");
        row.setPipelineName(CapabilityRunService.PIPELINE_NAME);
        row.setCapabilityId("agentk.generate_image");
        row.setStatus("completed");
        row.setOutputJson("{\"imageUrl\":\"https://cdn.example/out.png\"}");
        when(chatPersistService.requireOwnedPipelineExecByAgentkId("inv_abc", "wb_fuxingyi")).thenReturn(row);

        Message<CapabilityInvokeResponse> response = service.getOutput("inv_abc");
        assertThat(response.getCode()).isEqualTo(ResultCode.SUCCESS.getCode());
        assertThat(response.getData().getOutput().path("imageUrl").asText())
                .isEqualTo("https://cdn.example/out.png");
    }

    @Test
    void getAllowsSuperAdminToReadOthersInvoke() {
        PipelineExec row = new PipelineExec();
        row.setSessionId(99L);
        row.setUserId("wb_other");
        row.setAgentkSessionId("inv_other");
        row.setAgentkExecId("inv_other");
        row.setStatus("completed");
        row.setCapabilityId("agentk.generate_image");
        row.setOutputJson("{\"imageUrl\":\"https://cdn.example/other.png\"}");
        when(platformAdminService.isSuperAdmin("wb_fuxingyi")).thenReturn(true);
        when(chatPersistService.requirePipelineExecByAgentkId("inv_other")).thenReturn(row);

        Message<CapabilityInvokeResponse> response = service.get("inv_other");

        assertThat(response.getCode()).isEqualTo(ResultCode.SUCCESS.getCode());
        assertThat(response.getData().getInvocationId()).isEqualTo("inv_other");
        assertThat(response.getData().getOutput().path("imageUrl").asText())
                .isEqualTo("https://cdn.example/other.png");
        verify(chatPersistService).requirePipelineExecByAgentkId("inv_other");
        verify(chatPersistService, never()).requireOwnedPipelineExecByAgentkId(anyString(), anyString());
    }

    @Test
    void getForbidsNonOwnerWhenNotSuperAdmin() {
        when(chatPersistService.requireOwnedPipelineExecByAgentkId("inv_other", "wb_fuxingyi"))
                .thenThrow(new PlatformAccessException("session not owned by current user"));

        Message<CapabilityInvokeResponse> response = service.get("inv_other");

        assertThat(response.getCode()).isEqualTo(ResultCode.FORBIDDEN.getCode());
        assertThat(response.getMessage()).isEqualTo("session not owned by current user");
        verify(chatPersistService, never()).requirePipelineExecByAgentkId(anyString());
    }

    private void stubLiveCapability(String capabilityId) {
        AgentKCapabilitiesDto live = new AgentKCapabilitiesDto();
        AgentKCapabilityItemDto item = new AgentKCapabilityItemDto();
        item.setId(capabilityId);
        live.getCapabilities().add(item);
        when(cacheService.fetchLiveCapabilities(any())).thenReturn(live);
    }

    private void stubSessionPersist(long sessionId) {
        ChatSession session = new ChatSession();
        session.setId(sessionId);
        CreateSessionBegin begin = new CreateSessionBegin();
        begin.setSession(session);
        when(chatPersistService.beginCreateSession(anyString(), anyString(), anyString(),
                isNull(), isNull(), any(), any(), any())).thenReturn(begin);
        doAnswer(invocation -> {
            CreateSessionBegin target = invocation.getArgument(0);
            String invocationId = invocation.getArgument(1);
            target.getSession().setAgentkSessionId(invocationId);
            return null;
        }).when(chatPersistService).completeCreateSession(any(), anyString(), anyString(),
                anyBoolean(), any(), any());
    }

    private static com.kuaishou.agent.operation.backend.agentk.dto.AgentKErrorDto error(String message) {
        com.kuaishou.agent.operation.backend.agentk.dto.AgentKErrorDto dto =
                new com.kuaishou.agent.operation.backend.agentk.dto.AgentKErrorDto();
        dto.setError(message);
        return dto;
    }
}
