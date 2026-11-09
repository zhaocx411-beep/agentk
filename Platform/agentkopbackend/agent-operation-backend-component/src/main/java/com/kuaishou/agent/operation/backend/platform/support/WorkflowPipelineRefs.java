package com.kuaishou.agent.operation.backend.platform.support;

import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

/**
 * {@code aop_workflow_version.definition_json} 中的 pipeline ref 解析与匹配。
 * 官方 quick 路径的 workflowKey 以 ref 为准（运行时 pin 的唯一标识）。
 */
public final class WorkflowPipelineRefs {

    private static final String PIPELINE_PREFIX = "pipeline:";

    private WorkflowPipelineRefs() {
    }

    /**
     * 目录 / quick 路径使用的 workflowKey：优先 definition_json.ref，否则 pipeline:agentkId。
     */
    public static String resolveCatalogWorkflowKey(String definitionJson, String agentkId,
            String workflowName, ObjectMapper objectMapper) {
        String ref = extractRef(definitionJson, objectMapper);
        if (StringUtils.hasText(ref)) {
            return ref.trim();
        }
        if (StringUtils.hasText(agentkId)) {
            return PIPELINE_PREFIX + agentkId.trim();
        }
        return SessionProfileAssembler.sanitizeWorkflowKey(workflowName);
    }

    /**
     * 判断 path 中的 workflowKey 是否命中该 workflow 已发布版本。
     * 兼容 {@code definition_json.capability} / {@code definition_json.ref} / agentk_id / 展示名。
     */
    public static boolean matchesWorkflowKey(String workflowKey, String definitionJson, String agentkId,
            String workflowName, ObjectMapper objectMapper) {
        if (!StringUtils.hasText(workflowKey)) {
            return false;
        }
        String key = workflowKey.trim();
        String ref = extractRef(definitionJson, objectMapper);
        if (refEquals(key, ref)) {
            return true;
        }
        String capability = extractCapability(definitionJson, objectMapper);
        if (StringUtils.hasText(capability) && (key.equals(capability) || refEquals(key, capability))) {
            return true;
        }
        if (StringUtils.hasText(agentkId) && key.equals(agentkId.trim())) {
            return true;
        }
        if (StringUtils.hasText(agentkId) && refEquals(key, PIPELINE_PREFIX + agentkId.trim())) {
            return true;
        }
        String sanitized = SessionProfileAssembler.sanitizeWorkflowKey(key);
        return sanitized.equals(SessionProfileAssembler.sanitizeWorkflowKey(workflowName))
                || key.equals(workflowName);
    }

    public static String extractRef(String definitionJson, ObjectMapper objectMapper) {
        return extractField(definitionJson, objectMapper, "ref");
    }

    /**
     * 官方目录 {@code pipelineRef}：只读 {@code definition_json.capability}，不从 ref / agentkId 拼接。
     */
    public static String extractCapability(String definitionJson, ObjectMapper objectMapper) {
        return extractField(definitionJson, objectMapper, "capability");
    }

    /**
     * 写入 {@code aop_workflow.agentk_id}：取 definition_json.capability（裸 ID）。
     * 若误写成 {@code pipeline:contrib.xxx} 则去掉前缀。
     */
    public static String extractAgentkId(String definitionJson, ObjectMapper objectMapper) {
        String capability = extractCapability(definitionJson, objectMapper);
        if (!StringUtils.hasText(capability)) {
            return "";
        }
        String id = capability.trim();
        if (id.startsWith(PIPELINE_PREFIX)) {
            id = id.substring(PIPELINE_PREFIX.length()).trim();
        }
        return id;
    }

    private static String extractField(String definitionJson, ObjectMapper objectMapper, String field) {
        if (!StringUtils.hasText(definitionJson) || objectMapper == null) {
            return null;
        }
        try {
            JsonNode root = objectMapper.readTree(definitionJson);
            if (root == null || !root.isObject()) {
                return null;
            }
            String value = root.path(field).asText("").trim();
            return StringUtils.hasText(value) ? value : null;
        } catch (Exception ignored) {
            return null;
        }
    }

    /** 按 pipeline ref 匹配 workflowKey（兼容有无 pipeline: 前缀）。 */
    public static boolean refsEqual(String left, String right) {
        return refEquals(left, right);
    }

    private static boolean refEquals(String left, String right) {
        if (!StringUtils.hasText(left) || !StringUtils.hasText(right)) {
            return false;
        }
        String a = left.trim();
        String b = right.trim();
        if (a.equals(b)) {
            return true;
        }
        return normalizeRef(a).equals(normalizeRef(b));
    }

    private static String normalizeRef(String ref) {
        String trimmed = ref.trim();
        if (trimmed.startsWith(PIPELINE_PREFIX)) {
            return trimmed;
        }
        return PIPELINE_PREFIX + trimmed;
    }
}
