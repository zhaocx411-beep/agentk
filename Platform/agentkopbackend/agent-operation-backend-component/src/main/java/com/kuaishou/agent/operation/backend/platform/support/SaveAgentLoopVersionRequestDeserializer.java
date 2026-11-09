package com.kuaishou.agent.operation.backend.platform.support;

import java.io.IOException;

import com.fasterxml.jackson.core.JsonParser;
import com.fasterxml.jackson.databind.DeserializationContext;
import com.fasterxml.jackson.databind.JsonDeserializer;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.platform.dto.SaveAgentLoopVersionRequest;

/**
 * 保存版本请求：支持 {@code {"profileYaml":{...}}}，也支持直接把 Profile 对象当请求体。
 */
public class SaveAgentLoopVersionRequestDeserializer extends JsonDeserializer<SaveAgentLoopVersionRequest> {

    private static final ObjectMapper MAPPER = new ObjectMapper();

    @Override
    public SaveAgentLoopVersionRequest deserialize(JsonParser p, DeserializationContext ctxt) throws IOException {
        JsonNode node = p.getCodec().readTree(p);
        SaveAgentLoopVersionRequest request = new SaveAgentLoopVersionRequest();
        if (node == null || node.isNull() || !node.isObject()) {
            return request;
        }
        JsonNode profile = coerceObject(firstPresent(node, "profileYaml", "profile"));
        if (profile == null && looksLikeBareProfile(node)) {
            profile = node;
        }
        request.setProfileYaml(profile);
        return request;
    }

    private static JsonNode firstPresent(JsonNode node, String... names) {
        for (String name : names) {
            JsonNode value = node.get(name);
            if (value != null && !value.isNull() && !value.isMissingNode()) {
                return value;
            }
        }
        return null;
    }

    private static JsonNode coerceObject(JsonNode node) throws IOException {
        if (node == null || node.isNull() || node.isMissingNode()) {
            return null;
        }
        if (node.isObject()) {
            return node;
        }
        if (node.isTextual()) {
            String text = node.asText();
            if (text == null || text.trim().isEmpty()) {
                return null;
            }
            JsonNode parsed = MAPPER.readTree(text);
            return parsed != null && parsed.isObject() ? parsed : null;
        }
        return null;
    }

    private static boolean looksLikeBareProfile(JsonNode node) {
        return node.hasNonNull("defaultModel")
                || node.hasNonNull("system")
                || node.has("tools")
                || node.has("pipelines");
    }
}
