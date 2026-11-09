package com.kuaishou.agent.operation.backend.platform.agent100.narrato;

import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.stream.Collectors;

import com.kuaishou.agent.operation.backend.platform.agent100.Agent100Definition;
import com.kuaishou.agent.operation.backend.platform.agent100.Agent100PipelineDef;

/**
 * Narrato 静态定义，对齐 Demo v2：{@code settings.py}、{@code voice_options.py}、
 * {@code contracts/schemas.py}（version=2 / tts 对象 / narrativeProfile）。
 */
public final class NarratoDefinition {

    public static final String AGENT_ID = "narrato";
    public static final String CONTRACT_VERSION = "2";
    public static final String DEFAULT_MODEL = "deepseek-v4-pro";
    public static final String DEFAULT_TITLE = "Narrato";
    public static final String DEFAULT_ASPECT = "source";
    public static final String DEFAULT_TTS_PROVIDER = "qwen";
    public static final String DEFAULT_QWEN_VOICE_ID = "Cherry";
    public static final String QWEN_TTS_INSTRUCT_MODEL = "qwen3-tts-instruct-flash";
    public static final String QWEN_TTS_FLASH_MODEL = "qwen3-tts-flash";
    public static final String DEFAULT_MINIMAX_TTS_MODEL = "minimax-speech-2.8-hd";

    public static final String PIPELINE_VIDEO = "documentary_video";
    public static final String PIPELINE_SCRIPT = "documentary_script";
    public static final String PIPELINE_RENDER = "documentary_render";

    public static final String CAPABILITY_CREATE = "contrib.narrato.create_documentary_video";
    public static final String CAPABILITY_GENERATE = "contrib.narrato.generate_documentary_script";
    public static final String CAPABILITY_RENDER = "contrib.narrato.render_documentary_video";

    public static final String GENERATE_WORKFLOW_TYPE = "narrato.documentary.generate_script.v2";
    public static final String RENDER_WORKFLOW_TYPE = "narrato.documentary.render_video.v2";
    public static final String CREATE_WORKFLOW_TYPE = "narrato.documentary.create_video.v2";

    public static final List<String> ASPECT_VALUES = Collections.unmodifiableList(Arrays.asList(
            "source", "16:9", "9:16", "1:1"));

    public static final List<String> TTS_PROVIDERS = Collections.unmodifiableList(Arrays.asList(
            "qwen", "minimax"));

    public static final List<String> CUE_EMOTIONS = Collections.unmodifiableList(Arrays.asList(
            "happy", "sad", "angry", "fearful", "disgusted", "surprised", "calm", "fluent", "whisper"));

    /** @deprecated v1 分段 OST；v2 脚本不再使用。 */
    @Deprecated
    public static final List<Integer> OST_VALUES = Collections.unmodifiableList(Arrays.asList(0, 1, 2));

    /** @deprecated 使用 {@link #DEFAULT_QWEN_VOICE_ID}。 */
    @Deprecated
    public static final String DEFAULT_VOICE_ID = DEFAULT_QWEN_VOICE_ID;

    /** @deprecated v2 ASR 默认自动检测，空字符串。 */
    @Deprecated
    public static final String DEFAULT_ASR_LANGUAGE = "";

    private NarratoDefinition() {
    }

    public static Agent100Definition definition() {
        Map<String, List<Map<String, String>>> voices = new LinkedHashMap<>();
        voices.put("qwen", qwenVoices());
        voices.put("minimax", minimaxVoices());
        return new Agent100Definition(
                AGENT_ID,
                "Narrato",
                "N/A",
                "#ff6b48",
                "纪录片解说脚本、配音与成片 Pipeline（契约 v2：题材自动判断，TTS 用 provider/model/voiceId）。",
                DEFAULT_MODEL,
                DEFAULT_TITLE,
                pipelines(),
                modelDefaults(),
                modelOptions(),
                voices,
                fieldOptions());
    }

    public static List<Agent100PipelineDef> pipelines() {
        return Arrays.asList(
                new Agent100PipelineDef(CAPABILITY_CREATE, PIPELINE_VIDEO, "一键成片",
                        NarratoSchemas.createInputSchema(), CREATE_WORKFLOW_TYPE, CONTRACT_VERSION),
                new Agent100PipelineDef(CAPABILITY_GENERATE, PIPELINE_SCRIPT, "生成脚本",
                        NarratoSchemas.generateInputSchema(), GENERATE_WORKFLOW_TYPE, CONTRACT_VERSION),
                new Agent100PipelineDef(CAPABILITY_RENDER, PIPELINE_RENDER, "脚本渲染",
                        NarratoSchemas.renderInputSchema(), RENDER_WORKFLOW_TYPE, CONTRACT_VERSION));
    }

    public static Map<String, String> modelDefaults() {
        Map<String, String> defaults = new LinkedHashMap<>();
        defaults.put("vision", "gemini-3.1-flash-lite");
        defaults.put("asr", "azure-whisper");
        defaults.put("search", "volc-search");
        defaults.put("story", DEFAULT_MODEL);
        defaults.put("script", DEFAULT_MODEL);
        return defaults;
    }

    public static Map<String, List<String>> modelOptions() {
        Map<String, List<String>> options = new LinkedHashMap<>();
        options.put("vision", Arrays.asList(
                "gemini-3.1-flash-lite",
                "gemini-3-flash-preview",
                "gemini-3.1-pro-preview"));
        options.put("asr", Collections.singletonList("azure-whisper"));
        options.put("search", Arrays.asList("volc-search", "brightdata-serp"));
        options.put("story", textModelCandidates());
        options.put("script", textModelCandidates());
        return options;
    }

    public static Map<String, Object> ttsCatalog() {
        Map<String, Object> catalog = new LinkedHashMap<>();
        catalog.put("defaultProvider", DEFAULT_TTS_PROVIDER);
        Map<String, Object> providers = new LinkedHashMap<>();
        providers.put("qwen", ttsProvider(
                "agentk.generate_speech_qwen",
                QWEN_TTS_INSTRUCT_MODEL,
                qwenTtsModels(),
                DEFAULT_QWEN_VOICE_ID,
                qwenVoices()));
        providers.put("minimax", ttsProvider(
                "agentk.generate_speech_minimax",
                DEFAULT_MINIMAX_TTS_MODEL,
                minimaxTtsModels(),
                minimaxVoices().get(0).get("id"),
                minimaxVoices()));
        catalog.put("providers", providers);
        return catalog;
    }

    public static List<String> textModelCandidates() {
        return Arrays.asList(
                "deepseek-v4-pro",
                "deepseek-v4-flash",
                "gpt-5.4",
                "claude-sonnet-4-6",
                "claude-haiku-4-5",
                "glm-5.1",
                "kimi-k2.6",
                "qwen3.7-max",
                "qwen3.5-plus");
    }

    public static List<String> qwenTtsModels() {
        return Arrays.asList(QWEN_TTS_INSTRUCT_MODEL, QWEN_TTS_FLASH_MODEL);
    }

    public static List<String> minimaxTtsModels() {
        return Arrays.asList(
                "minimax-speech-2.8-hd",
                "minimax-speech-2.8-turbo",
                "minimax-speech-2.6-hd",
                "minimax-speech-2.6-turbo",
                "minimax-speech-02-hd",
                "minimax-speech-02-turbo",
                "minimax-speech-01-hd",
                "minimax-speech-01-turbo");
    }

    public static List<String> voiceIds() {
        return qwenVoices().stream().map(item -> item.get("id")).collect(Collectors.toList());
    }

    public static Map<String, List<Map<String, String>>> fieldOptions() {
        Map<String, List<Map<String, String>>> fields = new LinkedHashMap<>();
        fields.put("aspect", aspectOptions());
        fields.put("ttsProvider", Arrays.asList(
                option("qwen", "Qwen"),
                option("minimax", "MiniMax")));
        fields.put("emotion", CUE_EMOTIONS.stream()
                .map(value -> option(value, value))
                .collect(Collectors.toList()));
        fields.put("asrLanguage", Collections.singletonList(
                option("", "自动检测")));
        return fields;
    }

    public static List<Map<String, String>> aspectOptions() {
        return Arrays.asList(
                option("source", "跟随原视频"),
                option("16:9", "16:9"),
                option("9:16", "9:16"),
                option("1:1", "1:1"));
    }

    public static List<Map<String, String>> qwenVoices() {
        return Arrays.asList(
                voice("Cherry", "芊晓：阳光亲切女声"),
                voice("Serena", "佩霞：温婉女声"),
                voice("Ethan", "晨煦：阳光稳重男声"),
                voice("Christie", "千雪：二次元萌系女声"),
                voice("Rono", "元兔：俏皮活泼女声"),
                voice("Vivian", "十三：甜美女声"),
                voice("Ryan", "沐白：清爽大气男声"),
                voice("Rana", "兰月：温柔女声"),
                voice("Kai", "凯：未来感男声"),
                voice("Nafish", "不败枭：低沉男声"),
                voice("Bella", "碧宝：俏皮稳重女声"),
                voice("Jennifer", "珍妮弗：知性磁性女声"));
    }

    public static List<Map<String, String>> minimaxVoices() {
        return Arrays.asList(
                voice("moss_audio_ce44fc67-7ce3-11f0-8de5-96e35d26fb85", "中文系统音色 1"),
                voice("moss_audio_aaa1346a-7ce7-11f0-8e61-2e6e3c7ee85d", "中文系统音色 2"),
                voice("Chinese (Mandarin)_Lyrical_Voice", "中文普通话抒情音色"),
                voice("Chinese (Mandarin)_HK_Flight_Attendant", "中文普通话空乘音色"),
                voice("English_Graceful_Lady", "英文优雅女声"),
                voice("English_Insightful_Speaker", "英文睿智讲述音色"),
                voice("English_radiant_girl", "英文明快女声"),
                voice("English_Persuasive_Man", "英文有说服力男声"),
                voice("moss_audio_6dc281eb-713c-11f0-a447-9613c873494c", "英文系统音色 1"),
                voice("moss_audio_570551b1-735c-11f0-b236-0adeeecad052", "英文系统音色 2"),
                voice("moss_audio_ad5baf92-735f-11f0-8263-fe5a2fe98ec8", "英文系统音色 3"),
                voice("English_Lucky_Robot", "英文幸运机器人音色"),
                voice("Japanese_Whisper_Belle", "日文低语女声"),
                voice("moss_audio_24875c4a-7be4-11f0-9359-4e72c55db738", "日文系统音色 1"),
                voice("moss_audio_7f4ee608-78ea-11f0-bb73-1e2a4cfcd245", "日文系统音色 2"),
                voice("moss_audio_c1a6a3ac-7be6-11f0-8e8e-36b92fbb4f95", "日文系统音色 3"),
                voice("Cantonese_GentleLady", "粤语温柔女声"),
                voice("Cantonese_podcast_host_1", "粤语播客主持音色"));
    }

    private static Map<String, Object> ttsProvider(String capabilityId, String defaultModel,
            List<String> models, String defaultVoiceId, List<Map<String, String>> voices) {
        Map<String, Object> provider = new LinkedHashMap<>();
        provider.put("capabilityId", capabilityId);
        provider.put("defaultModel", defaultModel);
        provider.put("models", models);
        provider.put("defaultVoiceId", defaultVoiceId);
        provider.put("voices", voices);
        return provider;
    }

    private static Map<String, String> voice(String id, String description) {
        Map<String, String> item = new LinkedHashMap<>();
        item.put("id", id);
        item.put("description", description);
        return item;
    }

    private static Map<String, String> option(String id, String label) {
        Map<String, String> item = new LinkedHashMap<>();
        item.put("id", id);
        item.put("label", label);
        return item;
    }
}
