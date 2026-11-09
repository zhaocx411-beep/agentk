package com.kuaishou.agent.operation.backend.agentk.grpc;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.google.protobuf.util.JsonFormat;
import com.kuaishou.klingai.agentk.api.v1.Profile;
import com.kuaishou.klingai.agentk.api.v1.ProfilePipeline;
import com.kuaishou.klingai.agentk.api.v1.ProfileTool;

/**
 * 平台出站 profile（JSON Map，含 legacy {@code ref}）→ protobuf {@link Profile}。
 */
final class AgentKProfileProtoConverter {

    private static final String TOOL_REF_PREFIX = "tool:";
    private static final String PIPELINE_REF_PREFIX = "pipeline:";

    private AgentKProfileProtoConverter() {
    }

    static Profile toProfile(Map<String, Object> profileMap, ObjectMapper objectMapper) {
        if (profileMap == null || profileMap.isEmpty()) {
            return Profile.getDefaultInstance();
        }
        Map<String, Object> normalized = normalizeProfileMap(profileMap);
        try {
            Profile.Builder builder = Profile.newBuilder();
            String json = objectMapper.writeValueAsString(normalized);
            JsonFormat.parser().ignoringUnknownFields().merge(json, builder);
            return builder.build();
        } catch (java.io.IOException e) {
            throw new IllegalArgumentException("invalid profile for agent-k: " + e.getMessage(), e);
        }
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> normalizeProfileMap(Map<String, Object> source) {
        Map<String, Object> out = new LinkedHashMap<>();
        copyIfPresent(source, out, "defaultModel", "default_model");
        copyIfPresent(source, out, "system", "system");
        copyIfPresent(source, out, "maxToolRounds", "max_tool_rounds");
        if (source.containsKey("toolDefaults")) {
            out.put("toolDefaults", source.get("toolDefaults"));
        }
        if (source.containsKey("tool_defaults")) {
            out.put("toolDefaults", source.get("tool_defaults"));
        }
        Object tools = source.get("tools");
        if (tools != null) {
            out.put("tools", normalizeTools(tools));
        }
        Object pipelines = source.get("pipelines");
        if (pipelines != null) {
            out.put("pipelines", normalizePipelines(pipelines));
        }
        return out;
    }

    private static void copyIfPresent(Map<String, Object> from, Map<String, Object> to,
            String camelKey, String snakeKey) {
        if (from.containsKey(camelKey)) {
            to.put(camelKey, from.get(camelKey));
        } else if (from.containsKey(snakeKey)) {
            to.put(camelKey, from.get(snakeKey));
        }
    }

    @SuppressWarnings("unchecked")
    private static List<Map<String, Object>> normalizeTools(Object raw) {
        List<Map<String, Object>> list = new ArrayList<>();
        if (raw instanceof List) {
            for (Object item : (List<?>) raw) {
                Map<String, Object> entry = toolEntry(item);
                if (entry != null) {
                    list.add(entry);
                }
            }
        }
        return list;
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> toolEntry(Object item) {
        if (item instanceof String) {
            String name = ((String) item).trim();
            if (!StringUtils.hasText(name)) {
                return null;
            }
            Map<String, Object> m = new LinkedHashMap<>();
            m.put("name", name);
            return m;
        }
        if (item instanceof Map) {
            Map<?, ?> map = (Map<?, ?>) item;
            Object name = map.get("name");
            if (name == null || !StringUtils.hasText(String.valueOf(name))) {
                return null;
            }
            Map<String, Object> m = new LinkedHashMap<>();
            m.put("name", String.valueOf(name).trim());
            String capability = firstNonBlank(map.get("capability"), refToCapability(map.get("ref"), TOOL_REF_PREFIX));
            if (StringUtils.hasText(capability)) {
                m.put("capability", capability);
            }
            if (map.containsKey("instructions") && StringUtils.hasText(String.valueOf(map.get("instructions")))) {
                m.put("instructions", String.valueOf(map.get("instructions")));
            }
            return m;
        }
        return null;
    }

    @SuppressWarnings("unchecked")
    private static List<Map<String, Object>> normalizePipelines(Object raw) {
        List<Map<String, Object>> list = new ArrayList<>();
        if (raw instanceof List) {
            for (Object item : (List<?>) raw) {
                Map<String, Object> entry = pipelineEntry(item);
                if (entry != null) {
                    list.add(entry);
                }
            }
        }
        return list;
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> pipelineEntry(Object item) {
        if (item instanceof String) {
            String name = ((String) item).trim();
            if (!StringUtils.hasText(name)) {
                return null;
            }
            Map<String, Object> m = new LinkedHashMap<>();
            m.put("name", name);
            return m;
        }
        if (item instanceof Map) {
            Map<?, ?> map = (Map<?, ?>) item;
            String name = map.get("name") == null ? "" : String.valueOf(map.get("name")).trim();
            if (!StringUtils.hasText(name)) {
                return null;
            }
            Map<String, Object> m = new LinkedHashMap<>();
            m.put("name", name);
            String capability = firstNonBlank(map.get("capability"),
                    refToCapability(map.get("ref"), PIPELINE_REF_PREFIX));
            if (StringUtils.hasText(capability)) {
                m.put("capability", capability);
            }
            Object displayName = map.get("displayName");
            if (displayName == null) {
                displayName = map.get("display_name");
            }
            if (displayName != null && StringUtils.hasText(String.valueOf(displayName))) {
                m.put("displayName", String.valueOf(displayName).trim());
            }
            return m;
        }
        return null;
    }

    private static String refToCapability(Object refObj, String prefix) {
        if (refObj == null || !StringUtils.hasText(String.valueOf(refObj))) {
            return null;
        }
        String ref = String.valueOf(refObj).trim();
        if (ref.startsWith(prefix)) {
            return ref.substring(prefix.length());
        }
        return null;
    }

    private static String firstNonBlank(Object first, String second) {
        if (first != null && StringUtils.hasText(String.valueOf(first))) {
            return String.valueOf(first).trim();
        }
        return second;
    }

    static ProfileTool toProfileTool(String name, String capabilityId, String instructions) {
        ProfileTool.Builder b = ProfileTool.newBuilder().setName(name);
        if (StringUtils.hasText(capabilityId)) {
            b.setCapability(capabilityId);
        }
        if (StringUtils.hasText(instructions)) {
            b.setInstructions(instructions);
        }
        return b.build();
    }

    static ProfilePipeline toProfilePipeline(String name, String capabilityId, String displayName) {
        ProfilePipeline.Builder b = ProfilePipeline.newBuilder().setName(name);
        if (StringUtils.hasText(capabilityId)) {
            b.setCapability(capabilityId);
        }
        if (StringUtils.hasText(displayName)) {
            b.setDisplayName(displayName);
        }
        return b.build();
    }
}
