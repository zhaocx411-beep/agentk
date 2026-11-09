package com.kuaishou.agent.operation.backend.agentk.grpc;

import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Map;

import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.google.protobuf.Timestamp;
import com.kuaishou.klingai.agentk.api.v1.AskUserRequested;
import com.kuaishou.klingai.agentk.api.v1.AskUserResponded;
import com.kuaishou.klingai.agentk.api.v1.AssistantReasoning;
import com.kuaishou.klingai.agentk.api.v1.AssistantText;
import com.kuaishou.klingai.agentk.api.v1.AssistantToolCall;
import com.kuaishou.klingai.agentk.api.v1.Delta;
import com.kuaishou.klingai.agentk.api.v1.DeltaBlockKind;
import com.kuaishou.klingai.agentk.api.v1.Event;
import com.kuaishou.klingai.agentk.api.v1.InvocationProgress;
import com.kuaishou.klingai.agentk.api.v1.RoundCompleted;
import com.kuaishou.klingai.agentk.api.v1.RoundStarted;
import com.kuaishou.klingai.agentk.api.v1.ToolExecFailed;
import com.kuaishou.klingai.agentk.api.v1.ToolExecStarted;
import com.kuaishou.klingai.agentk.api.v1.ToolExecSucceeded;
import com.kuaishou.klingai.agentk.api.v1.UserInput;
import com.kuaishou.klingai.agentk.api.v1.WatchEventsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse;

/**
 * gRPC {@link Event} / {@link WatchEventsResponse} → 前端沿用的 REST 事件信封
 * {@code {seq, roundId, kind, data, createdAt}}。
 */
public final class AgentKEventWireConverter {

    private AgentKEventWireConverter() {
    }

    static AgentKListEventsResponse.AgentKEventDto toEventDto(Event event, ObjectMapper objectMapper) {
        WireEnvelope envelope = toWireEnvelope(event, objectMapper);
        if (envelope == null) {
            return null;
        }
        AgentKListEventsResponse.AgentKEventDto dto = new AgentKListEventsResponse.AgentKEventDto();
        dto.setEventId(envelope.eventId);
        dto.setExecutionId(envelope.executionId);
        dto.setSeq(null);
        dto.setRoundId(envelope.roundId);
        dto.setKind(envelope.kind);
        dto.setData(envelope.data);
        dto.setCreatedAt(envelope.createdAt);
        return dto;
    }

    static WireEnvelope toWireEnvelope(Event event, ObjectMapper objectMapper) {
        if (event == null) {
            return null;
        }
        // 现行 gRPC Event 没有 seq；禁止从 UUID event_id 或列表位置伪造序号。
        String eventId = event.getEventId();
        String executionId = event.getExecutionId();
        String roundId = event.getRoundId();
        String createdAt = formatTimestamp(event.getCreatedAt());
        String kind;
        JsonNode data;
        switch (event.getPayloadCase()) {
            case USER_INPUT:
                kind = "user.input";
                data = objectMapper.valueToTree(mapOf("text", event.getUserInput().getText()));
                break;
            case ROUND_STARTED:
                kind = "round.started";
                data = objectMapper.valueToTree(roundStartedData(event.getRoundStarted()));
                break;
            case ROUND_COMPLETED:
                kind = "round.completed";
                data = objectMapper.valueToTree(roundCompletedData(event.getRoundCompleted()));
                break;
            case ASSISTANT_TEXT:
                kind = "assistant.text";
                data = objectMapper.valueToTree(assistantTextData(event.getAssistantText()));
                break;
            case ASSISTANT_REASONING:
                kind = "assistant.reasoning";
                data = objectMapper.valueToTree(assistantTextData(event.getAssistantReasoning()));
                break;
            case ASSISTANT_TOOL_CALL:
                kind = "assistant.tool_call";
                data = structValueToJson(event.getAssistantToolCall(), objectMapper);
                break;
            case TOOL_EXEC_STARTED:
                kind = "tool.exec_started";
                data = objectMapper.valueToTree(mapOf("toolCallId", event.getToolExecStarted().getToolCallId()));
                break;
            case TOOL_EXEC_SUCCEEDED:
                kind = "tool.exec_succeeded";
                data = structValueToJson(event.getToolExecSucceeded(), objectMapper);
                break;
            case TOOL_EXEC_FAILED:
                kind = "tool.exec_failed";
                data = objectMapper.valueToTree(mapOf(
                        "toolCallId", event.getToolExecFailed().getToolCallId(),
                        "error", event.getToolExecFailed().getError()));
                break;
            case ASK_USER_REQUESTED:
                kind = "ask_user.requested";
                data = structValueToJson(event.getAskUserRequested(), objectMapper);
                break;
            case ASK_USER_RESPONDED:
                kind = "ask_user.responded";
                data = structValueToJson(event.getAskUserResponded(), objectMapper);
                break;
            case EXECUTION_STARTED:
                kind = "execution.started";
                data = structValueToJson(event.getExecutionStarted(), objectMapper);
                break;
            case EXECUTION_FINISHED:
                kind = "execution.finished";
                data = structValueToJson(event.getExecutionFinished(), objectMapper);
                break;
            case INVOCATION_PROGRESS:
                kind = "invocation.progress";
                data = structValueToJson(event.getInvocationProgress(), objectMapper);
                break;
            case PAYLOAD_NOT_SET:
                return null;
            default:
                kind = "error";
                data = objectMapper.createObjectNode();
        }
        return new WireEnvelope(eventId, executionId, roundId, kind, data, createdAt);
    }

    public static SseFrame toSseFrame(WatchEventsResponse frame, ObjectMapper objectMapper) {
        switch (frame.getFrameCase()) {
            case COMMITTED_EVENT:
                WireEnvelope envelope = toWireEnvelope(frame.getCommittedEvent(), objectMapper);
                if (envelope == null) {
                    return null;
                }
                ObjectNode root = objectMapper.createObjectNode();
                if (StringUtils.hasText(envelope.eventId)) {
                    root.put("eventId", envelope.eventId);
                }
                if (StringUtils.hasText(envelope.executionId)) {
                    root.put("executionId", envelope.executionId);
                }
                if (StringUtils.hasText(envelope.roundId)) {
                    root.put("roundId", envelope.roundId);
                }
                root.put("kind", envelope.kind);
                root.set("data", envelope.data);
                if (StringUtils.hasText(envelope.createdAt)) {
                    root.put("createdAt", envelope.createdAt);
                }
                try {
                    return new SseFrame(envelope.kind, 0L, objectMapper.writeValueAsString(root));
                } catch (Exception e) {
                    return new SseFrame(envelope.kind, 0L, "{}");
                }
            case DELTA:
                return deltaFrame(frame.getDelta(), objectMapper);
            case STREAM_END:
                return new SseFrame("stream.end", 0L, "{}");
            case FRAME_NOT_SET:
                return null;
            default:
                return null;
        }
    }

    private static SseFrame deltaFrame(Delta delta, ObjectMapper objectMapper) {
        String kind = delta.getKind() == DeltaBlockKind.DELTA_BLOCK_KIND_REASONING
                ? "reasoning.delta" : "text.delta";
        Map<String, Object> payload = new LinkedHashMap<>();
        payload.put("roundId", delta.getRoundId());
        payload.put("gen", delta.getGeneration());
        payload.put("idx", delta.getIndex());
        payload.put("block", delta.getBlock());
        payload.put("kind", kind);
        payload.put("text", delta.getText());
        try {
            return new SseFrame(kind, 0L, objectMapper.writeValueAsString(payload));
        } catch (Exception e) {
            return new SseFrame(kind, 0L, "{}");
        }
    }

    private static Map<String, Object> roundStartedData(RoundStarted started) {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("model", started.getModel());
        m.put("dialect", started.getDialect());
        m.put("profileVersion", started.getProfileVersion());
        m.put("promptSha256", started.getPromptSha256());
        m.put("tools", started.getToolsList());
        m.put("workerId", started.getWorkerId());
        return m;
    }

    private static Map<String, Object> roundCompletedData(RoundCompleted completed) {
        Map<String, Object> m = new LinkedHashMap<>();
        if (completed.hasUsage()) {
            m.put("usage", mapOf(
                    "inputTokens", completed.getUsage().getInputTokens(),
                    "outputTokens", completed.getUsage().getOutputTokens(),
                    "cacheReadTokens", completed.getUsage().getCacheReadTokens()));
        }
        m.put("finishReason", completed.getFinishReason());
        return m;
    }

    private static Map<String, Object> assistantTextData(AssistantText text) {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("block", text.getBlock());
        m.put("text", text.getText());
        return m;
    }

    private static Map<String, Object> assistantTextData(AssistantReasoning text) {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("block", text.getBlock());
        m.put("text", text.getText());
        return m;
    }

    private static JsonNode structValueToJson(Object proto, ObjectMapper objectMapper) {
        try {
            return objectMapper.readTree(com.google.protobuf.util.JsonFormat.printer()
                    .includingDefaultValueFields()
                    .print((com.google.protobuf.Message) proto));
        } catch (Exception e) {
            return objectMapper.createObjectNode();
        }
    }

    static String formatTimestamp(Timestamp timestamp) {
        if (timestamp == null || (timestamp.getSeconds() == 0 && timestamp.getNanos() == 0)) {
            return "";
        }
        Instant instant = Instant.ofEpochSecond(timestamp.getSeconds(), timestamp.getNanos());
        return instant.toString();
    }

    private static Map<String, Object> mapOf(Object... kv) {
        Map<String, Object> map = new LinkedHashMap<>();
        for (int i = 0; i < kv.length; i += 2) {
            map.put(String.valueOf(kv[i]), kv[i + 1]);
        }
        return map;
    }

    static final class WireEnvelope {
        final String eventId;
        final String executionId;
        final String roundId;
        final String kind;
        final JsonNode data;
        final String createdAt;

        WireEnvelope(String eventId, String executionId, String roundId, String kind,
                JsonNode data, String createdAt) {
            this.eventId = eventId;
            this.executionId = executionId;
            this.roundId = roundId;
            this.kind = kind;
            this.data = data;
            this.createdAt = createdAt;
        }
    }

    public static final class SseFrame {
        public final String event;
        public final long seq;
        public final String dataJson;

        SseFrame(String event, long seq, String dataJson) {
            this.event = event;
            this.seq = seq;
            this.dataJson = dataJson;
        }
    }
}
