package com.kuaishou.agent.operation.backend.agentk;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.HashMap;
import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

class AgentKProfileOutboundTest {

    private final ObjectMapper objectMapper = new ObjectMapper();

    private final AgentKToolCapabilityLookup lookup = new AgentKToolCapabilityLookup() {
        private final Map<String, String> map = new HashMap<>();
        {
            map.put("generate_image", "agentk.generate_image");
            map.put("edit_image", "agentk.edit_image");
            map.put("read_file", "agentk.read_file");
            map.put("generate_speech_qwen", "agentk.generate_speech_qwen");
        }

        @Override
        public String findCapabilityIdByToolName(String toolName) {
            return map.get(toolName);
        }

        @Override
        public boolean hasCapabilityId(String capabilityId) {
            return map.containsValue(capabilityId);
        }

        @Override
        public List<ActivityTool> listActivityTools() {
            List<ActivityTool> tools = new java.util.ArrayList<>();
            for (Map.Entry<String, String> e : map.entrySet()) {
                tools.add(new ActivityTool(e.getKey(), e.getValue(), "", ""));
            }
            return tools;
        }
    };

    @Test
    void shouldConvertLegacyPipelineDeclToCapabilityOnly() throws Exception {
        JsonNode profile = objectMapper.readTree("{"
                + "\"tools\":[\"echo\",\"current_time\"],"
                + "\"toolModels\":{\"generate_image\":[\"gpt\"]},"
                + "\"pipelines\":[{"
                + "  \"name\":\"storyboard_film\","
                + "  \"kind\":\"workflow\","
                + "  \"type\":\"agentk.capability.storyboard_film\","
                + "  \"taskQueue\":\"agentk\","
                + "  \"timeoutSeconds\":86400"
                + "}]}");

        Map<String, Object> body = AgentKProfileOutbound.toCreateSessionBody("t", profile, objectMapper, lookup);
        @SuppressWarnings("unchecked")
        Map<String, Object> outboundProfile = (Map<String, Object>) body.get("profile");

        assertThat(outboundProfile).doesNotContainKeys("toolModels", "version", "workflows");
        @SuppressWarnings("unchecked")
        List<Map<String, Object>> tools = (List<Map<String, Object>>) outboundProfile.get("tools");
        assertThat(tools).containsExactly(
                mapOf("name", "echo"),
                mapOf("name", "current_time"));

        @SuppressWarnings("unchecked")
        List<Map<String, Object>> pipelines = (List<Map<String, Object>>) outboundProfile.get("pipelines");
        assertThat(pipelines).hasSize(1);
        assertThat(pipelines.get(0)).containsEntry("capability", "agentk.storyboard_film")
                .containsEntry("name", "storyboard_film")
                .doesNotContainKeys("kind", "type", "taskQueue", "timeoutSeconds", "ref");
    }

    @Test
    void shouldFillCapabilityFromCacheForNameOnlyEntries() throws Exception {
        JsonNode profile = objectMapper.readTree("{"
                + "\"defaultModel\":\"deepseek-v4-pro\","
                + "\"tools\":[\"generate_image\",{\"name\":\"edit_image\"},"
                + "{\"name\":\"read_file\"},{\"name\":\"generate_speech_qwen\"},{\"name\":\"echo\"}]}");

        Map<String, Object> outbound = AgentKProfileOutbound.toAgentkProfileJson(profile, objectMapper, lookup);
        @SuppressWarnings("unchecked")
        List<Map<String, Object>> tools = (List<Map<String, Object>>) outbound.get("tools");

        assertThat(tools).hasSize(5);
        assertThat(tools.get(0))
                .containsEntry("name", "generate_image")
                .containsEntry("capability", "agentk.generate_image")
                .doesNotContainKey("ref");
        assertThat(tools.get(1))
                .containsEntry("name", "edit_image")
                .containsEntry("capability", "agentk.edit_image")
                .doesNotContainKey("ref");
        assertThat(tools.get(2))
                .containsEntry("name", "read_file")
                .containsEntry("capability", "agentk.read_file")
                .doesNotContainKey("ref");
        assertThat(tools.get(3))
                .containsEntry("name", "generate_speech_qwen")
                .containsEntry("capability", "agentk.generate_speech_qwen")
                .doesNotContainKey("ref");
        assertThat(tools.get(4))
                .containsEntry("name", "echo")
                .doesNotContainKey("ref")
                .doesNotContainKey("capability");
    }

    @Test
    void shouldKeepExistingCapabilityAndStripToolRef() throws Exception {
        JsonNode profile = objectMapper.readTree("{"
                + "\"tools\":[{"
                + "  \"name\":\"read_file\","
                + "  \"capability\":\"agentk.read_file\""
                + "},{"
                + "  \"ref\":\"tool:agentk.generate_image\","
                + "  \"name\":\"image\""
                + "}]}");

        Map<String, Object> outbound = AgentKProfileOutbound.toAgentkProfileJson(profile, objectMapper, lookup);
        @SuppressWarnings("unchecked")
        List<Map<String, Object>> tools = (List<Map<String, Object>>) outbound.get("tools");
        assertThat(tools.get(0))
                .containsEntry("name", "read_file")
                .containsEntry("capability", "agentk.read_file")
                .doesNotContainKey("ref");
        assertThat(tools.get(1))
                .containsEntry("capability", "agentk.generate_image")
                .containsEntry("name", "image")
                .doesNotContainKey("ref");
    }

    @Test
    void shouldStripFakeCapabilityForLocalWebFetch() throws Exception {
        JsonNode profile = objectMapper.readTree("{"
                + "\"tools\":[{"
                + "  \"name\":\"web_fetch\","
                + "  \"capability\":\"agentk.web_fetch\""
                + "},{"
                + "  \"name\":\"echo\""
                + "}]}");

        Map<String, Object> outbound = AgentKProfileOutbound.toAgentkProfileJson(profile, objectMapper, lookup);
        @SuppressWarnings("unchecked")
        List<Map<String, Object>> tools = (List<Map<String, Object>>) outbound.get("tools");
        assertThat(tools.get(0))
                .containsEntry("name", "web_fetch")
                .doesNotContainKey("capability")
                .doesNotContainKey("ref");
        assertThat(tools.get(1))
                .containsEntry("name", "echo")
                .doesNotContainKey("capability");
    }

    @Test
    void shouldMergePublishedWorkflowWithoutFakeDisplayNameEntry() throws Exception {
        JsonNode merged = AgentKProfileOutbound.mergeWorkflowVersion(
                null,
                "分镜成片",
                "{\"name\":\"storyboard_film\",\"kind\":\"workflow\","
                        + "\"type\":\"agentk.capability.storyboard_film\","
                        + "\"taskQueue\":\"agentk\",\"timeoutSeconds\":86400}",
                objectMapper);

        assertThat(merged.path("pipelines")).hasSize(1);
        assertThat(merged.path("pipelines").get(0).path("capability").asText())
                .isEqualTo("agentk.storyboard_film");
        assertThat(merged.path("pipelines").get(0).path("name").asText())
                .isEqualTo("storyboard_film");
        assertThat(merged.path("pipelines").get(0).has("kind")).isFalse();
        assertThat(merged.path("pipelines").get(0).has("ref")).isFalse();
    }

    @Test
    void shouldKeepModernPipelineCapability() throws Exception {
        JsonNode profile = objectMapper.readTree("{"
                + "\"pipelines\":[{"
                + "  \"capability\":\"agentk.generate_brand_ad\","
                + "  \"name\":\"generate_brand_ad\","
                + "  \"displayName\":\"品牌广告\""
                + "}]}");

        Map<String, Object> outbound = AgentKProfileOutbound.toAgentkProfileJson(profile, objectMapper, lookup);
        @SuppressWarnings("unchecked")
        List<Map<String, Object>> pipelines = (List<Map<String, Object>>) outbound.get("pipelines");
        assertThat(pipelines.get(0))
                .containsEntry("capability", "agentk.generate_brand_ad")
                .containsEntry("name", "generate_brand_ad")
                .containsEntry("displayName", "品牌广告")
                .doesNotContainKey("ref");
    }

    private static Map<String, Object> mapOf(String k, String v) {
        Map<String, Object> map = new java.util.LinkedHashMap<>();
        map.put(k, v);
        return map;
    }
}
