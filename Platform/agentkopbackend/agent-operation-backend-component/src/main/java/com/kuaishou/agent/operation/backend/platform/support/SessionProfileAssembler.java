package com.kuaishou.agent.operation.backend.platform.support;

import java.util.Collections;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.regex.Pattern;

import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

/**
 * 建会话时把平台 Workflow 版本内容解析为可合并的定义。
 * 现行出站由 {@code AgentKProfileOutbound} 转成 agent-k {@code pipelines[{capability,name}]}。
 */
public final class SessionProfileAssembler {

    private static final Pattern WORKFLOW_KEY = Pattern.compile("[A-Za-z_][A-Za-z0-9_]{0,63}");

    private SessionProfileAssembler() {
    }

    /**
     * 将已发布 workflow 的 definitionJson 合并进 profile。
     *
     * @param profileYaml     当前 profile JSON（可空；参数名历史遗留）
     * @param workflowName    平台 Workflow 逻辑名，作 workflows 键的兜底
     * @param definitionJson  aop_workflow_version.definition_json
     */
    @SuppressWarnings("unchecked")
    public static String mergeWorkflowIntoProfile(String profileYaml, String workflowName,
            String definitionJson, ObjectMapper objectMapper) {
        if (!StringUtils.hasText(definitionJson)) {
            throw new IllegalArgumentException("workflow definitionJson required");
        }
        Map<String, Object> doc = loadProfileDocument(profileYaml, objectMapper);
        Map<String, Object> workflows = asMutableMap(doc.get("workflows"));
        Map<String, Object> incoming = parseWorkflowDefinitions(definitionJson, workflowName, objectMapper);
        if (incoming.isEmpty()) {
            throw new IllegalArgumentException("workflow definitionJson produced empty workflows");
        }
        workflows.putAll(incoming);
        doc.put("workflows", workflows);
        return dumpJson(doc, objectMapper);
    }

    public static Map<String, Object> parseWorkflowDefinitions(String definitionJson, String fallbackName,
            ObjectMapper objectMapper) {
        JsonNode root;
        try {
            root = objectMapper.readTree(definitionJson);
        } catch (Exception e) {
            throw new IllegalArgumentException("workflow definitionJson is not valid JSON", e);
        }
        if (root == null || !root.isObject()) {
            throw new IllegalArgumentException("workflow definitionJson must be a JSON object");
        }
        if (root.has("workflows") && root.get("workflows").isObject()) {
            return objectMapper.convertValue(root.get("workflows"), Map.class);
        }
        if (root.has("nodes") && root.get("nodes").isObject()) {
            String key = resolveWorkflowKey(root, fallbackName);
            Map<String, Object> body = objectMapper.convertValue(root, Map.class);
            body.remove("name");
            return Collections.singletonMap(key, body);
        }
        if (looksLikeWorkflowMap(root)) {
            return objectMapper.convertValue(root, Map.class);
        }
        throw new IllegalArgumentException(
                "unsupported workflow definitionJson: expect {workflows:{name:decl}}, "
                        + "{name,nodes,...}, or {name:decl}");
    }

    private static boolean looksLikeWorkflowMap(JsonNode root) {
        if (root.size() == 0) {
            return false;
        }
        Iterator<Map.Entry<String, JsonNode>> fields = root.fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> entry = fields.next();
            JsonNode value = entry.getValue();
            if (value == null || !value.isObject() || !value.has("nodes") || !value.get("nodes").isObject()) {
                return false;
            }
        }
        return true;
    }

    private static String resolveWorkflowKey(JsonNode declRoot, String fallbackName) {
        if (declRoot.has("name") && declRoot.get("name").isTextual()
                && StringUtils.hasText(declRoot.get("name").asText())) {
            return sanitizeWorkflowKey(declRoot.get("name").asText());
        }
        return sanitizeWorkflowKey(fallbackName);
    }

    public static String sanitizeWorkflowKey(String raw) {
        if (!StringUtils.hasText(raw)) {
            throw new IllegalArgumentException("workflow name required");
        }
        String trimmed = raw.trim();
        if (WORKFLOW_KEY.matcher(trimmed).matches()) {
            return trimmed;
        }
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < trimmed.length(); i++) {
            char c = trimmed.charAt(i);
            if ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
                if (sb.length() == 0 && (c >= '0' && c <= '9')) {
                    sb.append('w').append('_');
                }
                sb.append(c);
            } else if (c == '-' || c == '.' || Character.isWhitespace(c)) {
                if (sb.length() > 0 && sb.charAt(sb.length() - 1) != '_') {
                    sb.append('_');
                }
            }
        }
        String key = sb.toString().replaceAll("_+", "_");
        if (key.endsWith("_")) {
            key = key.substring(0, key.length() - 1);
        }
        if (StringUtils.hasText(key) && WORKFLOW_KEY.matcher(key).matches()) {
            return key;
        }
        // 中文等展示名无法直接作 JSON key 时，用稳定兜底键
        return "workflow_" + Integer.toHexString(raw.hashCode()).replace("-", "n");
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> loadProfileDocument(String profileJson, ObjectMapper objectMapper) {
        if (!StringUtils.hasText(profileJson)) {
            return new LinkedHashMap<>();
        }
        try {
            Map<String, Object> loaded = objectMapper.readValue(profileJson, Map.class);
            if (loaded == null) {
                return new LinkedHashMap<>();
            }
            return new LinkedHashMap<>(loaded);
        } catch (Exception e) {
            throw new IllegalArgumentException("profile JSON top-level must be an object", e);
        }
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> asMutableMap(Object value) {
        if (value instanceof Map) {
            return new LinkedHashMap<>((Map<String, Object>) value);
        }
        return new LinkedHashMap<>();
    }

    private static String dumpJson(Map<String, Object> doc, ObjectMapper objectMapper) {
        try {
            return objectMapper.writeValueAsString(doc);
        } catch (Exception e) {
            throw new IllegalStateException("profile JSON dump failed", e);
        }
    }
}
