package com.kuaishou.agent.operation.backend.keffects.task;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import java.util.Arrays;
import java.util.Collections;

import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.AgentKIdentityResolver;
import com.kuaishou.agent.operation.backend.agentk.AgentKOutboundIdentity;
import com.kuaishou.agent.operation.backend.keffects.KEffectsClient;
import com.kuaishou.agent.operation.backend.keffects.KEffectsClient.KEffectsUpstreamException;
import com.kuaishou.agent.operation.backend.keffects.KEffectsProperties;
import com.kuaishou.agent.operation.backend.keffects.dto.KEffectsCreateTaskRequest;
import com.kuaishou.agent.operation.backend.keffects.task.adapter.ClothingDupeTaskAdapter;
import com.kuaishou.agent.operation.backend.keffects.task.adapter.GoodsScraperTaskAdapter;
import com.kuaishou.agent.operation.backend.keffects.task.adapter.GoodsStudioTaskAdapter;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;

class KEffectsTaskFacadeTest {

    private static final String TASK_TYPE = "demo-task";

    private final ObjectMapper objectMapper = new ObjectMapper();

    @Test
    void createTaskRequiresTaskTypeInBody() {
        KEffectsTaskFacade facade = newFacade(mock(KEffectsClient.class), mock(AgentKIdentityResolver.class));

        Message<JsonNode> response = facade.createTask(new KEffectsCreateTaskRequest());

        assertThat(response.getCode()).isEqualTo(ResultCode.PARAM_REQUIRED.getCode());
        assertThat(response.getMessage()).contains("taskType is required");
    }

    @Test
    void createTaskPersistsViaAssetService() throws Exception {
        KEffectsClient client = mock(KEffectsClient.class);
        AgentKIdentityResolver identityResolver = mock(AgentKIdentityResolver.class);
        KEffectsTaskAssetService assetService = mock(KEffectsTaskAssetService.class);
        RecordingAdapter adapter = new RecordingAdapter();
        KEffectsTaskFacade facade = new KEffectsTaskFacade(
                client, new KEffectsProperties(),
                new KEffectsTaskRegistry(Collections.singletonList(adapter)),
                identityResolver, assetService);
        KEffectsCreateTaskRequest request = new KEffectsCreateTaskRequest();
        request.setTaskType(TASK_TYPE);
        request.setInput(objectMapper.readTree("{\"value\":\"ok\"}"));
        JsonNode upstream = objectMapper.readTree("{\"taskId\":\"t_1\",\"taskType\":\"demo-task\"}");
        when(client.createTask(any(), any())).thenReturn(upstream);
        when(identityResolver.resolve()).thenReturn(new AgentKOutboundIdentity("u1", "u1", "U1"));
        when(assetService.persistAfterCreate(any(), any(), anyString(), any())).thenReturn(1);
        when(assetService.enrichWithBlobUrls(any())).thenAnswer(invocation -> invocation.getArgument(0));

        Message<JsonNode> response = facade.createTask(request);

        assertThat(response.getCode()).isEqualTo(ResultCode.SUCCESS.getCode());
        assertThat(adapter.validated).isTrue();
        verify(assetService).persistAfterCreate(eq(upstream), eq(request), eq("u1"), any());
    }

    @Test
    void createTaskMapsUpstreamValidationFailure() throws Exception {
        KEffectsClient client = mock(KEffectsClient.class);
        KEffectsTaskFacade facade = newFacade(client, mock(AgentKIdentityResolver.class));
        KEffectsCreateTaskRequest request = new KEffectsCreateTaskRequest();
        request.setTaskType(TASK_TYPE);
        request.setInput(objectMapper.readTree("{\"value\":\"ok\"}"));
        when(client.createTask(any(), any())).thenThrow(new KEffectsUpstreamException(422, "invalid input"));

        Message<JsonNode> response = facade.createTask(request);

        assertThat(response.getCode()).isEqualTo(ResultCode.BAD_REQUEST.getCode());
        assertThat(response.getMessage()).contains("invalid input");
    }

    @Test
    void getTaskRoutesEcsTaskIdToKlingAigcEngine() throws Exception {
        KEffectsClient client = mock(KEffectsClient.class);
        KEffectsTaskFacade facade = facadeWithProductionAdapters(client, mock(AgentKIdentityResolver.class));
        when(client.getTask(eq("ecs-abc"), any())).thenReturn(objectMapper.createObjectNode());

        facade.getTask("ecs-abc", null);

        ArgumentCaptor<String> baseUrl = ArgumentCaptor.forClass(String.class);
        verify(client).getTask(eq("ecs-abc"), baseUrl.capture());
        assertThat(baseUrl.getValue()).isEqualTo("https://kling-aigc-engine-prt.test.gifshow.com");
    }

    @Test
    void getTaskRoutesClothingDupeUuidToKlingAigcEngine() throws Exception {
        KEffectsClient client = mock(KEffectsClient.class);
        KEffectsTaskFacade facade = facadeWithProductionAdapters(client, mock(AgentKIdentityResolver.class));
        String taskId = "b776098c-2580-4158-a0aa-0c80d539963d";
        when(client.getTask(eq(taskId), any())).thenReturn(objectMapper.createObjectNode());

        facade.getTask(taskId, null);

        ArgumentCaptor<String> baseUrl = ArgumentCaptor.forClass(String.class);
        verify(client).getTask(eq(taskId), baseUrl.capture());
        assertThat(baseUrl.getValue()).isEqualTo("https://kling-aigc-engine-prt.test.gifshow.com");
    }

    @Test
    void cancelTaskRejectsKlingAigcEngineTaskIdWithoutUpstreamCall() {
        KEffectsClient client = mock(KEffectsClient.class);
        KEffectsTaskFacade facade = facadeWithProductionAdapters(client, mock(AgentKIdentityResolver.class));

        Message<JsonNode> ecs = facade.cancelTask("ecs-abc", null);
        Message<JsonNode> egs = facade.cancelTask("egs-92ec36db08da", null);

        assertThat(ecs.getCode()).isEqualTo(ResultCode.OPERATION_UNSUPPORTED.getCode());
        assertThat(ecs.getMessage()).contains("不支持取消");
        assertThat(egs.getCode()).isEqualTo(ResultCode.OPERATION_UNSUPPORTED.getCode());
    }

    @Test
    void cancelTaskRoutesClothingDupeUuidToKlingAigcEngine() throws Exception {
        KEffectsClient client = mock(KEffectsClient.class);
        KEffectsTaskFacade facade = facadeWithProductionAdapters(client, mock(AgentKIdentityResolver.class));
        String taskId = "b776098c-2580-4158-a0aa-0c80d539963d";
        when(client.cancelTask(eq(taskId), any())).thenReturn(objectMapper.createObjectNode());

        facade.cancelTask(taskId, null);

        ArgumentCaptor<String> baseUrl = ArgumentCaptor.forClass(String.class);
        verify(client).cancelTask(eq(taskId), baseUrl.capture());
        assertThat(baseUrl.getValue()).isEqualTo("https://kling-aigc-engine-prt.test.gifshow.com");
    }

    @Test
    void getTaskRoutesEgsTaskIdToKlingAigcEngine() throws Exception {
        KEffectsClient client = mock(KEffectsClient.class);
        KEffectsTaskFacade facade = facadeWithProductionAdapters(client, mock(AgentKIdentityResolver.class));
        when(client.getTask(eq("egs-abc"), any())).thenReturn(objectMapper.createObjectNode());

        facade.getTask("egs-abc", null);

        ArgumentCaptor<String> baseUrl = ArgumentCaptor.forClass(String.class);
        verify(client).getTask(eq("egs-abc"), baseUrl.capture());
        assertThat(baseUrl.getValue()).isEqualTo("https://kling-aigc-engine-prt.test.gifshow.com");
    }

    @Test
    void inferTaskTypeFromTaskId() {
        assertThat(KEffectsTaskFacade.inferTaskTypeFromTaskId("ecs-1"))
                .isEqualTo(GoodsScraperTaskAdapter.TASK_TYPE);
        assertThat(KEffectsTaskFacade.inferTaskTypeFromTaskId("egs-1"))
                .isEqualTo(GoodsStudioTaskAdapter.TASK_TYPE);
        assertThat(KEffectsTaskFacade.inferTaskTypeFromTaskId("uuid")).isNull();
    }

    @Test
    void getTaskPersistsStudioByEgsPrefix() throws Exception {
        KEffectsClient client = mock(KEffectsClient.class);
        KEffectsTaskAssetService assetService = mock(KEffectsTaskAssetService.class);
        AgentKIdentityResolver identityResolver = mock(AgentKIdentityResolver.class);
        when(identityResolver.resolve()).thenReturn(new AgentKOutboundIdentity("u1", "u1", "U1"));
        JsonNode upstream = objectMapper.readTree(
                "{\"taskId\":\"egs-abc\",\"status\":\"succeeded\",\"output\":{}}");
        when(client.getTask(eq("egs-abc"), any())).thenReturn(upstream);
        when(assetService.persistAfterGet(any(), anyString(), anyString(), any())).thenReturn(0);
        when(assetService.enrichWithBlobUrls(any())).thenAnswer(invocation -> invocation.getArgument(0));

        KEffectsTaskFacade facade = new KEffectsTaskFacade(
                client, new KEffectsProperties(),
                new KEffectsTaskRegistry(Arrays.asList(
                        new GoodsScraperTaskAdapter(),
                        new GoodsStudioTaskAdapter(objectMapper),
                        new ClothingDupeTaskAdapter(objectMapper))),
                identityResolver, assetService);

        facade.getTask("egs-abc", null);

        ArgumentCaptor<KEffectsTaskAssetService.AssetProfile> profile =
                ArgumentCaptor.forClass(KEffectsTaskAssetService.AssetProfile.class);
        verify(assetService).persistAfterGet(eq(upstream), eq("egs-abc"), eq("u1"), profile.capture());
        assertThat(profile.getValue().getTaskType()).isEqualTo(GoodsStudioTaskAdapter.TASK_TYPE);
    }

    private KEffectsTaskFacade facadeWithProductionAdapters(KEffectsClient client,
            AgentKIdentityResolver identityResolver) {
        return new KEffectsTaskFacade(
                client, new KEffectsProperties(),
                new KEffectsTaskRegistry(Arrays.asList(
                        new GoodsScraperTaskAdapter(),
                        new GoodsStudioTaskAdapter(objectMapper),
                        new ClothingDupeTaskAdapter(objectMapper))),
                identityResolver, mockAssetService());
    }

    private KEffectsTaskFacade newFacade(KEffectsClient client, AgentKIdentityResolver identityResolver) {
        return new KEffectsTaskFacade(
                client, new KEffectsProperties(),
                new KEffectsTaskRegistry(Collections.singletonList(new RecordingAdapter())),
                identityResolver, mockAssetService());
    }

    private static KEffectsTaskAssetService mockAssetService() {
        KEffectsTaskAssetService assetService = mock(KEffectsTaskAssetService.class);
        when(assetService.enrichWithBlobUrls(any())).thenAnswer(invocation -> invocation.getArgument(0));
        return assetService;
    }

    private static final class RecordingAdapter implements KEffectsTaskAdapter {
        private boolean validated;

        @Override
        public String taskType() {
            return TASK_TYPE;
        }

        @Override
        public String upstreamBaseUrl() {
            return "https://upstream.example.com";
        }

        @Override
        public void validateCreate(KEffectsCreateTaskRequest request) {
            if (request.getInput() == null || !request.getInput().hasNonNull("value")) {
                throw new IllegalArgumentException("value is required");
            }
            validated = true;
        }
    }
}
