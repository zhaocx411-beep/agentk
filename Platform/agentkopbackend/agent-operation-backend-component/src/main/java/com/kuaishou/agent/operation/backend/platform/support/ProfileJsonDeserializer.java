package com.kuaishou.agent.operation.backend.platform.support;

import java.io.IOException;

import com.fasterxml.jackson.core.JsonParser;
import com.fasterxml.jackson.databind.DeserializationContext;
import com.fasterxml.jackson.databind.JsonDeserializer;
import com.fasterxml.jackson.databind.JsonMappingException;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

/**
 * profile 字段反序列化：支持 JSON object，或可再 parse 的 JSON 字符串。
 */
public class ProfileJsonDeserializer extends JsonDeserializer<JsonNode> {

    private static final ObjectMapper MAPPER = new ObjectMapper();

    @Override
    public JsonNode deserialize(JsonParser p, DeserializationContext ctxt) throws IOException {
        JsonNode node = p.getCodec().readTree(p);
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
            if (parsed != null && parsed.isObject()) {
                return parsed;
            }
            throw JsonMappingException.from(p, "JSON string value must parse to a JSON object");
        }
        throw JsonMappingException.from(p, "field must be a JSON object (or a JSON-object string)");
    }
}
