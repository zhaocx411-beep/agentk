package com.kuaishou.agent.operation.backend.agentk.tts;

import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

import org.springframework.util.StringUtils;

/**
 * 语音合成引擎与默认音色（对齐 agent-k models catalog 未配 voices 时的内置候选）。
 */
public enum AgentKTtsEngine {

    QWEN("qwen", "Qwen", "generate_speech_qwen", "qwen3-tts-flash", "Cherry",
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
            voice("Jennifer", "珍妮弗：知性磁性女声")),

    MINIMAX("minimax", "MiniMax", "generate_speech_minimax", "minimax-speech-2.8-hd", "Wise_Woman",
            voice("Chinese (Mandarin)_Lyrical_Voice", "中文普通话抒情"),
            voice("Chinese (Mandarin)_HK_Flight_Attendant", "中文普通话空乘"),
            voice("Wise_Woman", "威严成熟女声"),
            voice("Calm_Woman", "沉稳女声"),
            voice("Lively_Girl", "活泼年轻女声"),
            voice("Lovely_Girl", "可爱年轻女声"),
            voice("Deep_Voice_Man", "低沉威严男声"),
            voice("Elegant_Man", "儒雅男声"),
            voice("Patient_Man", "温和中年男声"),
            voice("Casual_Guy", "随性男声"),
            voice("Decent_Boy", "干净年轻男声"));

    private final String id;
    private final String displayName;
    private final String tool;
    private final String model;
    private final String defaultVoiceId;
    private final List<Voice> voices;
    private final Map<String, Voice> byVoiceId;

    AgentKTtsEngine(String id, String displayName, String tool, String model, String defaultVoiceId,
            Voice... voices) {
        this.id = id;
        this.displayName = displayName;
        this.tool = tool;
        this.model = model;
        this.defaultVoiceId = defaultVoiceId;
        this.voices = Collections.unmodifiableList(Arrays.asList(voices));
        Map<String, Voice> index = new LinkedHashMap<>();
        for (Voice voice : voices) {
            index.put(voice.voiceId, voice);
        }
        this.byVoiceId = Collections.unmodifiableMap(index);
    }

    public String getId() {
        return id;
    }

    public String getDisplayName() {
        return displayName;
    }

    public String getTool() {
        return tool;
    }

    public String getModel() {
        return model;
    }

    public String getDefaultVoiceId() {
        return defaultVoiceId;
    }

    public List<Voice> getVoices() {
        return voices;
    }

    public boolean hasVoice(String voiceId) {
        return StringUtils.hasText(voiceId) && byVoiceId.containsKey(voiceId.trim());
    }

    public static AgentKTtsEngine require(String raw) {
        AgentKTtsEngine engine = find(raw);
        if (engine == null) {
            throw new IllegalArgumentException("tts.engine must be qwen or minimax");
        }
        return engine;
    }

    public static AgentKTtsEngine find(String raw) {
        if (!StringUtils.hasText(raw)) {
            return null;
        }
        String key = raw.trim().toLowerCase(Locale.ROOT);
        if ("qwen".equals(key) || "dashscope".equals(key) || "generate_speech_qwen".equals(key)) {
            return QWEN;
        }
        if ("minimax".equals(key) || "generate_speech_minimax".equals(key)) {
            return MINIMAX;
        }
        return null;
    }

    private static Voice voice(String voiceId, String displayName) {
        return new Voice(voiceId, displayName);
    }

    public static final class Voice {
        private final String voiceId;
        private final String displayName;

        private Voice(String voiceId, String displayName) {
            this.voiceId = voiceId;
            this.displayName = displayName;
        }

        public String getVoiceId() {
            return voiceId;
        }

        public String getDisplayName() {
            return displayName;
        }
    }
}
