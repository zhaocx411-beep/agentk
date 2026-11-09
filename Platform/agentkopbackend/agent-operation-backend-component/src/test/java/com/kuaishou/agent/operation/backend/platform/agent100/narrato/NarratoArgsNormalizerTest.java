package com.kuaishou.agent.operation.backend.platform.agent100.narrato;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;

class NarratoArgsNormalizerTest {

    private final ObjectMapper mapper = new ObjectMapper();

    @Test
    void generateAcceptsVideoUriWithoutTheme() throws Exception {
        ObjectNode args = mapper.createObjectNode();
        args.put("videoUri", "https://cdn.test/source.mp4");
        assertThat(NarratoArgsNormalizer.normalize(NarratoDefinition.PIPELINE_SCRIPT, args)
                .path("videoUri").asText()).isEqualTo("https://cdn.test/source.mp4");
    }

    @Test
    void rejectsV1ThemeAndTopLevelVoiceId() throws Exception {
        ObjectNode withTheme = mapper.createObjectNode();
        withTheme.put("videoUri", "https://cdn.test/source.mp4");
        withTheme.put("theme", "default");
        assertThatThrownBy(() -> NarratoArgsNormalizer.normalize(
                NarratoDefinition.PIPELINE_SCRIPT, withTheme))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("theme");

        ObjectNode withVoice = mapper.createObjectNode();
        withVoice.put("videoUri", "https://cdn.test/source.mp4");
        withVoice.put("voiceId", "Cherry");
        assertThatThrownBy(() -> NarratoArgsNormalizer.normalize(
                NarratoDefinition.PIPELINE_VIDEO, withVoice))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("voiceId");
    }

    @Test
    void renderRequiresV2ScriptAndTtsObject() throws Exception {
        ObjectNode args = mapper.createObjectNode();
        args.set("tts", mapper.createObjectNode()
                .put("provider", "qwen")
                .put("model", "qwen3-tts-flash")
                .put("voiceId", "Cherry"));
        ObjectNode script = mapper.createObjectNode();
        script.put("version", "2");
        script.set("narrativeProfile", mapper.createObjectNode().put("primaryGenre", "纪实"));
        script.set("narrationCues", mapper.createArrayNode().add(mapper.createObjectNode()
                .put("id", "cue-001")));
        args.set("script", script);

        assertThat(NarratoArgsNormalizer.normalize(NarratoDefinition.PIPELINE_RENDER, args)
                .path("tts").path("provider").asText()).isEqualTo("qwen");
    }

    @Test
    void renderRejectsV1Segments() throws Exception {
        ObjectNode args = mapper.createObjectNode();
        args.set("tts", mapper.createObjectNode()
                .put("provider", "qwen")
                .put("model", "qwen3-tts-flash")
                .put("voiceId", "Cherry"));
        ObjectNode script = mapper.createObjectNode();
        script.put("version", "1");
        script.set("segments", mapper.createArrayNode());
        args.set("script", script);

        assertThatThrownBy(() -> NarratoArgsNormalizer.normalize(
                NarratoDefinition.PIPELINE_RENDER, args))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("narrativeProfile");
    }
}
