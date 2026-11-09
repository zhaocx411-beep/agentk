package com.kuaishou.agent.operation.backend.platform.agent100.narrato;

import com.fasterxml.jackson.databind.JsonNode;

/**
 * 把管理平台入参规范成 Demo v2 契约后再交给 agent-k。
 * v1 字段（theme、顶层 voiceId、script.segments）直接拒绝，避免静默发到上游。
 */
public final class NarratoArgsNormalizer {

    private NarratoArgsNormalizer() {
    }

    public static JsonNode normalize(String pipeline, JsonNode args) {
        if (args == null || args.isNull() || args.isMissingNode()) {
            throw new IllegalArgumentException("args is required");
        }
        if (!args.isObject()) {
            throw new IllegalArgumentException("args must be an object");
        }
        rejectLegacyFields(args);
        if (NarratoDefinition.PIPELINE_RENDER.equals(pipeline)
                || NarratoDefinition.PIPELINE_VIDEO.equals(pipeline)) {
            requireTts(args);
        }
        if (NarratoDefinition.PIPELINE_RENDER.equals(pipeline)) {
            requireV2Script(args.path("script"));
        }
        if (NarratoDefinition.PIPELINE_VIDEO.equals(pipeline)
                || NarratoDefinition.PIPELINE_SCRIPT.equals(pipeline)) {
            requireVideoUri(args);
        }
        return args;
    }

    private static void rejectLegacyFields(JsonNode args) {
        if (args.has("theme")) {
            throw new IllegalArgumentException(
                    "v2 不再接受 theme，题材由策划自动判断（narrativeProfile.primaryGenre）");
        }
        if (args.has("voiceId")) {
            throw new IllegalArgumentException(
                    "v2 不再接受顶层 voiceId，请传 tts: {provider, model, voiceId}");
        }
        JsonNode models = args.get("models");
        if (models != null && models.isObject() && models.has("tts")) {
            throw new IllegalArgumentException(
                    "v2 的 models 不含 tts；配音请使用 tts.provider/model/voiceId");
        }
        JsonNode script = args.get("script");
        if (script != null && script.isObject()) {
            if (script.has("theme") || script.has("segments")) {
                throw new IllegalArgumentException(
                        "v2 脚本使用 narrativeProfile + narrationCues，不再使用 theme/segments");
            }
            JsonNode version = script.get("version");
            if (version != null && !"2".equals(version.asText())) {
                throw new IllegalArgumentException("script.version must be '2'");
            }
        }
    }

    private static void requireVideoUri(JsonNode args) {
        JsonNode uri = args.get("videoUri");
        if (uri == null || !uri.isTextual() || uri.asText().trim().isEmpty()) {
            throw new IllegalArgumentException("videoUri is required");
        }
    }

    private static void requireTts(JsonNode args) {
        JsonNode tts = args.get("tts");
        if (tts == null || !tts.isObject()) {
            throw new IllegalArgumentException("tts is required: {provider, model, voiceId}");
        }
        for (String field : new String[] {"provider", "model", "voiceId"}) {
            JsonNode value = tts.get(field);
            if (value == null || !value.isTextual() || value.asText().trim().isEmpty()) {
                throw new IllegalArgumentException("tts." + field + " is required");
            }
        }
        String provider = tts.get("provider").asText().trim();
        if (!NarratoDefinition.TTS_PROVIDERS.contains(provider)) {
            throw new IllegalArgumentException("tts.provider must be qwen or minimax");
        }
    }

    private static void requireV2Script(JsonNode script) {
        if (script == null || !script.isObject()) {
            throw new IllegalArgumentException("script is required");
        }
        if (!"2".equals(script.path("version").asText())) {
            throw new IllegalArgumentException("script.version must be '2'");
        }
        if (!script.has("narrativeProfile") || !script.get("narrativeProfile").isObject()) {
            throw new IllegalArgumentException("script.narrativeProfile is required");
        }
        JsonNode cues = script.get("narrationCues");
        if (cues == null || !cues.isArray() || cues.size() == 0) {
            throw new IllegalArgumentException("script.narrationCues must contain at least one cue");
        }
    }
}
