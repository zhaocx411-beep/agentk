package com.kuaishou.agent.operation.backend.platform.agent100.narrato;

import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Narrato Pipeline 入参 JSON Schema，对齐 Demo v2 {@code contracts/schemas.py}。
 * <p>
 * 相对 Demo：把 {@code models.*} 候选枚举写入 schema，方便管理平台渲染下拉。
 * TTS 走 {@code tts.provider/model/voiceId}，脚本 {@code version} 固定为 {@code "2"}。
 */
public final class NarratoSchemas {

    private NarratoSchemas() {
    }

    public static Map<String, Object> generateInputSchema() {
        Map<String, Object> schema = objectSchema();
        schema.put("required", Collections.singletonList("videoUri"));
        Map<String, Object> properties = new LinkedHashMap<>();
        properties.put("videoUri", stringProp(1));
        properties.put("instructions", stringWithDefault(""));
        properties.put("models", modelsSchema(true));
        properties.put("analysisOptions", analysisOptionsSchema());
        schema.put("properties", properties);
        return schema;
    }

    public static Map<String, Object> renderInputSchema() {
        Map<String, Object> schema = objectSchema();
        schema.put("required", Arrays.asList("script", "tts"));
        Map<String, Object> properties = new LinkedHashMap<>();
        properties.put("script", documentaryScriptSchema());
        properties.put("tts", ttsInputSchema());
        properties.put("aspect", aspectSchema());
        properties.put("narrationVolume", numberRange(0, 2, 1.0));
        properties.put("originalAudioVolume", numberRange(0, 2, 0.2));
        properties.put("subtitles", boolDefault(true));
        schema.put("properties", properties);
        return schema;
    }

    @SuppressWarnings("unchecked")
    public static Map<String, Object> createInputSchema() {
        Map<String, Object> schema = objectSchema();
        schema.put("required", Arrays.asList("videoUri", "tts"));
        Map<String, Object> properties = new LinkedHashMap<>();
        Object generateProps = generateInputSchema().get("properties");
        if (generateProps instanceof Map) {
            properties.putAll((Map<String, Object>) generateProps);
        }
        properties.put("tts", ttsInputSchema());
        properties.put("aspect", aspectSchema());
        properties.put("narrationVolume", numberRange(0, 2, 1.0));
        properties.put("originalAudioVolume", numberRange(0, 2, 0.2));
        properties.put("subtitles", boolDefault(true));
        schema.put("properties", properties);
        return schema;
    }

    static Map<String, Object> documentaryScriptSchema() {
        Map<String, Object> schema = objectSchema();
        schema.put("required", Arrays.asList(
                "version", "sourceVideoUri", "sourceDurationSeconds", "narrativeProfile", "narrationCues"));
        Map<String, Object> properties = new LinkedHashMap<>();
        properties.put("version", Collections.singletonMap("const", NarratoDefinition.CONTRACT_VERSION));
        properties.put("sourceVideoUri", stringProp(1));
        properties.put("sourceDurationSeconds", exclusiveMinNumber(0));
        properties.put("narrativeProfile", narrativeProfileSchema());
        Map<String, Object> cues = new LinkedHashMap<>();
        cues.put("type", "array");
        cues.put("minItems", 1);
        cues.put("maxItems", 500);
        cues.put("items", narrationCueSchema());
        properties.put("narrationCues", cues);
        properties.put("pacingWarning", stringProp(1));
        schema.put("properties", properties);
        return schema;
    }

    private static Map<String, Object> narrativeProfileSchema() {
        Map<String, Object> schema = objectSchema();
        schema.put("required", Arrays.asList(
                "primaryGenre", "secondaryGenres", "thesis", "hook", "style", "pacing",
                "globalTtsInstruction"));
        Map<String, Object> properties = new LinkedHashMap<>();
        properties.put("primaryGenre", stringProp(1));
        Map<String, Object> secondary = new LinkedHashMap<>();
        secondary.put("type", "array");
        secondary.put("maxItems", 3);
        secondary.put("items", stringProp(1));
        properties.put("secondaryGenres", secondary);
        properties.put("thesis", stringProp(1));
        properties.put("hook", stringProp(1));
        properties.put("style", stringProp(1));
        properties.put("pacing", stringProp(1));
        properties.put("globalTtsInstruction", stringProp(1));
        schema.put("properties", properties);
        return schema;
    }

    private static Map<String, Object> narrationCueSchema() {
        Map<String, Object> schema = objectSchema();
        schema.put("required", Arrays.asList(
                "id", "startSeconds", "endSeconds", "narration", "evidenceWindowIndexes",
                "ttsInstruction", "importance"));
        Map<String, Object> properties = new LinkedHashMap<>();
        Map<String, Object> id = stringProp(1);
        id.put("pattern", "^cue-[0-9]{3,}$");
        properties.put("id", id);
        properties.put("startSeconds", numberMin(0));
        properties.put("endSeconds", exclusiveMinNumber(0));
        Map<String, Object> evidenceEnd = new LinkedHashMap<>();
        evidenceEnd.put("type", "number");
        evidenceEnd.put("description",
                "解说所使用的全部画面与 ASR 证据在原片中最晚发生完毕的时间；自动脚本应尽量让 startSeconds 不早于此时间");
        properties.put("evidenceEndSeconds", evidenceEnd);
        properties.put("narration", stringProp(1));
        Map<String, Object> evidence = new LinkedHashMap<>();
        evidence.put("type", "array");
        evidence.put("minItems", 1);
        Map<String, Object> index = new LinkedHashMap<>();
        index.put("type", "integer");
        index.put("minimum", 0);
        evidence.put("items", index);
        properties.put("evidenceWindowIndexes", evidence);
        properties.put("ttsInstruction", stringProp(1));
        Map<String, Object> importance = new LinkedHashMap<>();
        importance.put("type", "integer");
        importance.put("minimum", 1);
        importance.put("maximum", 5);
        properties.put("importance", importance);
        properties.put("emotion", enumString(NarratoDefinition.CUE_EMOTIONS));
        schema.put("properties", properties);
        return schema;
    }

    private static Map<String, Object> ttsInputSchema() {
        Map<String, Object> schema = objectSchema();
        schema.put("required", Arrays.asList("provider", "model", "voiceId"));
        Map<String, Object> properties = new LinkedHashMap<>();
        Map<String, Object> provider = enumString(NarratoDefinition.TTS_PROVIDERS);
        provider.put("default", NarratoDefinition.DEFAULT_TTS_PROVIDER);
        properties.put("provider", provider);
        Map<String, Object> model = stringProp(1);
        model.put("description", "按 provider 在 config.tts.providers[].models 中选择");
        properties.put("model", model);
        Map<String, Object> voiceId = stringProp(1);
        voiceId.put("default", NarratoDefinition.DEFAULT_QWEN_VOICE_ID);
        properties.put("voiceId", voiceId);
        schema.put("properties", properties);
        return schema;
    }

    private static Map<String, Object> modelsSchema(boolean generateSlots) {
        Map<String, Object> schema = objectSchema();
        Map<String, Object> properties = new LinkedHashMap<>();
        Map<String, List<String>> options = NarratoDefinition.modelOptions();
        Map<String, String> defaults = NarratoDefinition.modelDefaults();
        if (generateSlots) {
            properties.put("vision", modelSlotSchema(options.get("vision"), defaults.get("vision")));
            properties.put("asr", modelSlotSchema(options.get("asr"), defaults.get("asr")));
            properties.put("search", modelSlotSchema(options.get("search"), defaults.get("search")));
            properties.put("story", modelSlotSchema(options.get("story"), defaults.get("story")));
            properties.put("script", modelSlotSchema(options.get("script"), defaults.get("script")));
        }
        schema.put("properties", properties);
        return schema;
    }

    private static Map<String, Object> modelSlotSchema(List<String> candidates, String defaultValue) {
        Map<String, Object> schema = enumString(candidates == null ? Collections.emptyList() : candidates);
        if (defaultValue != null) {
            schema.put("default", defaultValue);
        }
        return schema;
    }

    private static Map<String, Object> aspectSchema() {
        Map<String, Object> schema = enumString(NarratoDefinition.ASPECT_VALUES);
        schema.put("default", NarratoDefinition.DEFAULT_ASPECT);
        return schema;
    }

    private static Map<String, Object> analysisOptionsSchema() {
        Map<String, Object> schema = objectSchema();
        Map<String, Object> properties = new LinkedHashMap<>();
        properties.put("asrEnabled", boolDefault(true));
        Map<String, Object> asrLanguage = stringWithDefault("");
        asrLanguage.put("description", "BCP-47 language hint; empty lets ASR auto-detect");
        properties.put("asrLanguage", asrLanguage);
        schema.put("properties", properties);
        return schema;
    }

    private static Map<String, Object> objectSchema() {
        Map<String, Object> schema = new LinkedHashMap<>();
        schema.put("type", "object");
        schema.put("additionalProperties", false);
        return schema;
    }

    private static Map<String, Object> stringProp(int minLength) {
        Map<String, Object> schema = new LinkedHashMap<>();
        schema.put("type", "string");
        if (minLength > 0) {
            schema.put("minLength", minLength);
        }
        return schema;
    }

    private static Map<String, Object> stringWithDefault(String defaultValue) {
        Map<String, Object> schema = stringProp(0);
        schema.put("default", defaultValue);
        return schema;
    }

    private static Map<String, Object> enumString(List<String> values) {
        Map<String, Object> schema = new LinkedHashMap<>();
        schema.put("type", "string");
        schema.put("enum", values);
        return schema;
    }

    private static Map<String, Object> boolDefault(boolean defaultValue) {
        Map<String, Object> schema = new LinkedHashMap<>();
        schema.put("type", "boolean");
        schema.put("default", defaultValue);
        return schema;
    }

    private static Map<String, Object> numberRange(double min, double max, double defaultValue) {
        Map<String, Object> schema = new LinkedHashMap<>();
        schema.put("type", "number");
        schema.put("minimum", min);
        schema.put("maximum", max);
        schema.put("default", defaultValue);
        return schema;
    }

    private static Map<String, Object> numberMin(double min) {
        Map<String, Object> schema = new LinkedHashMap<>();
        schema.put("type", "number");
        schema.put("minimum", min);
        return schema;
    }

    private static Map<String, Object> exclusiveMinNumber(double min) {
        Map<String, Object> schema = new LinkedHashMap<>();
        schema.put("type", "number");
        schema.put("exclusiveMinimum", min);
        return schema;
    }
}
