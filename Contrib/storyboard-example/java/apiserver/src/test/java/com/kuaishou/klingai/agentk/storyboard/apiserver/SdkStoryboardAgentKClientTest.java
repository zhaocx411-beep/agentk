package com.kuaishou.klingai.agentk.storyboard.apiserver;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.google.protobuf.Struct;
import com.google.protobuf.Value;
import com.kuaishou.klingai.agentk.api.v1.AgentExecution;
import com.kuaishou.klingai.agentk.api.v1.CreateSessionResponse;
import com.kuaishou.klingai.agentk.api.v1.Execution;
import com.kuaishou.klingai.agentk.api.v1.ExecutionStatus;
import com.kuaishou.klingai.agentk.api.v1.GetExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.PipelineExecution;
import com.kuaishou.klingai.agentk.api.v1.Profile;
import com.kuaishou.klingai.agentk.api.v1.Session;
import com.kuaishou.klingai.agentk.api.v1.StartExecutionResponse;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.Test;

class SdkStoryboardAgentKClientTest {
    @Test
    void buildsRequestsAndDecodesCompletedPipeline() throws Exception {
        FakeAgentKClient rpc = new FakeAgentKClient();
        AtomicReference<com.kuaishou.klingai.agentk.api.v1.CreateSessionRequest> create =
                new AtomicReference<>();
        AtomicReference<com.kuaishou.klingai.agentk.api.v1.StartExecutionRequest> start =
                new AtomicReference<>();
        AtomicReference<com.kuaishou.klingai.agentk.api.v1.GetExecutionRequest> get =
                new AtomicReference<>();
        rpc.createSession = request -> {
            create.set(request);
            return CreateSessionResponse.newBuilder()
                    .setSession(Session.newBuilder().setId("ses_test"))
                    .build();
        };
        rpc.startExecution = request -> {
            start.set(request);
            return StartExecutionResponse.newBuilder()
                    .setExecution(Execution.newBuilder().setId("exe_test"))
                    .build();
        };
        rpc.getExecution = request -> {
            get.set(request);
            return pipelineResponse(ExecutionStatus.EXECUTION_STATUS_COMPLETED, output(), "");
        };

        Profile profile = Profile.newBuilder().setDefaultModel("model").build();
        SdkStoryboardAgentKClient client =
                new SdkStoryboardAgentKClient(rpc, JsonSupport.createMapper());
        String sessionId = client.createSession("Storyboard Film: rain", profile);
        String executionId = client.startPipeline(
                sessionId,
                new StoryboardArgs("rain", 1, "chat", "image", "video"));
        PipelineSnapshot snapshot = client.getPipelineExecution(sessionId, executionId);

        assertEquals("Storyboard Film: rain", create.get().getTitle());
        assertEquals(profile, create.get().getProfile());
        assertEquals("ses_test", start.get().getSessionId());
        assertEquals("storyboard_film_example", start.get().getPipeline().getPipeline());
        assertEquals("rain", start.get().getPipeline().getArgs().getFieldsOrThrow("topic").getStringValue());
        assertEquals(1, start.get().getPipeline().getArgs().getFieldsOrThrow("shots").getNumberValue());
        assertEquals("chat", start.get().getPipeline().getArgs().getFieldsOrThrow("chatModel").getStringValue());
        assertEquals("image", start.get().getPipeline().getArgs().getFieldsOrThrow("imageModel").getStringValue());
        assertEquals("video", start.get().getPipeline().getArgs().getFieldsOrThrow("videoModel").getStringValue());
        assertEquals("ses_test", get.get().getSessionId());
        assertEquals("exe_test", get.get().getExecutionId());
        assertEquals(JobStatus.COMPLETED, snapshot.status());
        assertNotNull(snapshot.output());
        assertEquals(1, snapshot.output().clipCount());
        assertEquals("https://video.example/film.mp4", snapshot.output().artifacts().get(0).uri());
        assertEquals("5s", snapshot.output().artifacts().get(0).params().get("duration"));

        client.close();
        assertTrue(rpc.closed);
    }

    @Test
    void mapsRunningFailedAndCancelledStatuses() throws Exception {
        FakeAgentKClient rpc = baseRpc();
        SdkStoryboardAgentKClient client =
                new SdkStoryboardAgentKClient(rpc, JsonSupport.createMapper());

        rpc.getExecution = ignored -> pipelineResponse(
                ExecutionStatus.EXECUTION_STATUS_RUNNING, null, "");
        assertEquals(JobStatus.RUNNING, client.getPipelineExecution("ses_x", "exe_x").status());

        rpc.getExecution = ignored -> pipelineResponse(
                ExecutionStatus.EXECUTION_STATUS_FAILED, null, "provider failed");
        PipelineSnapshot failed = client.getPipelineExecution("ses_x", "exe_x");
        assertEquals(JobStatus.FAILED, failed.status());
        assertEquals("provider failed", failed.error());

        rpc.getExecution = ignored -> pipelineResponse(
                ExecutionStatus.EXECUTION_STATUS_CANCELLED, null, "");
        PipelineSnapshot cancelled = client.getPipelineExecution("ses_x", "exe_x");
        assertEquals(JobStatus.FAILED, cancelled.status());
        assertEquals("pipeline cancelled", cancelled.error());
    }

    @Test
    void rejectsMalformedAgentKResponses() {
        FakeAgentKClient rpc = baseRpc();
        SdkStoryboardAgentKClient client =
                new SdkStoryboardAgentKClient(rpc, JsonSupport.createMapper());

        rpc.createSession = ignored -> CreateSessionResponse.getDefaultInstance();
        assertTrue(assertThrows(
                        AgentKCallException.class,
                        () -> client.createSession("x", Profile.getDefaultInstance()))
                .getMessage().contains("ses_"));

        rpc.startExecution = ignored -> StartExecutionResponse.getDefaultInstance();
        assertTrue(assertThrows(
                        AgentKCallException.class,
                        () -> client.startPipeline(
                                "ses_x", new StoryboardArgs("x", 1, "c", "i", "v")))
                .getMessage().contains("exe_"));

        rpc.getExecution = ignored -> GetExecutionResponse.getDefaultInstance();
        assertTrue(assertThrows(
                        AgentKCallException.class,
                        () -> client.getPipelineExecution("ses_x", "exe_x"))
                .getMessage().contains("missing execution"));

        rpc.getExecution = ignored -> GetExecutionResponse.newBuilder()
                .setExecution(Execution.newBuilder()
                        .setId("exe_x")
                        .setAgent(AgentExecution.getDefaultInstance()))
                .build();
        assertTrue(assertThrows(
                        AgentKCallException.class,
                        () -> client.getPipelineExecution("ses_x", "exe_x"))
                .getMessage().contains("not a pipeline"));
    }

    @Test
    void completedPipelineMayOmitOutput() throws Exception {
        FakeAgentKClient rpc = baseRpc();
        rpc.getExecution = ignored -> pipelineResponse(
                ExecutionStatus.EXECUTION_STATUS_COMPLETED, null, "");
        PipelineSnapshot snapshot = new SdkStoryboardAgentKClient(rpc, JsonSupport.createMapper())
                .getPipelineExecution("ses_x", "exe_x");
        assertEquals(JobStatus.COMPLETED, snapshot.status());
        assertNull(snapshot.output());
        assertFalse(snapshot.capabilityId().isEmpty());
    }

    private static FakeAgentKClient baseRpc() {
        FakeAgentKClient rpc = new FakeAgentKClient();
        rpc.createSession = ignored -> CreateSessionResponse.getDefaultInstance();
        rpc.startExecution = ignored -> StartExecutionResponse.getDefaultInstance();
        rpc.getExecution = ignored -> GetExecutionResponse.getDefaultInstance();
        return rpc;
    }

    private static GetExecutionResponse pipelineResponse(
            ExecutionStatus status, Value output, String error) {
        PipelineExecution.Builder pipeline = PipelineExecution.newBuilder()
                .setName("storyboard_film_example")
                .setCapabilityId("contrib.storyboard_example.storyboard_film_go");
        if (output != null) {
            pipeline.setOutput(output);
        }
        return GetExecutionResponse.newBuilder()
                .setExecution(Execution.newBuilder()
                        .setId("exe_test")
                        .setSessionId("ses_test")
                        .setStatus(status)
                        .setError(error)
                        .setPipeline(pipeline))
                .build();
    }

    private static Value output() {
        Struct params = Struct.newBuilder()
                .putFields("duration", Value.newBuilder().setStringValue("5s").build())
                .build();
        Struct artifact = Struct.newBuilder()
                .putFields("type", Value.newBuilder().setStringValue("video").build())
                .putFields("uri", Value.newBuilder().setStringValue("https://video.example/film.mp4").build())
                .putFields("params", Value.newBuilder().setStructValue(params).build())
                .build();
        Struct output = Struct.newBuilder()
                .putFields("artifacts", Value.newBuilder()
                        .setListValue(com.google.protobuf.ListValue.newBuilder()
                                .addValues(Value.newBuilder().setStructValue(artifact)))
                        .build())
                .putFields("clipCount", Value.newBuilder().setNumberValue(1).build())
                .build();
        return Value.newBuilder().setStructValue(output).build();
    }
}
