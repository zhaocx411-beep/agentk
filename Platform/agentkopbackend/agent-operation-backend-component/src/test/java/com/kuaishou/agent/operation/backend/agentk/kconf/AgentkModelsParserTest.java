package com.kuaishou.agent.operation.backend.agentk.kconf;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkModelsParser.ParsedCatalog;

class AgentkModelsParserTest {

    @Test
    void shouldParseChatAndVideoModelsWithoutSecrets() {
        String raw = "{"
                + "\"version\":\"models-staging-v1\","
                + "\"models\":["
                + "{\"name\":\"deepseek-v4-pro\",\"baseUrl\":\"https://api.deepseek.com\",\"apiKey\":\"sk-secret-must-not-leak\"},"
                + "{\"name\":\"kling-3.0-t2v\",\"kind\":\"video\",\"api\":\"kling-t2v\",\"modelName\":\"kling-3.0\",\"apiKeyEnv\":\"KLING_API_KEY\"},"
                + "{\"name\":\"kling-3.0-omni\",\"kind\":\"video\",\"api\":\"kling-omni\",\"apiKey\":\"another-secret\"},"
                + "{\"name\":\"kling-3.0\",\"kind\":\"video\",\"accessKey\":\"ak\",\"secretKey\":\"sk\"}"
                + "]}";

        ParsedCatalog catalog = AgentkModelsParser.parse(raw);

        assertThat(catalog.getVersion()).isEqualTo("models-staging-v1");
        assertThat(catalog.chatModelNames()).containsExactly("deepseek-v4-pro");
        assertThat(catalog.allNames()).containsExactly(
                "deepseek-v4-pro", "kling-3.0-t2v", "kling-3.0-omni", "kling-3.0");

        Map<String, List<String>> toolModels = catalog.toolModels();
        assertThat(toolModels.get("kling_text_to_video")).containsExactly("kling-3.0-t2v");
        assertThat(toolModels.get("kling_omni_video")).containsExactly("kling-3.0-omni");
        assertThat(toolModels.get("kling_image_to_video")).containsExactly("kling-3.0");

        AgentkModelInfo chat = catalog.getModels().get(0);
        assertThat(chat.getAttachments()).isNotNull();
        assertThat(chat.getAttachments().getMode()).isEqualTo(AgentkChatAttachments.MODE_NONE);
        assertThat(catalog.getModels().get(1).getAttachments()).isNull();

        for (AgentkModelInfo model : catalog.getModels()) {
            String jsonish = model.toString();
            assertThat(jsonish).doesNotContain("sk-secret");
            assertThat(jsonish).doesNotContain("another-secret");
        }
    }

    @Test
    void shouldAttachMediaCapabilitiesToHardcodedChatModels() {
        ParsedCatalog catalog = AgentkModelsParser.fromChatModelNames(AgentkChatModelCatalog.names());

        assertThat(catalog.chatModelNames()).containsExactly(
                "deepseek-v4-pro",
                "deepseek-v4-flash",
                "qwen3.5-plus",
                "qwen3.7-max",
                "gpt-5.4",
                "claude-sonnet-4-6",
                "claude-haiku-4-5",
                "glm-5.1",
                "kimi-k2.6",
                "gemini-3.1-pro-preview",
                "gemini-3-flash-preview",
                "gemini-3.1-flash-lite");

        assertThat(modeOf(catalog, "deepseek-v4-pro")).isEqualTo("none");
        assertThat(modeOf(catalog, "qwen3.5-plus")).isEqualTo("image");
        assertThat(modeOf(catalog, "gpt-5.4")).isEqualTo("image");
        assertThat(modeOf(catalog, "kimi-k2.6")).isEqualTo("image");
        assertThat(modeOf(catalog, "gemini-3.1-pro-preview")).isEqualTo("all");
        assertThat(modeOf(catalog, "gemini-3-flash-preview")).isEqualTo("all");
        assertThat(modeOf(catalog, "gemini-3.1-flash-lite")).isEqualTo("all");

        AgentkChatAttachments gemini = attachmentsOf(catalog, "gemini-3.1-flash-lite");
        assertThat(gemini.isImage()).isTrue();
        assertThat(gemini.isVideo()).isTrue();
        assertThat(gemini.isAudio()).isTrue();

        AgentkChatAttachments qwen = attachmentsOf(catalog, "qwen3.5-plus");
        assertThat(qwen.isImage()).isTrue();
        assertThat(qwen.isVideo()).isFalse();
        assertThat(qwen.isAudio()).isFalse();
    }

    private static String modeOf(ParsedCatalog catalog, String name) {
        return attachmentsOf(catalog, name).getMode();
    }

    private static AgentkChatAttachments attachmentsOf(ParsedCatalog catalog, String name) {
        return catalog.getModels().stream()
                .filter(model -> name.equals(model.getName()))
                .findFirst()
                .orElseThrow()
                .getAttachments();
    }

    @Test
    void shouldReturnEmptyOnBlankRaw() {
        assertThat(AgentkModelsParser.parse("").isEmpty()).isTrue();
        assertThat(AgentkModelsParser.parse(null).isEmpty()).isTrue();
    }

    @Test
    void shouldRejectNonMappingRoot() {
        assertThatThrownBy(() -> AgentkModelsParser.parse("[]"))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("根节点必须是 mapping");
    }
}
