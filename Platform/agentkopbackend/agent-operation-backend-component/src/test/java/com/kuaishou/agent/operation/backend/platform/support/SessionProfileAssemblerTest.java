package com.kuaishou.agent.operation.backend.platform.support;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.Map;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.ObjectMapper;

class SessionProfileAssemblerTest {

    private final ObjectMapper objectMapper = new ObjectMapper();

    @Test
    void mergeDeclBodyUsesWorkflowNameAsKey() throws Exception {
        String profile = "{\"version\":\"v1\",\"system\":\"hi\",\"tools\":[\"echo\"]}";
        String definition = "{"
                + "\"description\":\"echo once\","
                + "\"nodes\":{\"echo\":{\"type\":\"tool\",\"tool\":\"echo\",\"args\":{\"text\":\"${inputs.text}\"}}}"
                + "}";
        String merged = SessionProfileAssembler.mergeWorkflowIntoProfile(profile, "echo_once",
                definition, objectMapper);
        @SuppressWarnings("unchecked")
        Map<String, Object> doc = objectMapper.readValue(merged, Map.class);
        @SuppressWarnings("unchecked")
        Map<String, Object> workflows = (Map<String, Object>) doc.get("workflows");
        assertThat(workflows).containsKey("echo_once");
        assertThat(doc.get("version")).isEqualTo("v1");
        assertThat(doc.get("tools")).asList().contains("echo");
    }

    @Test
    void mergeWorkflowsObject() throws Exception {
        String definition = "{\"workflows\":{\"storyboard\":{\"description\":\"d\",\"nodes\":{\"a\":{\"type\":\"llm\",\"prompt\":\"p\"}}}}}";
        String merged = SessionProfileAssembler.mergeWorkflowIntoProfile("", "ignored", definition, objectMapper);
        @SuppressWarnings("unchecked")
        Map<String, Object> doc = objectMapper.readValue(merged, Map.class);
        @SuppressWarnings("unchecked")
        Map<String, Object> workflows = (Map<String, Object>) doc.get("workflows");
        assertThat(workflows).containsKey("storyboard");
    }

    @Test
    void sanitizeChineseNameFallsBack() {
        String key = SessionProfileAssembler.sanitizeWorkflowKey("分镜工作流");
        assertThat(key).startsWith("workflow_");
        assertThat(SessionProfileAssembler.sanitizeWorkflowKey("echo_once")).isEqualTo("echo_once");
    }
}
