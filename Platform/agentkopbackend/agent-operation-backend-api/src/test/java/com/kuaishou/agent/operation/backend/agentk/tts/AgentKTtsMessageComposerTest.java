package com.kuaishou.agent.operation.backend.agentk.tts;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKPostMessageRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKTtsHint;

class AgentKTtsMessageComposerTest {

    @Test
    void composeWithoutHintKeepsOriginalText() {
        AgentKPostMessageRequest request = new AgentKPostMessageRequest();
        request.setText("请转写这段音频");

        assertThat(AgentKTtsMessageComposer.compose(request)).isEqualTo("请转写这段音频");
    }

    @Test
    void composePrependsQwenVoiceLine() {
        AgentKPostMessageRequest request = new AgentKPostMessageRequest();
        request.setText("你好，这是一段测试。");
        AgentKTtsHint hint = new AgentKTtsHint();
        hint.setEngine("qwen");
        hint.setVoiceId("Cherry");
        request.setTts(hint);

        assertThat(AgentKTtsMessageComposer.compose(request)).isEqualTo(
                "【TTS】工具=generate_speech_qwen；model=qwen3-tts-flash；voiceId=Cherry\n"
                        + "你好，这是一段测试。");
    }

    @Test
    void composeUsesEngineDefaultVoiceWhenVoiceIdOmitted() {
        AgentKPostMessageRequest request = new AgentKPostMessageRequest();
        request.setText("合成这段话");
        AgentKTtsHint hint = new AgentKTtsHint();
        hint.setEngine("minimax");
        request.setTts(hint);

        assertThat(AgentKTtsMessageComposer.compose(request))
                .contains("工具=generate_speech_minimax")
                .contains("voiceId=Wise_Woman");
    }

    @Test
    void composeRejectsVoiceFromAnotherEngine() {
        AgentKPostMessageRequest request = new AgentKPostMessageRequest();
        request.setText("你好");
        AgentKTtsHint hint = new AgentKTtsHint();
        hint.setEngine("minimax");
        hint.setVoiceId("Cherry");
        request.setTts(hint);

        assertThatThrownBy(() -> AgentKTtsMessageComposer.compose(request))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("Cherry");
    }
}
