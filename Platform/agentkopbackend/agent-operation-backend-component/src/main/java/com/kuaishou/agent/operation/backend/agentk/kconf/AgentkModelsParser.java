package com.kuaishou.agent.operation.backend.agentk.kconf;

import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;

import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

/**
 * 解析 agent-k 模型目录文档（JSON）。
 * <p>
 * 只抽取可回显字段；凭证键一律丢弃。派生规则对齐 agent-k {@code modelcatalog}：
 * video 的 api 方言决定进哪个生视频工具。
 */
public final class AgentkModelsParser {

    private static final ObjectMapper MAPPER = new ObjectMapper();

    private AgentkModelsParser() {
    }

    public static ParsedCatalog fromChatModelNames(List<String> names) {
        if (names == null || names.isEmpty()) {
            return ParsedCatalog.empty();
        }
        List<AgentkModelInfo> models = new ArrayList<>();
        LinkedHashSet<String> seen = new LinkedHashSet<>();
        for (String name : names) {
            if (!StringUtils.hasText(name) || !seen.add(name)) {
                continue;
            }
            AgentkModelInfo info = new AgentkModelInfo();
            info.setName(name);
            info.setKind("chat");
            info.setAttachments(AgentkChatModelCatalog.attachmentsOf(name));
            models.add(info);
        }
        return new ParsedCatalog(null, models);
    }

    public static ParsedCatalog parse(String raw) {
        if (!StringUtils.hasText(raw)) {
            return ParsedCatalog.empty();
        }
        JsonNode rootNode;
        try {
            rootNode = MAPPER.readTree(raw);
        } catch (Exception e) {
            throw new IllegalStateException("解析 kconf " + AgentkModelsKconf.KEY + " 失败: " + e.getMessage(), e);
        }
        if (rootNode == null || !rootNode.isObject()) {
            throw new IllegalStateException("kconf " + AgentkModelsKconf.KEY + " 根节点必须是 mapping");
        }
        @SuppressWarnings("unchecked")
        Map<String, Object> root = MAPPER.convertValue(rootNode, Map.class);
        String version = asText(root.get("version"));
        Object modelsNode = root.get("models");
        if (modelsNode == null) {
            return new ParsedCatalog(version, Collections.emptyList());
        }
        if (!(modelsNode instanceof List)) {
            throw new IllegalStateException("kconf " + AgentkModelsKconf.KEY + " 的 models 必须是列表");
        }
        List<AgentkModelInfo> models = new ArrayList<>();
        LinkedHashSet<String> seen = new LinkedHashSet<>();
        for (Object item : (List<?>) modelsNode) {
            if (!(item instanceof Map)) {
                continue;
            }
            @SuppressWarnings("unchecked")
            Map<String, Object> entry = (Map<String, Object>) item;
            String name = asText(entry.get("name"));
            if (!StringUtils.hasText(name) || !seen.add(name)) {
                continue;
            }
            String kind = asText(entry.get("kind"));
            if (!StringUtils.hasText(kind)) {
                kind = "chat";
            }
            String api = asText(entry.get("api"));
            AgentkModelInfo info = new AgentkModelInfo();
            info.setName(name);
            info.setKind(kind);
            info.setApi(api);
            info.setDescription(asText(entry.get("description")));
            info.setTool(resolveTool(kind, api));
            if ("chat".equals(kind)) {
                info.setAttachments(AgentkChatModelCatalog.attachmentsOf(name));
            }
            models.add(info);
        }
        return new ParsedCatalog(version, models);
    }

    /**
     * kind + api → 能力工具名；与 agent-k modelcatalog 注释一致。
     * chat 返回 null。
     */
    static String resolveTool(String kind, String api) {
        if (!StringUtils.hasText(kind) || "chat".equals(kind)) {
            return null;
        }
        switch (kind) {
            case "video":
                if (!StringUtils.hasText(api) || "kling".equals(api)) {
                    return "kling_image_to_video";
                }
                if ("kling-t2v".equals(api)) {
                    return "kling_text_to_video";
                }
                if ("kling-omni".equals(api)) {
                    return "kling_omni_video";
                }
                if ("seedance".equals(api)) {
                    return "seedance_generate_video";
                }
                return null;
            case "image":
                return "generate_image";
            case "image_edit":
                return "edit_image";
            case "media_understand":
                return "understand_media";
            case "search":
                return "web_search";
            case "tts":
                // api 方言决定进哪个 TTS 工具（对齐 agent-k modelcatalog）
                if ("minimax".equals(api)) {
                    return "generate_speech_minimax";
                }
                if ("dashscope".equals(api)) {
                    return "generate_speech_qwen";
                }
                if ("cosyvoice".equals(api)) {
                    return "generate_speech_cosyvoice";
                }
                return null;
            case "asr":
                return "transcribe_audio";
            default:
                return null;
        }
    }

    private static String asText(Object value) {
        if (value == null) {
            return null;
        }
        String text = String.valueOf(value).trim();
        return text.isEmpty() ? null : text;
    }

    /** 解析结果：版本 + 条目；并提供 chat / toolModels 投影。 */
    public static final class ParsedCatalog {
        private final String version;
        private final List<AgentkModelInfo> models;

        private ParsedCatalog(String version, List<AgentkModelInfo> models) {
            this.version = version;
            this.models = models == null ? Collections.emptyList() : Collections.unmodifiableList(models);
        }

        public static ParsedCatalog empty() {
            return new ParsedCatalog(null, Collections.emptyList());
        }

        public String getVersion() {
            return version;
        }

        public List<AgentkModelInfo> getModels() {
            return models;
        }

        public boolean isEmpty() {
            return models.isEmpty();
        }

        public List<String> chatModelNames() {
            List<String> out = new ArrayList<>();
            for (AgentkModelInfo model : models) {
                if ("chat".equals(model.getKind())) {
                    out.add(model.getName());
                }
            }
            return out;
        }

        public List<String> allNames() {
            List<String> out = new ArrayList<>(models.size());
            for (AgentkModelInfo model : models) {
                out.add(model.getName());
            }
            return out;
        }

        /** 工具名 → 该工具 model 入参候选（目录条目名）。 */
        public Map<String, List<String>> toolModels() {
            Map<String, List<String>> out = new LinkedHashMap<>();
            for (AgentkModelInfo model : models) {
                if (!StringUtils.hasText(model.getTool())) {
                    continue;
                }
                out.computeIfAbsent(model.getTool(), key -> new ArrayList<>()).add(model.getName());
            }
            return out;
        }
    }
}
