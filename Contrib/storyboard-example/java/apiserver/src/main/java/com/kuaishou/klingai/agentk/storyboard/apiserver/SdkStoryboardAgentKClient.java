package com.kuaishou.klingai.agentk.storyboard.apiserver;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.google.protobuf.ListValue;
import com.google.protobuf.Struct;
import com.google.protobuf.Value;
import com.kuaishou.klingai.agentk.api.v1.CreateSessionRequest;
import com.kuaishou.klingai.agentk.api.v1.CreateSessionResponse;
import com.kuaishou.klingai.agentk.api.v1.Execution;
import com.kuaishou.klingai.agentk.api.v1.GetExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.GetExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.PipelineExecution;
import com.kuaishou.klingai.agentk.api.v1.PipelineExecutionInput;
import com.kuaishou.klingai.agentk.api.v1.Profile;
import com.kuaishou.klingai.agentk.api.v1.StartExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.StartExecutionResponse;
import com.kuaishou.klingai.agentk.client.AgentKClient;
import com.kuaishou.klingai.agentk.client.ClientOptions;
import com.kuaishou.klingai.agentk.client.grpc.AgentKGrpcClient;

import java.util.List;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

final class SdkStoryboardAgentKClient implements StoryboardAgentKClient {
    static final String PIPELINE_NAME = "storyboard_film_example";
    private static final long WAIT_SECONDS = 15;

    private final AgentKClient client;
    private final ObjectMapper mapper;

    static SdkStoryboardAgentKClient create(ObjectMapper mapper) {
        return new SdkStoryboardAgentKClient(
                AgentKGrpcClient.create(new ClientOptions()), mapper);
    }

    SdkStoryboardAgentKClient(AgentKClient client, ObjectMapper mapper) {
        this.client = client;
        this.mapper = mapper;
    }

    @Override
    public String createSession(String title, Profile profile) throws AgentKCallException {
        CreateSessionResponse response = await(
                client.createSession(CreateSessionRequest.newBuilder()
                        .setTitle(title)
                        .setProfile(profile)
                        .build()),
                "create AgentK session");
        String sessionId = response.hasSession() ? response.getSession().getId() : "";
        if (!sessionId.startsWith("ses_")) {
            throw new AgentKCallException("AgentK response is missing a valid ses_ id");
        }
        return sessionId;
    }

    @Override
    public String startPipeline(String sessionId, StoryboardArgs args)
            throws AgentKCallException {
        Struct input = Struct.newBuilder()
                .putFields("topic", stringValue(args.topic()))
                .putFields("shots", numberValue(args.shots()))
                .putFields("chatModel", stringValue(args.chatModel()))
                .putFields("imageModel", stringValue(args.imageModel()))
                .putFields("videoModel", stringValue(args.videoModel()))
                .build();
        StartExecutionResponse response = await(
                client.startExecution(StartExecutionRequest.newBuilder()
                        .setSessionId(sessionId)
                        .setPipeline(PipelineExecutionInput.newBuilder()
                                .setPipeline(PIPELINE_NAME)
                                .setArgs(input))
                        .build()),
                "start AgentK pipeline");
        String executionId = response.hasExecution() ? response.getExecution().getId() : "";
        if (!executionId.startsWith("exe_")) {
            throw new AgentKCallException("AgentK response is missing a valid exe_ id");
        }
        return executionId;
    }

    @Override
    public PipelineSnapshot getPipelineExecution(String sessionId, String executionId)
            throws AgentKCallException {
        GetExecutionResponse response = await(
                client.getExecution(GetExecutionRequest.newBuilder()
                        .setSessionId(sessionId)
                        .setExecutionId(executionId)
                        .build()),
                "get AgentK execution");
        if (!response.hasExecution()) {
            throw new AgentKCallException("AgentK response is missing execution");
        }
        Execution execution = response.getExecution();
        if (!execution.hasPipeline()) {
            throw new AgentKCallException(
                    "AgentK execution " + executionId + " is not a pipeline");
        }
        PipelineExecution pipeline = execution.getPipeline();
        return switch (execution.getStatus()) {
            case EXECUTION_STATUS_RUNNING -> new PipelineSnapshot(
                    pipeline.getCapabilityId(), JobStatus.RUNNING, null, null);
            case EXECUTION_STATUS_COMPLETED -> new PipelineSnapshot(
                    pipeline.getCapabilityId(),
                    JobStatus.COMPLETED,
                    pipeline.hasOutput() ? decodeOutput(pipeline.getOutput()) : null,
                    emptyToNull(execution.getError()));
            case EXECUTION_STATUS_FAILED -> new PipelineSnapshot(
                    pipeline.getCapabilityId(),
                    JobStatus.FAILED,
                    null,
                    emptyToNull(execution.getError()));
            case EXECUTION_STATUS_CANCELLED -> new PipelineSnapshot(
                    pipeline.getCapabilityId(),
                    JobStatus.FAILED,
                    null,
                    emptyToDefault(execution.getError(), "pipeline cancelled"));
            default -> throw new AgentKCallException(
                    "AgentK returned unknown execution status " + execution.getStatus());
        };
    }

    private StoryboardOutput decodeOutput(Value value) throws AgentKCallException {
        try {
            Object decoded = fromProtoValue(value);
            return mapper.convertValue(decoded, StoryboardOutput.class);
        } catch (IllegalArgumentException error) {
            throw new AgentKCallException("decode pipeline output", error);
        }
    }

    private static Object fromProtoValue(Value value) {
        return switch (value.getKindCase()) {
            case NULL_VALUE, KIND_NOT_SET -> null;
            case NUMBER_VALUE -> value.getNumberValue();
            case STRING_VALUE -> value.getStringValue();
            case BOOL_VALUE -> value.getBoolValue();
            case STRUCT_VALUE -> fromProtoStruct(value.getStructValue());
            case LIST_VALUE -> fromProtoList(value.getListValue());
        };
    }

    private static List<Object> fromProtoList(ListValue value) {
        return value.getValuesList().stream()
                .map(SdkStoryboardAgentKClient::fromProtoValue)
                .toList();
    }

    private static Map<String, Object> fromProtoStruct(Struct value) {
        Map<String, Object> decoded = new LinkedHashMap<>();
        value.getFieldsMap().forEach(
                (key, fieldValue) -> decoded.put(key, fromProtoValue(fieldValue)));
        return decoded;
    }

    private static Value stringValue(String value) {
        return Value.newBuilder().setStringValue(value).build();
    }

    private static Value numberValue(double value) {
        return Value.newBuilder().setNumberValue(value).build();
    }

    private static String emptyToNull(String value) {
        return value == null || value.trim().isEmpty() ? null : value;
    }

    private static String emptyToDefault(String value, String fallback) {
        String normalized = emptyToNull(value);
        return normalized == null ? fallback : normalized;
    }

    private static <T> T await(CompletionStage<T> stage, String operation)
            throws AgentKCallException {
        try {
            return stage.toCompletableFuture().get(WAIT_SECONDS, TimeUnit.SECONDS);
        } catch (InterruptedException error) {
            Thread.currentThread().interrupt();
            throw new AgentKCallException(operation + " was interrupted", error);
        } catch (ExecutionException error) {
            Throwable cause = error.getCause() == null ? error : error.getCause();
            throw new AgentKCallException(operation + ": " + cause.getMessage(), cause);
        } catch (TimeoutException error) {
            throw new AgentKCallException(operation + " timed out", error);
        }
    }

    @Override
    public void close() {
        client.close();
    }
}
