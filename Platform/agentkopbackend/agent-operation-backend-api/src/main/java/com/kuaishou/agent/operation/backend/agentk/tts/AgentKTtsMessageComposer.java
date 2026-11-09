package com.kuaishou.agent.operation.backend.agentk.tts;

import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKPostMessageRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKTtsHint;

/**
 * 把前端选择的 TTS 引擎/音色拼进发给 agent-k 的 text；用户原文仍单独落库。
 */
public final class AgentKTtsMessageComposer {

    private AgentKTtsMessageComposer() {
    }

    public static String compose(AgentKPostMessageRequest request) {
        String text = request == null ? "" : (request.getText() == null ? "" : request.getText());
        AgentKTtsHint hint = request == null ? null : request.getTts();
        if (hint == null || !hint.isPresent()) {
            return text;
        }
        if (!StringUtils.hasText(hint.getEngine())) {
            throw new IllegalArgumentException("tts.engine is required");
        }
        AgentKTtsEngine engine = AgentKTtsEngine.require(hint.getEngine());
        String voiceId = StringUtils.hasText(hint.getVoiceId())
                ? hint.getVoiceId().trim() : engine.getDefaultVoiceId();
        if (!engine.hasVoice(voiceId)) {
            throw new IllegalArgumentException(
                    "tts.voiceId " + voiceId + " is not valid for engine " + engine.getId());
        }
        String prefix = "【TTS】工具=" + engine.getTool()
                + "；model=" + engine.getModel()
                + "；voiceId=" + voiceId;
        if (!StringUtils.hasText(text)) {
            return prefix;
        }
        return prefix + "\n" + text;
    }
}
