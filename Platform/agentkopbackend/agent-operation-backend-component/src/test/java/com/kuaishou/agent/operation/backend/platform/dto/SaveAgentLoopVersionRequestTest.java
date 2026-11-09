package com.kuaishou.agent.operation.backend.platform.dto;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.ObjectMapper;

class SaveAgentLoopVersionRequestTest {

    private final ObjectMapper objectMapper = new ObjectMapper();

    @Test
    void acceptsWrappedProfileYaml() throws Exception {
        SaveAgentLoopVersionRequest request = objectMapper.readValue(
                "{\"profileYaml\":{\"defaultModel\":\"deepseek-v4-pro\",\"tools\":[]}}",
                SaveAgentLoopVersionRequest.class);
        assertThat(request.getProfileYaml().path("defaultModel").asText()).isEqualTo("deepseek-v4-pro");
        assertThat(request.getProfileYaml().path("tools").isArray()).isTrue();
    }

    @Test
    void acceptsBareProfileObject() throws Exception {
        SaveAgentLoopVersionRequest request = objectMapper.readValue(
                "{\"defaultModel\":\"deepseek-v4-pro\",\"system\":\"你是助手\",\"tools\":[],\"pipelines\":[]}",
                SaveAgentLoopVersionRequest.class);
        assertThat(request.getProfileYaml().path("defaultModel").asText()).isEqualTo("deepseek-v4-pro");
        assertThat(request.getProfileYaml().path("system").asText()).isEqualTo("你是助手");
    }
}
