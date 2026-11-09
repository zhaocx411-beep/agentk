package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkModelsParser;
import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkModelsParser.ParsedCatalog;
import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkModelsService;
import com.kuaishou.agent.operation.backend.agentk.tools.AgentkPipelineInfo;
import com.kuaishou.agent.operation.backend.agentk.tools.AgentkToolInfo;
import com.kuaishou.agent.operation.backend.agentk.tools.AgentkToolsService;
import com.kuaishou.agent.operation.backend.agentk.tools.AgentkToolsService.ToolsSnapshot;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkProperties;
import com.kuaishou.agent.operation.backend.batcheval.service.impl.ProfileCompiler.CompiledProfile;

class ProfileCompilerTest {

    private final ObjectMapper objectMapper = new ObjectMapper();
    private AgentkProperties properties;
    private AgentkModelsService modelsService;
    private AgentkToolsService toolsService;
    private ProfileCompiler compiler;

    @BeforeEach
    void setUp() {
        properties = new AgentkProperties();
        properties.setChatModels(Arrays.asList("deepseek-v4-pro", "qwen3.5-plus"));
        properties.setDefaultChatModel("deepseek-v4-pro");
        properties.setTools(Collections.emptyList());
        Map<String, List<String>> toolModels = new LinkedHashMap<>();
        toolModels.put("kling_text_to_video", Collections.singletonList("kling-3.0-t2v"));
        toolModels.put("kling_omni_video", Collections.singletonList("kling-3.0-omni"));
        properties.setToolModels(toolModels);

        modelsService = mock(AgentkModelsService.class);
        when(modelsService.snapshot()).thenReturn(ParsedCatalog.empty());

        toolsService = mock(AgentkToolsService.class);
        when(toolsService.snapshot()).thenReturn(mockToolsSnapshot(
                tool("echo", null, "mock"),
                tool("kling_text_to_video", null, "mock"),
                tool("kling_omni_video", null, "mock"),
                tool("make_video", "tool:contrib.generate_video", "agentk")));

        compiler = new ProfileCompiler(objectMapper, properties, modelsService, toolsService);
    }

    @Test
    void shouldPreferKconfModelsWhenCompiling() throws Exception {
        when(modelsService.snapshot()).thenReturn(AgentkModelsParser.parse("{"
                + "\"version\":\"staging-v2\","
                + "\"models\":["
                + "{\"name\":\"chat-from-kconf\",\"baseUrl\":\"http://x\"},"
                + "{\"name\":\"kling-from-kconf\",\"kind\":\"video\",\"api\":\"kling-t2v\",\"apiKey\":\"secret\"}"
                + "]}"));
        properties.setDefaultChatModel(null);

        CompiledProfile compiled = compiler.compile(
                "chat-from-kconf", "s", Collections.singletonList("kling_text_to_video"),
                Collections.emptyMap());
        JsonNode parsed = objectMapper.readTree(compiled.getYaml());
        assertThat(parsed.path("defaultModel").asText()).isEqualTo("chat-from-kconf");
        assertThat(parsed.path("toolDefaults").path("kling_text_to_video").path("model").asText())
                .isEqualTo("kling-from-kconf");
    }

    @Test
    void shouldCompileDefaultModelAndToolDefaults() throws Exception {
        Map<String, String> toolDefaults = new LinkedHashMap<>();
        toolDefaults.put("kling_text_to_video", "kling-3.0-t2v");

        CompiledProfile compiled = compiler.compile(
                "deepseek-v4-pro",
                "You are a test agent.",
                Arrays.asList("echo", "kling_text_to_video", "make_video"),
                toolDefaults);

        JsonNode parsed = objectMapper.readTree(compiled.getYaml());
        assertThat(parsed.path("defaultModel").asText()).isEqualTo("deepseek-v4-pro");
        assertThat(parsed.path("system").asText()).isEqualTo("You are a test agent.");
        assertThat(parsed.path("tools")).hasSize(3);
        assertThat(parsed.path("tools").get(0).path("name").asText()).isEqualTo("echo");
        assertThat(parsed.path("tools").get(0).has("ref")).isFalse();
        assertThat(parsed.path("tools").get(1).path("name").asText()).isEqualTo("kling_text_to_video");
        assertThat(parsed.path("tools").get(2).path("name").asText()).isEqualTo("make_video");
        assertThat(parsed.path("tools").get(2).path("ref").asText())
                .isEqualTo("tool:contrib.generate_video");
        assertThat(parsed.path("toolDefaults").path("kling_text_to_video").path("model").asText())
                .isEqualTo("kling-3.0-t2v");
        assertThat(parsed.has("toolModels")).isFalse();
        assertThat(parsed.get("version")).isNull();
        assertThat(compiled.getVersion()).hasSize(12);
    }

    @Test
    void shouldDefaultToolModelWhenRequestOmitsIt() throws Exception {
        CompiledProfile compiled = compiler.compile(
                "qwen3.5-plus",
                "prompt",
                Collections.singletonList("kling_omni_video"),
                Collections.emptyMap());

        JsonNode parsed = objectMapper.readTree(compiled.getYaml());
        assertThat(parsed.path("defaultModel").asText()).isEqualTo("qwen3.5-plus");
        assertThat(parsed.path("toolDefaults").path("kling_omni_video").path("model").asText())
                .isEqualTo("kling-3.0-omni");
    }

    @Test
    void shouldRejectUnknownChatModel() {
        assertThatThrownBy(() -> compiler.compile(
                "unknown-chat", "s", Collections.singletonList("echo"), Collections.emptyMap()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("聊天模型不在模型名单中");
    }

    @Test
    void shouldRejectUnknownTool() {
        assertThatThrownBy(() -> compiler.compile(
                "deepseek-v4-pro", "s", Collections.singletonList("generate_video"), Collections.emptyMap()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("工具不在可选名单中");
    }

    @Test
    void shouldCompilePipelineAndDropSameNameTool() throws Exception {
        when(toolsService.snapshot()).thenReturn(ToolsSnapshot.of("mixed",
                Arrays.asList(tool("echo", null, "mock"), tool("generate_brand_ad", null, "mock")),
                Collections.singletonList(pipeline("generate_brand_ad"))));

        CompiledProfile compiled = compiler.compile(
                "deepseek-v4-pro", "s",
                Arrays.asList("echo", "generate_brand_ad"),
                Collections.emptyMap(),
                "generate_brand_ad");

        assertThat(compiled.getTools()).containsExactly("echo");
        assertThat(compiled.getPipelineName()).isEqualTo("generate_brand_ad");
        assertThat(compiled.getPipelineRef()).isEqualTo("pipeline:agentk.generate_brand_ad");
        JsonNode parsed = objectMapper.readTree(compiled.getYaml());
        assertThat(parsed.path("tools")).hasSize(1);
        assertThat(parsed.path("pipelines")).hasSize(1);
        assertThat(parsed.path("pipelines").get(0).path("ref").asText())
                .isEqualTo("pipeline:agentk.generate_brand_ad");
        assertThat(parsed.path("pipelines").get(0).path("name").asText())
                .isEqualTo("generate_brand_ad");
    }

    @Test
    void shouldOmitPipelinesWhenNoneSelected() throws Exception {
        CompiledProfile compiled = compiler.compile(
                "deepseek-v4-pro", "s", Collections.singletonList("echo"), Collections.emptyMap(), null);

        assertThat(compiled.getPipelineName()).isNull();
        assertThat(objectMapper.readTree(compiled.getYaml()).has("pipelines")).isFalse();
    }

    @Test
    void shouldRejectUnknownPipeline() {
        assertThatThrownBy(() -> compiler.compile(
                "deepseek-v4-pro", "s", Collections.singletonList("echo"), Collections.emptyMap(),
                "no_such_pipeline"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("pipeline 不在可选名单中");
    }

    @Test
    void shouldFindPipelineInputSchemaForRunner() {
        when(toolsService.snapshot()).thenReturn(ToolsSnapshot.of("mixed",
                Collections.singletonList(tool("echo", null, "mock")),
                Collections.singletonList(pipeline("generate_brand_ad"))));

        assertThat(compiler.findPipeline("generate_brand_ad").getInputSchema())
                .containsKey("properties");
    }

    private static ToolsSnapshot mockToolsSnapshot(AgentkToolInfo... tools) {
        return ToolsSnapshot.of("mixed", Arrays.asList(tools));
    }

    private static AgentkPipelineInfo pipeline(String name) {
        AgentkPipelineInfo info = new AgentkPipelineInfo();
        info.setName(name);
        info.setRef("pipeline:agentk." + name);
        info.setCapabilityId("agentk." + name);
        info.setSource("agentk");
        Map<String, Object> schema = new LinkedHashMap<>();
        schema.put("type", "object");
        schema.put("required", Collections.singletonList("title"));
        Map<String, Object> properties = new LinkedHashMap<>();
        properties.put("title", Collections.singletonMap("type", "string"));
        schema.put("properties", properties);
        info.setInputSchema(schema);
        return info;
    }

    private static AgentkToolInfo tool(String name, String ref, String source) {
        AgentkToolInfo info = new AgentkToolInfo();
        info.setName(name);
        info.setRef(ref);
        info.setSource(source);
        return info;
    }
}
