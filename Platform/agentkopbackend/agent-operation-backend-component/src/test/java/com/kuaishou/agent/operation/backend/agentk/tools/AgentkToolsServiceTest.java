package com.kuaishou.agent.operation.backend.agentk.tools;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import java.util.Collections;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.kuaishou.agent.operation.backend.agentk.tools.AgentkToolsService.ToolsSnapshot;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkClient;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkClient.AgentkClientException;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkProperties;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.AgentKCapabilityCacheMapper;
import com.kuaishou.agent.operation.backend.platform.entity.AgentKCapabilityCache;

class AgentkToolsServiceTest {

    private final ObjectMapper objectMapper = new ObjectMapper();
    private AgentkClient client;
    private AgentkProperties properties;
    private AgentKCapabilityCacheMapper capabilityCacheMapper;
    private AgentkToolsService service;

    @BeforeEach
    void setUp() {
        client = mock(AgentkClient.class);
        properties = new AgentkProperties();
        properties.setTools(Collections.emptyList());
        capabilityCacheMapper = mock(AgentKCapabilityCacheMapper.class);
        when(capabilityCacheMapper.selectList(any())).thenReturn(Collections.emptyList());
        service = new AgentkToolsService(client, properties, capabilityCacheMapper);
    }

    @Test
    void catalogFromCapabilitiesDoesNotUseMockWhenListFails() {
        when(client.listCapabilities()).thenThrow(new AgentkClientException("down"));

        ToolsSnapshot snapshot = service.catalogFromCapabilities();
        assertThat(snapshot.getSource()).isEqualTo("cache");
        assertThat(snapshot.getTools()).isEmpty();
        assertThat(snapshot.getPipelines()).isEmpty();
        assertThat(snapshot.catalogItems()).isEmpty();
    }

    @Test
    void catalogFromCapabilitiesDoesNotMergeMockPipelines() {
        ObjectNode root = objectMapper.createObjectNode();
        ArrayNode caps = root.putArray("capabilities");
        ObjectNode core = caps.addObject();
        core.put("id", "agentk.compose_video");
        core.put("kind", "activity");
        core.put("toolRef", "tool:agentk.compose_video");
        ObjectNode coreTool = core.putObject("tool");
        coreTool.put("name", "compose_video");
        when(client.listCapabilities()).thenReturn(root);

        ToolsSnapshot snapshot = service.catalogFromCapabilities();
        assertThat(snapshot.getSource()).isEqualTo("agentk");
        assertThat(snapshot.getPipelines()).extracting(AgentkPipelineInfo::getName)
                .doesNotContain("generate_brand_ad");
        assertThat(snapshot.catalogItems()).extracting(AgentkToolInfo::getCapabilityId)
                .containsExactly("agentk.compose_video");
    }

    @Test
    void snapshotFallsBackToMockWhenCapabilitiesFail() {
        when(client.listCapabilities()).thenThrow(new AgentkClientException("down"));

        ToolsSnapshot snapshot = service.snapshot();
        assertThat(snapshot.getSource()).isEqualTo("mock");
        assertThat(snapshot.getTools()).extracting(AgentkToolInfo::getName)
                .contains("kling_image_to_video", "compose_video", "generate_brand_ad",
                        "generate_speech_minimax")
                .doesNotContain("ask_user", "job_status", "list_artifacts", "web_fetch",
                        "generate_speech", "generate_text");
        assertThat(byName(snapshot, "compose_video").getRef())
                .isEqualTo("tool:agentk.compose_video");
        assertThat(snapshot.getPipelines()).extracting(AgentkPipelineInfo::getName)
                .contains("generate_brand_ad");
        assertThat(snapshot.getPipelines().get(0).getRef())
                .isEqualTo("pipeline:agentk.generate_brand_ad");
        assertThat(snapshot.getPipelines().get(0).getInputSchema()).containsKey("properties");
    }

    @Test
    void shouldFallbackToDbCacheWhenCapabilitiesFail() {
        when(client.listCapabilities()).thenThrow(new AgentkClientException("down"));
        AgentKCapabilityCache row = new AgentKCapabilityCache();
        row.setEnabled(true);
        row.setAgentkId("agentk.compose_video");
        row.setToolName("compose_video");
        row.setToolRef("tool:agentk.compose_video");
        row.setToolDescription("拼接");
        row.setToolMode("blocking");
        when(capabilityCacheMapper.selectList(any())).thenReturn(Collections.singletonList(row));

        ToolsSnapshot snapshot = service.snapshot();
        assertThat(snapshot.getSource()).isEqualTo("cache");
        assertThat(snapshot.getTools()).extracting(AgentkToolInfo::getName)
                .containsExactly("compose_video");
        assertThat(byName(snapshot, "compose_video").getSource()).isEqualTo("cache");
        assertThat(byName(snapshot, "compose_video").getRef())
                .isEqualTo("tool:agentk.compose_video");
    }

    @Test
    void shouldUseRemoteCapabilitiesOnlyWithoutLocalMechanismTools() {
        ObjectNode root = objectMapper.createObjectNode();
        ArrayNode caps = root.putArray("capabilities");

        ObjectNode core = caps.addObject();
        core.put("id", "agentk.compose_video");
        core.put("toolRef", "tool:agentk.compose_video");
        ObjectNode coreTool = core.putObject("tool");
        coreTool.put("name", "compose_video");
        coreTool.put("description", "在线拼接");
        coreTool.put("mode", "blocking");

        ObjectNode item = caps.addObject();
        item.put("id", "contrib.generate_video");
        item.put("toolRef", "tool:contrib.generate_video");
        ObjectNode tool = item.putObject("tool");
        tool.put("name", "make_video");
        tool.put("description", "外部生视频");
        tool.put("mode", "background");

        ObjectNode brand = caps.addObject();
        brand.put("id", "agentk.generate_brand_ad");
        brand.put("kind", "activity");
        brand.put("toolRef", "tool:agentk.generate_brand_ad");
        ObjectNode brandTool = brand.putObject("tool");
        brandTool.put("name", "generate_brand_ad");
        brandTool.put("description", "品牌广告");
        brandTool.put("mode", "background");

        ObjectNode pipeItem = caps.addObject();
        pipeItem.put("id", "contrib.storyboard");
        pipeItem.put("kind", "workflow");
        pipeItem.put("pipelineRef", "pipeline:contrib.storyboard");
        pipeItem.putObject("inputSchema").put("type", "object");
        ObjectNode pipeline = pipeItem.putObject("pipeline");
        pipeline.put("name", "storyboard");
        pipeline.put("description", "分镜");
        when(client.listCapabilities()).thenReturn(root);

        ToolsSnapshot snapshot = service.snapshot();
        assertThat(snapshot.getSource()).isEqualTo("agentk");
        assertThat(snapshot.getTools()).extracting(AgentkToolInfo::getName)
                .containsExactly("compose_video", "make_video", "generate_brand_ad");
        assertThat(snapshot.getTools()).extracting(AgentkToolInfo::getName)
                .doesNotContain("ask_user", "job_status", "list_artifacts", "web_fetch",
                        "kling_text_to_video");

        assertThat(byName(snapshot, "compose_video").getSource()).isEqualTo("agentk");
        assertThat(byName(snapshot, "make_video").getRef())
                .isEqualTo("tool:contrib.generate_video");

        assertThat(snapshot.getPipelines()).extracting(AgentkPipelineInfo::getName)
                .contains("generate_brand_ad", "storyboard");
        assertThat(snapshot.catalogItems()).extracting(AgentkToolInfo::getKind)
                .contains("activity", "workflow");
        assertThat(snapshot.catalogItems()).extracting(AgentkToolInfo::getCapabilityId)
                .contains("agentk.compose_video", "contrib.generate_video", "contrib.storyboard",
                        "agentk.generate_brand_ad");
        assertThat(snapshot.catalogItems()).filteredOn(t -> "agentk.generate_brand_ad".equals(t.getCapabilityId()))
                .extracting(AgentkToolInfo::getKind)
                .containsExactlyInAnyOrder("activity", "workflow");
    }

    private static AgentkToolInfo byName(ToolsSnapshot snapshot, String name) {
        return snapshot.getTools().stream()
                .filter(t -> name.equals(t.getName()))
                .findFirst()
                .orElseThrow(() -> new AssertionError("missing tool: " + name));
    }
}
