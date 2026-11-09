package com.kuaishou.agent.operation.backend.agentk;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.regex.Pattern;

import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSpecDto;
import com.kuaishou.agent.operation.backend.platform.support.SessionProfileAssembler;

/**
 * profile 规范化：保证出站 JSON 与现行 agent-k Profile schema 一致。
 * <p>
 * agent-k 现行契约（{@code additionalProperties: false}）：
 * <ul>
 *   <li>{@code tools}: {@code [{name, capability?, instructions?}]}，兼容旧字符串数组 / 旧 {@code ref}；
 *       能力工具出站只写裸 {@code capability}，不再写 {@code tool:*} ref</li>
 *   <li>{@code pipelines}: {@code [{capability, name, displayName?}]}，{@code capability} 为裸 ID</li>
 *   <li>禁止：{@code kind/type/taskQueue/timeoutSeconds/toolModels/version/workflows}</li>
 * </ul>
 * 能力工具出站若仅有 name，通过 {@link AgentKToolCapabilityLookup}（capability 表缓存）补
 * {@code capability}；本地工具（如 {@code echo}）保持 name-only。
 */
public final class AgentKProfileOutbound {

    private static final Pattern CANONICAL_NAME = Pattern.compile("^[a-z][a-z0-9_]{0,127}$");
    private static final Pattern CAPABILITY_ID = Pattern.compile(
            "^(agentk\\.[a-z][a-z0-9_]*|contrib\\.[a-z][a-z0-9_]*\\.[a-z][a-z0-9_]*)$");
    private static final String CAPABILITY_TYPE_PREFIX = "agentk.capability.";
    private static final String PIPELINE_REF_PREFIX = "pipeline:";
    private static final String TOOL_REF_PREFIX = "tool:";
    private static final String FIRST_PARTY_OWNER = "agentk";

    private AgentKProfileOutbound() {
    }

    /** 前端 JSON profile → agent-k 出站 profile。 */
    public static Map<String, Object> toAgentkProfileJson(JsonNode profile, ObjectMapper objectMapper) {
        return toAgentkProfileJson(profile, objectMapper, null);
    }

    public static Map<String, Object> toAgentkProfileJson(JsonNode profile, ObjectMapper objectMapper,
            AgentKToolCapabilityLookup toolCapabilityLookup) {
        return normalizeForAgentk(profileToMap(profile, objectMapper), objectMapper, toolCapabilityLookup);
    }

    /** 平台版本 JSON 字符串 → agent-k 出站 profile。 */
    public static Map<String, Object> toAgentkProfileJson(String profileYamlOrJson, ObjectMapper objectMapper) {
        return toAgentkProfileJson(profileYamlOrJson, objectMapper, null);
    }

    public static Map<String, Object> toAgentkProfileJson(String profileYamlOrJson, ObjectMapper objectMapper,
            AgentKToolCapabilityLookup toolCapabilityLookup) {
        return normalizeForAgentk(loadDocument(profileYamlOrJson, objectMapper), objectMapper, toolCapabilityLookup);
    }

    /** @deprecated 仅回归/旧调用兼容；新路径不要再传独立 spec。 */
    @Deprecated
    public static Map<String, Object> toAgentkProfileJson(String profileYamlOrJson, AgentKSpecDto spec,
            ObjectMapper objectMapper) {
        Map<String, Object> doc = loadDocument(profileYamlOrJson, objectMapper);
        mergeSpecIntoDoc(doc, spec);
        return normalizeForAgentk(doc, objectMapper, null);
    }

    public static Map<String, Object> toCreateSessionBody(String title, JsonNode profile, ObjectMapper objectMapper) {
        return toCreateSessionBody(title, profile, objectMapper, null);
    }

    public static Map<String, Object> toCreateSessionBody(String title, JsonNode profile, ObjectMapper objectMapper,
            AgentKToolCapabilityLookup toolCapabilityLookup) {
        Map<String, Object> body = new LinkedHashMap<>();
        body.put("title", title != null ? title : "");
        body.put("profile", toAgentkProfileJson(profile, objectMapper, toolCapabilityLookup));
        return body;
    }

    /** 平台版本原文 → 与前端一致的 JSON object（并尽量对齐 agent-k）。 */
    public static JsonNode yamlOrJsonToJsonNode(String profileYamlOrJson, ObjectMapper objectMapper) {
        return yamlOrJsonToJsonNode(profileYamlOrJson, objectMapper, null);
    }

    public static JsonNode yamlOrJsonToJsonNode(String profileYamlOrJson, ObjectMapper objectMapper,
            AgentKToolCapabilityLookup toolCapabilityLookup) {
        Map<String, Object> normalized = normalizeForAgentk(
                loadDocument(profileYamlOrJson, objectMapper), objectMapper, toolCapabilityLookup);
        return objectMapper.valueToTree(normalized);
    }

    /**
     * 将已发布 workflow 合并进 profile.pipelines（capability + name）。
     * <p>
     * {@code definitionJson} 支持：
     * <ul>
     *   <li>现行：{@code {capability,name,displayName?}}</li>
     *   <li>旧 ref：{@code {ref,name,displayName?}}，出站转成 capability</li>
     *   <li>旧 Decl：{@code {name,kind,type,taskQueue,timeoutSeconds}}</li>
     *   <li>旧平台图：{@code {name,nodes,...}} / {@code {workflows:{...}}}</li>
     * </ul>
     * 定义里已解析出 pipeline 时，不再用中文 workflow 显示名再塞一条假 Decl。
     */
    public static JsonNode mergeWorkflowVersion(JsonNode profile, String workflowName, String definitionJson,
            ObjectMapper objectMapper) {
        ObjectNode root = profile != null && profile.isObject()
                ? (ObjectNode) profile.deepCopy()
                : objectMapper.createObjectNode();

        Map<String, Map<String, Object>> byName = new LinkedHashMap<>();
        collectPipelineEntries(root.get("pipelines"), byName);

        if (StringUtils.hasText(definitionJson)) {
            try {
                JsonNode defRoot = objectMapper.readTree(definitionJson);
                Map<String, Object> pipelineEntry = tryAsPipelineEntry(defRoot);
                if (pipelineEntry != null) {
                    putEntry(byName, pipelineEntry);
                } else {
                    Map<String, Object> incoming = SessionProfileAssembler.parseWorkflowDefinitions(
                            definitionJson, workflowName, objectMapper);
                    for (String name : incoming.keySet()) {
                        if (StringUtils.hasText(name) && !byName.containsKey(name)) {
                            putEntry(byName, pipelineEntryFromName(name));
                        }
                    }
                }
            } catch (IllegalArgumentException e) {
                throw e;
            } catch (Exception e) {
                throw new IllegalArgumentException("workflow definitionJson is not valid JSON", e);
            }
        }
        // 仅当 definition 没产出任何 pipeline 时，才用 workflow 名兜底
        if (byName.isEmpty() && StringUtils.hasText(workflowName)) {
            String key = SessionProfileAssembler.sanitizeWorkflowKey(workflowName);
            putEntry(byName, pipelineEntryFromName(key));
        }

        ArrayNode pipelines = objectMapper.createArrayNode();
        for (Map<String, Object> entry : byName.values()) {
            pipelines.add(objectMapper.valueToTree(entry));
        }
        root.set("pipelines", pipelines);
        root.remove("workflows");
        root.remove("version");
        root.remove("toolModels");
        return root;
    }

    /**
     * 识别种子 / 已规范化的 pipeline 条目（新 ref-only，或旧 Temporal Decl）。
     */
    private static Map<String, Object> tryAsPipelineEntry(JsonNode root) {
        if (root == null || !root.isObject()) {
            return null;
        }
        if (root.has("workflows") || root.has("nodes")) {
            return null;
        }
        if (root.has("capability") || root.has("ref") || root.has("name")) {
            return entryFromJsonNode(root);
        }
        return null;
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> normalizeForAgentk(Map<String, Object> doc, ObjectMapper objectMapper,
            AgentKToolCapabilityLookup toolCapabilityLookup) {
        Object pipelinesRaw = doc.remove("pipelines");
        Object workflowsRaw = doc.remove("workflows");
        Object toolsRaw = doc.remove("tools");

        // agent-k Profile additionalProperties=false：去掉历史 / 未知字段
        doc.remove("version");
        doc.remove("toolModels");
        doc.remove("models");
        doc.remove("catalogVersion");
        // profile 里 tools 与 httpTools 是两个独立字段：
        // - tools：启用给模型的 activity/capability 工具列表
        // - httpTools：会话内声明的 HTTP 低代码工具（有内容才保留）
        // 未使用 HTTP 工具时省略空 httpTools，避免把空数组当成有效声明发出。
        Object httpToolsRaw = doc.get("httpTools");
        if (httpToolsRaw == null
                || (httpToolsRaw instanceof List && ((List<?>) httpToolsRaw).isEmpty())
                || (httpToolsRaw instanceof JsonNode && (((JsonNode) httpToolsRaw).isNull()
                || ((JsonNode) httpToolsRaw).isMissingNode()
                || (((JsonNode) httpToolsRaw).isArray() && ((JsonNode) httpToolsRaw).size() == 0)))) {
            doc.remove("httpTools");
        }

        List<Map<String, Object>> tools = normalizeTools(toolsRaw, toolCapabilityLookup);
        if (!tools.isEmpty()) {
            doc.put("tools", tools);
        } else {
            doc.put("tools", new ArrayList<>());
        }

        Map<String, Map<String, Object>> byName = new LinkedHashMap<>();
        collectPipelineEntries(pipelinesRaw, byName);
        for (String name : asNameList(workflowsRaw)) {
            if (StringUtils.hasText(name) && !byName.containsKey(name)) {
                putEntry(byName, pipelineEntryFromName(name));
            }
        }
        if (!byName.isEmpty()) {
            doc.put("pipelines", new ArrayList<>(byName.values()));
        }

        return objectMapper.convertValue(doc, Map.class);
    }

    private static void mergeSpecIntoDoc(Map<String, Object> doc, AgentKSpecDto spec) {
        if (spec == null) {
            return;
        }
        if (StringUtils.hasText(spec.getModel()) && !doc.containsKey("defaultModel")) {
            doc.put("defaultModel", spec.getModel());
        }
        if (spec.getTools() != null && !spec.getTools().isEmpty() && !doc.containsKey("tools")) {
            doc.put("tools", new ArrayList<>(spec.getTools()));
        }
        // toolModels 已从 agent-k Profile 移除，不再合并
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> profileToMap(JsonNode profile, ObjectMapper objectMapper) {
        if (profile == null || profile.isNull() || profile.isMissingNode()) {
            return new LinkedHashMap<>();
        }
        if (!profile.isObject()) {
            throw new IllegalArgumentException("profile must be a JSON object");
        }
        return objectMapper.convertValue(profile, Map.class);
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> loadDocument(String profileJson, ObjectMapper objectMapper) {
        if (!StringUtils.hasText(profileJson)) {
            return new LinkedHashMap<>();
        }
        try {
            Map<String, Object> asJson = objectMapper.readValue(profileJson.trim(), Map.class);
            return asJson != null ? new LinkedHashMap<>(asJson) : new LinkedHashMap<>();
        } catch (Exception e) {
            throw new IllegalArgumentException("profile must be a JSON object", e);
        }
    }

    private static List<Map<String, Object>> normalizeTools(Object raw,
            AgentKToolCapabilityLookup toolCapabilityLookup) {
        List<Map<String, Object>> tools = new ArrayList<>();
        if (raw == null) {
            return tools;
        }
        if (raw instanceof JsonNode) {
            JsonNode node = (JsonNode) raw;
            if (node.isArray()) {
                for (JsonNode item : node) {
                    Map<String, Object> entry = toolEntryFromJsonNode(item, toolCapabilityLookup);
                    if (entry != null) {
                        tools.add(entry);
                    }
                }
            }
            return tools;
        }
        if (raw instanceof List) {
            for (Object item : (List<?>) raw) {
                Map<String, Object> entry = toolEntryFromObject(item, toolCapabilityLookup);
                if (entry != null) {
                    tools.add(entry);
                }
            }
        }
        return tools;
    }

    private static Map<String, Object> toolEntryFromJsonNode(JsonNode item,
            AgentKToolCapabilityLookup toolCapabilityLookup) {
        if (item == null || item.isNull()) {
            return null;
        }
        if (item.isTextual()) {
            return toolEntryFromName(item.asText(), toolCapabilityLookup);
        }
        if (!item.isObject()) {
            return null;
        }
        String name = item.path("name").asText("").trim();
        if (!StringUtils.hasText(name)) {
            return null;
        }
        Map<String, Object> entry = new LinkedHashMap<>();
        entry.put("name", name);
        if (item.hasNonNull("capability") && StringUtils.hasText(item.get("capability").asText())) {
            entry.put("capability", item.get("capability").asText().trim());
        }
        // 入站旧 ref 只用于推导 capability，出站不再保留 ref
        if (!entry.containsKey("capability")
                && item.hasNonNull("ref")
                && StringUtils.hasText(item.get("ref").asText())) {
            String capabilityFromRef = capabilityIdFromToolRef(item.get("ref").asText().trim());
            if (StringUtils.hasText(capabilityFromRef)) {
                entry.put("capability", capabilityFromRef);
            }
        }
        if (item.hasNonNull("instructions") && StringUtils.hasText(item.get("instructions").asText())) {
            entry.put("instructions", item.get("instructions").asText());
        }
        fillMissingCapability(entry, toolCapabilityLookup);
        return entry;
    }

    private static Map<String, Object> toolEntryFromObject(Object item,
            AgentKToolCapabilityLookup toolCapabilityLookup) {
        if (item == null) {
            return null;
        }
        if (item instanceof CharSequence) {
            return toolEntryFromName(String.valueOf(item), toolCapabilityLookup);
        }
        if (item instanceof Map) {
            Map<?, ?> map = (Map<?, ?>) item;
            Object name = map.get("name");
            if (name == null || !StringUtils.hasText(String.valueOf(name))) {
                return null;
            }
            Map<String, Object> entry = new LinkedHashMap<>();
            entry.put("name", String.valueOf(name).trim());
            Object capability = map.get("capability");
            if (capability != null && StringUtils.hasText(String.valueOf(capability))) {
                entry.put("capability", String.valueOf(capability).trim());
            }
            if (!entry.containsKey("capability") && map.get("ref") != null
                    && StringUtils.hasText(String.valueOf(map.get("ref")))) {
                String capabilityFromRef = capabilityIdFromToolRef(String.valueOf(map.get("ref")).trim());
                if (StringUtils.hasText(capabilityFromRef)) {
                    entry.put("capability", capabilityFromRef);
                }
            }
            Object instructions = map.get("instructions");
            if (instructions != null && StringUtils.hasText(String.valueOf(instructions))) {
                entry.put("instructions", String.valueOf(instructions));
            }
            fillMissingCapability(entry, toolCapabilityLookup);
            return entry;
        }
        return null;
    }

    private static Map<String, Object> toolEntryFromName(String rawName,
            AgentKToolCapabilityLookup toolCapabilityLookup) {
        if (!StringUtils.hasText(rawName)) {
            return null;
        }
        String name = rawName.trim();
        Map<String, Object> entry = new LinkedHashMap<>();
        entry.put("name", name);
        fillMissingCapability(entry, toolCapabilityLookup);
        return entry;
    }

    /**
     * 旧 profile / 种子数据常只有 name；能力工具出站必须带 {@code capability}（裸 ID）。
     * <p>
     * 以 capability 表缓存为准：
     * <ul>
     *   <li>name 能命中缓存 → 补/纠正 capability</li>
     *   <li>已有 capability 但不在缓存（如误写的 {@code agentk.web_fetch}）→ 剥离，
     *       本地工具（web_fetch / echo 等）保持 name-only</li>
     * </ul>
     * 出站不写 {@code ref}。
     */
    private static void fillMissingCapability(Map<String, Object> entry,
            AgentKToolCapabilityLookup toolCapabilityLookup) {
        if (entry == null) {
            return;
        }
        entry.remove("ref");
        if (toolCapabilityLookup == null) {
            return;
        }
        Object nameObj = entry.get("name");
        String name = nameObj == null ? "" : String.valueOf(nameObj).trim();
        String fromCache = StringUtils.hasText(name)
                ? toolCapabilityLookup.findCapabilityIdByToolName(name) : null;

        String existing = null;
        if (entry.containsKey("capability") && entry.get("capability") != null) {
            existing = String.valueOf(entry.get("capability")).trim();
        }
        if (StringUtils.hasText(existing)) {
            if (toolCapabilityLookup.hasCapabilityId(existing)) {
                return;
            }
            // 伪 capability（本地工具被误标，或缓存已下线）——剥掉后再按 name 回填
            entry.remove("capability");
        }
        if (StringUtils.hasText(fromCache)) {
            entry.put("capability", fromCache.trim());
        }
    }

    private static String capabilityIdFromToolRef(String ref) {
        if (!StringUtils.hasText(ref)) {
            return null;
        }
        String trimmed = ref.trim();
        if (trimmed.startsWith(TOOL_REF_PREFIX)) {
            trimmed = trimmed.substring(TOOL_REF_PREFIX.length()).trim();
        }
        return CAPABILITY_ID.matcher(trimmed).matches() ? trimmed : null;
    }

    /** 从 JsonNode / List / Map 收集 pipeline 条目，按 name 去重（先到先得）。 */
    @SuppressWarnings("unchecked")
    private static void collectPipelineEntries(Object raw, Map<String, Map<String, Object>> byName) {
        if (raw == null) {
            return;
        }
        if (raw instanceof JsonNode) {
            JsonNode node = (JsonNode) raw;
            if (node.isArray()) {
                for (JsonNode item : node) {
                    putEntry(byName, entryFromJsonNode(item));
                }
            } else if (node.isObject()) {
                // 旧 map 形：{"storyboard_film": {...}} 或单条 Decl
                if (node.has("capability") || node.has("ref") || node.has("name")
                        || node.has("type") || node.has("kind")) {
                    putEntry(byName, entryFromJsonNode(node));
                } else {
                    node.fields().forEachRemaining(e -> {
                        if (!byName.containsKey(e.getKey())) {
                            putEntry(byName, pipelineEntryFromName(e.getKey()));
                        }
                    });
                }
            }
            return;
        }
        if (raw instanceof List) {
            for (Object item : (List<?>) raw) {
                putEntry(byName, entryFromObject(item));
            }
            return;
        }
        if (raw instanceof Map) {
            Map<?, ?> map = (Map<?, ?>) raw;
            if (map.containsKey("capability") || map.containsKey("ref") || map.containsKey("name")
                    || map.containsKey("type") || map.containsKey("kind")) {
                putEntry(byName, entryFromObject(map));
            } else {
                for (Object key : map.keySet()) {
                    if (key != null && StringUtils.hasText(String.valueOf(key))) {
                        String name = String.valueOf(key).trim();
                        if (!byName.containsKey(name)) {
                            putEntry(byName, pipelineEntryFromName(name));
                        }
                    }
                }
            }
        }
    }

    private static void putEntry(Map<String, Map<String, Object>> byName, Map<String, Object> entry) {
        if (entry == null) {
            return;
        }
        Object name = entry.get("name");
        if (name == null || !StringUtils.hasText(String.valueOf(name))) {
            return;
        }
        String key = String.valueOf(name).trim();
        if (!byName.containsKey(key)) {
            byName.put(key, entry);
        }
    }

    private static Map<String, Object> entryFromJsonNode(JsonNode item) {
        if (item == null || item.isNull()) {
            return null;
        }
        if (item.isTextual()) {
            return pipelineEntryFromName(item.asText().trim());
        }
        if (!item.isObject()) {
            return null;
        }
        String capability = item.path("capability").asText("").trim();
        String name = item.path("name").asText("").trim();
        String ref = item.path("ref").asText("").trim();
        String type = item.path("type").asText("").trim();
        String displayName = item.path("displayName").asText("").trim();
        return buildPipelineEntry(name, capability, ref, type, displayName);
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> entryFromObject(Object item) {
        if (item == null) {
            return null;
        }
        if (item instanceof CharSequence) {
            return pipelineEntryFromName(String.valueOf(item).trim());
        }
        if (item instanceof Map) {
            Map<?, ?> map = (Map<?, ?>) item;
            String capability = map.get("capability") == null ? "" : String.valueOf(map.get("capability")).trim();
            String name = map.get("name") == null ? "" : String.valueOf(map.get("name")).trim();
            String ref = map.get("ref") == null ? "" : String.valueOf(map.get("ref")).trim();
            String type = map.get("type") == null ? "" : String.valueOf(map.get("type")).trim();
            String displayName = map.get("displayName") == null ? "" : String.valueOf(map.get("displayName")).trim();
            return buildPipelineEntry(name, capability, ref, type, displayName);
        }
        return null;
    }

    /**
     * 统一成 agent-k 现行 {@code {capability,name,displayName?}}。
     * 入站仍兼容旧 {@code ref} / Temporal {@code type}；出站不写 {@code ref}，也不用 name 拼 {@code agentk.*}。
     */
    private static Map<String, Object> buildPipelineEntry(String name, String capability, String ref, String type,
            String displayName) {
        String capabilityId = resolveCapabilityId(capability, ref, type);
        if (!StringUtils.hasText(capabilityId)) {
            return null;
        }
        String resolvedName = StringUtils.hasText(name) ? name : capabilityLeaf(capabilityId);
        if (!StringUtils.hasText(resolvedName)) {
            return null;
        }
        String normalizedName = resolvedName.toLowerCase(Locale.ROOT);
        if (!CANONICAL_NAME.matcher(normalizedName).matches()) {
            normalizedName = capabilityLeaf(capabilityId);
        }
        if (!CANONICAL_NAME.matcher(normalizedName).matches()) {
            return null;
        }

        Map<String, Object> entry = new LinkedHashMap<>();
        entry.put("capability", capabilityId);
        entry.put("name", normalizedName);
        if (StringUtils.hasText(displayName)) {
            entry.put("displayName", displayName);
        }
        return entry;
    }

    private static String resolveCapabilityId(String capability, String ref, String type) {
        String fromCapability = normalizeCapabilityId(capability);
        if (fromCapability != null) {
            return fromCapability;
        }
        String fromRef = normalizeCapabilityId(ref);
        if (fromRef != null) {
            return fromRef;
        }
        if (StringUtils.hasText(type)) {
            return capabilityIdFromTemporalType(type.trim());
        }
        return null;
    }

    private static String normalizeCapabilityId(String raw) {
        if (!StringUtils.hasText(raw)) {
            return null;
        }
        String trimmed = raw.trim();
        if (trimmed.startsWith(PIPELINE_REF_PREFIX)) {
            trimmed = trimmed.substring(PIPELINE_REF_PREFIX.length()).trim();
        }
        return CAPABILITY_ID.matcher(trimmed).matches() ? trimmed : null;
    }

    /**
     * {@code agentk.capability.storyboard_film} → {@code agentk.storyboard_film}；
     * 已是 {@code owner.cap} 则原样返回；{@code agentk.pipeline.xxx} 等假 type 丢弃。
     */
    private static String capabilityIdFromTemporalType(String type) {
        if (!StringUtils.hasText(type)) {
            return null;
        }
        if (type.startsWith(CAPABILITY_TYPE_PREFIX)) {
            String leaf = type.substring(CAPABILITY_TYPE_PREFIX.length()).trim();
            if (CANONICAL_NAME.matcher(leaf).matches()) {
                return FIRST_PARTY_OWNER + "." + leaf;
            }
            return null;
        }
        if (CAPABILITY_ID.matcher(type).matches()) {
            return type;
        }
        return null;
    }

    private static String capabilityLeaf(String capabilityId) {
        if (!StringUtils.hasText(capabilityId)) {
            return "";
        }
        int dot = capabilityId.lastIndexOf('.');
        return dot >= 0 ? capabilityId.substring(dot + 1) : capabilityId;
    }

    private static Map<String, Object> pipelineEntryFromName(String name) {
        return buildPipelineEntry(name, null, null, null, null);
    }

    private static List<String> asNameList(Object raw) {
        List<String> names = new ArrayList<>();
        if (raw == null) {
            return names;
        }
        if (raw instanceof List) {
            for (Object item : (List<?>) raw) {
                String name = pipelineNameOf(item);
                if (StringUtils.hasText(name)) {
                    names.add(name);
                }
            }
            return names;
        }
        if (raw instanceof Map) {
            for (Object key : ((Map<?, ?>) raw).keySet()) {
                if (key != null && StringUtils.hasText(String.valueOf(key))) {
                    names.add(String.valueOf(key).trim());
                }
            }
        }
        return names;
    }

    private static String pipelineNameOf(Object item) {
        if (item == null) {
            return null;
        }
        if (item instanceof CharSequence) {
            return String.valueOf(item).trim();
        }
        if (item instanceof Map) {
            Object name = ((Map<?, ?>) item).get("name");
            if (name != null && StringUtils.hasText(String.valueOf(name))) {
                return String.valueOf(name).trim();
            }
        }
        return null;
    }
}
