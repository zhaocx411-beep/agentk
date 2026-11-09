package com.kuaishou.agent.operation.backend.agentk;

import java.util.LinkedHashMap;
import java.util.Map;

import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse;

/**
 * 从会话事件解析作品「创作指令」。
 * <p>
 * 创作指令是<strong>触发工具时的用户原话</strong>（{@code user.input}），按
 * {@code toolCallId} 冻结在 {@code assistant.tool_call} 时刻；
 * <strong>不是</strong> tool args / artifact 里的模型改写 prompt。
 * 这样异步 {@code job.completed} 不会落到后续催进度的 user.input 上。
 */
final class ChatAssetCreationPrompt {

    private ChatAssetCreationPrompt() {
    }

    /**
     * 扫事件流，建立 toolCallId → 触发该次工具的 user.input 文本。
     */
    static Map<String, String> indexToolCallPrompts(Iterable<AgentKListEventsResponse.AgentKEventDto> events) {
        Map<String, String> byToolCallId = new LinkedHashMap<>();
        if (events == null) {
            return byToolCallId;
        }
        String lastUserPrompt = "";
        for (AgentKListEventsResponse.AgentKEventDto event : events) {
            if (event == null || event.getData() == null || !StringUtils.hasText(event.getKind())) {
                continue;
            }
            if ("user.input".equals(event.getKind())) {
                String text = event.getData().path("text").asText("");
                if (StringUtils.hasText(text)) {
                    lastUserPrompt = text;
                }
                continue;
            }
            if ("assistant.tool_call".equals(event.getKind())) {
                String toolCallId = toolCallId(event.getData());
                if (!StringUtils.hasText(toolCallId) || byToolCallId.containsKey(toolCallId)) {
                    continue;
                }
                if (StringUtils.hasText(lastUserPrompt)) {
                    byToolCallId.put(toolCallId.trim(), lastUserPrompt);
                }
            }
        }
        return byToolCallId;
    }

    /**
     * 解析某次产物事件应展示的创作指令：toolCallId 绑定的用户原话，否则回退文案。
     */
    static String resolve(JsonNode eventData, Map<String, String> toolCallPrompts, String fallbackUserPrompt) {
        String toolCallId = toolCallId(eventData);
        if (StringUtils.hasText(toolCallId) && toolCallPrompts != null) {
            String indexed = toolCallPrompts.get(toolCallId.trim());
            if (StringUtils.hasText(indexed)) {
                return indexed;
            }
        }
        return fallbackUserPrompt == null ? "" : fallbackUserPrompt;
    }

    static String toolCallId(JsonNode data) {
        if (data == null || data.isNull() || data.isMissingNode()) {
            return "";
        }
        String id = data.path("toolCallId").asText("");
        if (StringUtils.hasText(id)) {
            return id.trim();
        }
        return data.path("tool_call_id").asText("").trim();
    }

    /**
     * 合并落库时：空不覆盖；更长的创作指令优先（避免催进度短句冲掉已有指令）。
     */
    static String mergePreferCreationPrompt(String existing, String incoming) {
        String oldText = existing == null ? "" : existing.trim();
        String newText = incoming == null ? "" : incoming.trim();
        if (!StringUtils.hasText(newText)) {
            return oldText;
        }
        if (!StringUtils.hasText(oldText)) {
            return newText;
        }
        if (newText.length() >= oldText.length()) {
            return newText;
        }
        return oldText;
    }
}
