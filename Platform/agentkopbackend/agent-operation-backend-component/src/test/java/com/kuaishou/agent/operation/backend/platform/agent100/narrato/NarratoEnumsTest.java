package com.kuaishou.agent.operation.backend.platform.agent100.narrato;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.platform.agent100.Agent100Definition;
import com.kuaishou.agent.operation.backend.platform.agent100.Agent100PipelineDef;
import com.kuaishou.agent.operation.backend.platform.agent100.Agent100Registry;

class NarratoEnumsTest {

    @Test
    void configExposesV2ModelVoiceAndFieldEnums() {
        Agent100Definition narrato = Agent100Registry.require(NarratoDefinition.AGENT_ID);

        assertThat(narrato.getModelOptions().get("vision"))
                .containsExactly("gemini-3.1-flash-lite", "gemini-3-flash-preview", "gemini-3.1-pro-preview");
        assertThat(narrato.getModelOptions().get("search")).containsExactly("volc-search", "brightdata-serp");
        assertThat(narrato.getModelDefaults().get("search")).isEqualTo("volc-search");
        assertThat(narrato.getModelDefaults()).doesNotContainKey("tts");
        assertThat(narrato.getModelOptions().get("story")).contains("deepseek-v4-pro", "qwen3.5-plus");

        assertThat(narrato.getVoices().get("qwen"))
                .extracting(item -> item.get("id"))
                .contains("Cherry", "Serena", "Jennifer");
        assertThat(narrato.getVoices().get("minimax"))
                .extracting(item -> item.get("id"))
                .contains("Chinese (Mandarin)_Lyrical_Voice");

        assertThat(narrato.getFieldOptions().get("aspect"))
                .extracting(item -> item.get("id"))
                .containsExactly("source", "16:9", "9:16", "1:1");
        assertThat(narrato.getFieldOptions().get("aspect").get(0).get("label"))
                .isEqualTo("跟随原视频");
        assertThat(narrato.getFieldOptions()).doesNotContainKey("ost");
        assertThat(narrato.getFieldOptions().get("ttsProvider"))
                .extracting(item -> item.get("id"))
                .containsExactly("qwen", "minimax");
    }

    @Test
    void pipelinesAdvertiseV2WorkflowTypes() {
        Agent100Definition narrato = Agent100Registry.require(NarratoDefinition.AGENT_ID);
        Agent100PipelineDef create = narrato.findPipeline(NarratoDefinition.PIPELINE_VIDEO);
        assertThat(create.getTemporalType()).isEqualTo(NarratoDefinition.CREATE_WORKFLOW_TYPE);
        assertThat(create.getContractVersion()).isEqualTo("2");
        assertThat(narrato.findPipeline(NarratoDefinition.PIPELINE_SCRIPT).getTemporalType())
                .isEqualTo(NarratoDefinition.GENERATE_WORKFLOW_TYPE);
        assertThat(narrato.findPipeline(NarratoDefinition.PIPELINE_RENDER).getTemporalType())
                .isEqualTo(NarratoDefinition.RENDER_WORKFLOW_TYPE);
    }

    @SuppressWarnings("unchecked")
    @Test
    void inputSchemaUsesV2TtsAndNarrativeProfile() {
        Map<String, Object> create = NarratoSchemas.createInputSchema();
        Map<String, Object> properties = (Map<String, Object>) create.get("properties");
        assertThat((List<String>) create.get("required")).containsExactly("videoUri", "tts");
        assertThat(properties).doesNotContainKey("theme");
        assertThat(properties).doesNotContainKey("voiceId");

        Map<String, Object> aspect = (Map<String, Object>) properties.get("aspect");
        assertThat((List<String>) aspect.get("enum")).containsExactly("source", "16:9", "9:16", "1:1");
        assertThat(aspect.get("default")).isEqualTo("source");

        Map<String, Object> tts = (Map<String, Object>) properties.get("tts");
        Map<String, Object> ttsProps = (Map<String, Object>) tts.get("properties");
        Map<String, Object> provider = (Map<String, Object>) ttsProps.get("provider");
        assertThat((List<String>) provider.get("enum")).containsExactly("qwen", "minimax");

        Map<String, Object> models = (Map<String, Object>) properties.get("models");
        Map<String, Object> modelProps = (Map<String, Object>) models.get("properties");
        assertThat(modelProps).containsKeys("vision", "asr", "search", "story", "script");
        assertThat(modelProps).doesNotContainKey("tts");
        Map<String, Object> vision = (Map<String, Object>) modelProps.get("vision");
        assertThat((List<String>) vision.get("enum")).contains("gemini-3.1-flash-lite");
        assertThat(vision.get("default")).isEqualTo("gemini-3.1-flash-lite");

        Map<String, Object> script = (Map<String, Object>) properties.get("script");
        assertThat(script).isNull();

        Map<String, Object> render = NarratoSchemas.renderInputSchema();
        Map<String, Object> renderProps = (Map<String, Object>) render.get("properties");
        Map<String, Object> scriptSchema = (Map<String, Object>) renderProps.get("script");
        Map<String, Object> scriptProps = (Map<String, Object>) scriptSchema.get("properties");
        assertThat(scriptProps.get("version")).isEqualTo(java.util.Collections.singletonMap("const", "2"));
        assertThat(scriptProps).containsKeys("narrativeProfile", "narrationCues");
        assertThat(scriptProps).doesNotContainKey("segments");
        assertThat(scriptProps).doesNotContainKey("theme");
    }
}
