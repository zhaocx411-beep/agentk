package com.kuaishou.agent.operation.backend.platform.dto;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

import org.springframework.util.StringUtils;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.agentk.AgentKToolCapabilityLookup.ActivityTool;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityViewDto;

import lombok.Data;

/**
 * 官方工具广场返回体：
 * {@code { source, total, tools: [{ name, capability, description, parameters: [...] }] }}。
 * <p>
 * 数据来自 agent-k 实时 {@code ListCapabilities} 全量（activity + workflow），写 Profile 用裸 {@code capability}。
 */
@Data
public class OfficialToolPlazaView {

    /** agentk / empty；前端可忽略。 */
    private String source;
    /** 工具总数，便于前端直接展示。 */
    private int total;
    /** 工具卡片列表。 */
    private List<OfficialToolItem> tools = new ArrayList<>();

    @Data
    public static class OfficialToolItem {
        private String name;
        /** activity / workflow */
        private String kind;
        /** Profile tools[].capability，如 {@code agentk.generate_image}。 */
        private String capability;
        private String description;
        private List<OfficialToolParameter> parameters = new ArrayList<>();
    }

    @Data
    public static class OfficialToolParameter {
        private String name;
        private String type;
        private boolean required;
        private String description;
        @JsonInclude(JsonInclude.Include.ALWAYS)
        private Object defaultValue;
        private List<String> enumValues = new ArrayList<>();
        private List<OfficialToolParameter> children = new ArrayList<>();
    }

    /** 无能力缓存时返回空广场（不再回退 OfficialDefaultTool mock）。 */
    public static OfficialToolPlazaView empty() {
        OfficialToolPlazaView view = new OfficialToolPlazaView();
        view.setSource("empty");
        view.setTotal(0);
        return view;
    }

    /**
     * 将 ListCapabilities / {@code aop_agentk_capability} 全量转为工具广场结构。
     * 不去掉 workflow；前端用 {@code kind} 区分。按 capability id 去重，避免同名跨 kind 被丢掉。
     */
    public static OfficialToolPlazaView fromActivityCapabilities(List<AgentKCapabilityViewDto> capabilities) {
        OfficialToolPlazaView view = new OfficialToolPlazaView();
        view.setSource("agentk");
        if (capabilities == null || capabilities.isEmpty()) {
            view.setTotal(0);
            return view;
        }
        Set<String> seen = new HashSet<>();
        for (AgentKCapabilityViewDto cap : capabilities) {
            if (cap == null) {
                continue;
            }
            String name = StringUtils.hasText(cap.getName()) ? cap.getName().trim() : "";
            String capability = resolveCapability(cap);
            String seenKey = StringUtils.hasText(capability) ? capability : name;
            if (!StringUtils.hasText(name) || !StringUtils.hasText(seenKey) || !seen.add(seenKey)) {
                continue;
            }
            OfficialToolItem item = new OfficialToolItem();
            item.setName(name);
            item.setKind(StringUtils.hasText(cap.getKind()) ? cap.getKind().trim() : "");
            item.setCapability(capability);
            item.setDescription(StringUtils.hasText(cap.getDescription()) ? cap.getDescription() : "");
            item.setParameters(parametersFromInputSchema(cap.getInputSchema()));
            view.getTools().add(item);
        }
        view.setTotal(view.getTools().size());
        return view;
    }

    /** 直接从 capability 缓存 activity 列表构建（与出站补全同一数据源）。 */
    public static OfficialToolPlazaView fromActivityTools(List<ActivityTool> activityTools) {
        OfficialToolPlazaView view = new OfficialToolPlazaView();
        view.setSource("agentk");
        if (activityTools == null || activityTools.isEmpty()) {
            view.setTotal(0);
            return view;
        }
        Set<String> seen = new HashSet<>();
        for (ActivityTool tool : activityTools) {
            if (tool == null || !StringUtils.hasText(tool.getName()) || !seen.add(tool.getName().trim())) {
                continue;
            }
            OfficialToolItem item = new OfficialToolItem();
            item.setName(tool.getName().trim());
            item.setKind("activity");
            item.setCapability(tool.getCapabilityId());
            item.setDescription(tool.getDescription() == null ? "" : tool.getDescription());
            view.getTools().add(item);
        }
        view.setTotal(view.getTools().size());
        return view;
    }

    private static String resolveCapability(AgentKCapabilityViewDto cap) {
        if (StringUtils.hasText(cap.getId())) {
            return cap.getId().trim();
        }
        if (StringUtils.hasText(cap.getToolRef())) {
            String toolRef = cap.getToolRef().trim();
            if (toolRef.startsWith("tool:")) {
                return toolRef.substring("tool:".length()).trim();
            }
            return toolRef;
        }
        return null;
    }

    private static List<OfficialToolParameter> parametersFromInputSchema(JsonNode schema) {
        List<OfficialToolParameter> out = new ArrayList<>();
        if (schema == null || !schema.isObject() || !schema.path("properties").isObject()) {
            return out;
        }
        Set<String> required = new HashSet<>();
        JsonNode requiredNode = schema.get("required");
        if (requiredNode != null && requiredNode.isArray()) {
            for (JsonNode item : requiredNode) {
                if (item != null && item.isTextual()) {
                    required.add(item.asText());
                }
            }
        }
        schema.path("properties").fields().forEachRemaining(entry -> {
            out.add(parameterFromProperty(entry.getKey(), entry.getValue(), required.contains(entry.getKey())));
        });
        return out;
    }

    private static OfficialToolParameter parameterFromProperty(String name, JsonNode schema, boolean required) {
        OfficialToolParameter item = new OfficialToolParameter();
        item.setName(name);
        item.setRequired(required);
        item.setDescription(schema == null ? "" : schema.path("description").asText(""));
        item.setDefaultValue(schema == null || !schema.has("default") ? null : toPlainValue(schema.get("default")));
        item.setEnumValues(new ArrayList<>());
        item.setChildren(new ArrayList<>());
        if (schema != null && schema.has("enum") && schema.get("enum").isArray()) {
            for (JsonNode value : schema.get("enum")) {
                item.getEnumValues().add(value.asText());
            }
        }

        String type = schema == null ? "string" : schema.path("type").asText("string");
        if ("array".equals(type) && schema != null && schema.path("items").isObject()) {
            JsonNode items = schema.path("items");
            if (items.path("properties").isObject()) {
                item.setType("array<object>");
                item.setChildren(parametersFromInputSchema(items));
            } else {
                String itemType = items.path("type").asText("string");
                item.setType("array<" + itemType + ">");
            }
        } else if ("object".equals(type) && schema != null && schema.path("properties").isObject()) {
            item.setType("object");
            item.setChildren(parametersFromInputSchema(schema));
        } else {
            item.setType(type);
        }
        return item;
    }

    /** 把 JSON Schema 的 default 转成普通 Java 值，避免 JsonNode 原样序列化。 */
    private static Object toPlainValue(JsonNode node) {
        if (node == null || node.isNull() || node.isMissingNode()) {
            return null;
        }
        if (node.isBoolean()) {
            return node.booleanValue();
        }
        if (node.isInt()) {
            return node.intValue();
        }
        if (node.isLong()) {
            return node.longValue();
        }
        if (node.isFloatingPointNumber()) {
            return node.doubleValue();
        }
        if (node.isTextual()) {
            return node.asText();
        }
        if (node.isArray()) {
            List<Object> list = new ArrayList<>();
            for (JsonNode child : node) {
                list.add(toPlainValue(child));
            }
            return list;
        }
        if (node.isObject()) {
            return node;
        }
        return node.asText();
    }
}
