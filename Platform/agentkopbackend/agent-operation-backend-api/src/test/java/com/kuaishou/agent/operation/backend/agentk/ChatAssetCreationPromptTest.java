package com.kuaishou.agent.operation.backend.agentk;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.Arrays;
import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse;

class ChatAssetCreationPromptTest {

    private final ObjectMapper objectMapper = new ObjectMapper();

    @Test
    void shouldUseUserInputAtToolCallNotToolArgsOrLaterProgressPing() {
        String userCreation = "帮我生成一碗花甲粉的图";
        String toolPrompt = "俯拍固定机位，白瓷碗装少油花甲粉，窗边柔光，蒸汽升腾";
        List<AgentKListEventsResponse.AgentKEventDto> events = Arrays.asList(
                event("user.input", objectMapper.createObjectNode().put("text", userCreation)),
                event("assistant.tool_call", toolCall("call_img", "generate_image", toolPrompt)),
                event("user.input", objectMapper.createObjectNode().put("text", "生成好了吗")),
                event("job.completed", jobCompleted("call_img", "https://cdn.example/a.png"))
        );

        Map<String, String> indexed = ChatAssetCreationPrompt.indexToolCallPrompts(events);
        assertThat(indexed.get("call_img")).isEqualTo(userCreation);
        assertThat(indexed.get("call_img")).isNotEqualTo(toolPrompt);

        String resolved = ChatAssetCreationPrompt.resolve(
                events.get(3).getData(), indexed, "生成好了吗");
        assertThat(resolved).isEqualTo(userCreation);
    }

    @Test
    void shouldIgnoreToolArgsEvenWhenUserInputMissingAtIndexTime() {
        // 无 user.input 时不应回退成 tool args.prompt
        List<AgentKListEventsResponse.AgentKEventDto> events = Arrays.asList(
                event("assistant.tool_call", toolCall("call_1", "generate_image", "模型改写后的长提示词")),
                event("job.completed", jobCompleted("call_1", "https://cdn.example/b.png"))
        );
        Map<String, String> indexed = ChatAssetCreationPrompt.indexToolCallPrompts(events);
        assertThat(indexed).doesNotContainKey("call_1");
        assertThat(ChatAssetCreationPrompt.resolve(events.get(1).getData(), indexed, ""))
                .isEmpty();
    }

    @Test
    void mergeShouldKeepLongerCreationPrompt() {
        assertThat(ChatAssetCreationPrompt.mergePreferCreationPrompt(
                "帮我生成一碗花甲粉的图", "生成好了吗"))
                .isEqualTo("帮我生成一碗花甲粉的图");
        assertThat(ChatAssetCreationPrompt.mergePreferCreationPrompt(
                "生成好了吗", "帮我生成一碗花甲粉的图"))
                .isEqualTo("帮我生成一碗花甲粉的图");
    }

    private AgentKListEventsResponse.AgentKEventDto event(String kind, ObjectNode data) {
        AgentKListEventsResponse.AgentKEventDto dto = new AgentKListEventsResponse.AgentKEventDto();
        dto.setKind(kind);
        dto.setSeq(1L);
        dto.setData(data);
        return dto;
    }

    private ObjectNode toolCall(String toolCallId, String toolName, String prompt) {
        ObjectNode args = objectMapper.createObjectNode();
        args.put("prompt", prompt);
        args.put("model", "z-image-turbo");
        ObjectNode data = objectMapper.createObjectNode();
        data.put("toolCallId", toolCallId);
        data.put("toolName", toolName);
        data.set("args", args);
        return data;
    }

    private ObjectNode jobCompleted(String toolCallId, String imageUrl) {
        ObjectNode artifact = objectMapper.createObjectNode();
        artifact.put("type", "image");
        artifact.put("uri", imageUrl);
        ObjectNode data = objectMapper.createObjectNode();
        data.put("toolCallId", toolCallId);
        data.put("jobId", "job_1");
        data.set("artifacts", objectMapper.createArrayNode().add(artifact));
        return data;
    }
}
