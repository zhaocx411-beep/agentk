package com.kuaishou.agent.operation.backend.agentk;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.Arrays;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto.AgentKCapabilityItemDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto.AgentKCapabilityToolDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityListViewDto;

class AgentKCapabilityLiveViewTest {

    private final AgentKCapabilityCacheService service =
            new AgentKCapabilityCacheService(null, null, new ObjectMapper());

    @Test
    void liveViewKeepsActivityAndFiltersByKind() {
        AgentKCapabilityItemDto activity = new AgentKCapabilityItemDto();
        activity.setId("agentk.generate_image");
        activity.setKind("activity");
        activity.setDescription("文生图");
        AgentKCapabilityToolDto tool = new AgentKCapabilityToolDto();
        tool.setName("generate_image");
        activity.setTool(tool);
        activity.setToolRef("tool:agentk.generate_image");

        AgentKCapabilityItemDto workflow = new AgentKCapabilityItemDto();
        workflow.setId("agentk.generate_brand_ad");
        workflow.setKind("workflow");

        AgentKCapabilitiesDto live = new AgentKCapabilitiesDto();
        live.setCapabilities(Arrays.asList(activity, workflow));

        AgentKCapabilityListViewDto all = service.toFrontendView(live, null, null);
        assertThat(all.getCache().getSource()).isEqualTo("live");
        assertThat(all.getCapabilities()).hasSize(2);

        AgentKCapabilityListViewDto tools = service.toFrontendView(live, "activity", null);
        assertThat(tools.getCapabilities()).hasSize(1);
        assertThat(tools.getCapabilities().get(0).getId()).isEqualTo("agentk.generate_image");
        assertThat(tools.getCapabilities().get(0).getName()).isEqualTo("generate_image");
        assertThat(tools.getCapabilities().get(0).getFields()).isEmpty();
    }

    @Test
    void liveViewMatchesWorkflowKeyAliasesAndIgnoresKind() {
        AgentKCapabilityItemDto workflow = new AgentKCapabilityItemDto();
        workflow.setId("contrib.storyboard_film_go");
        workflow.setKind("activity");
        AgentKCapabilitiesDto.AgentKCapabilityPipelineDto pipeline =
                new AgentKCapabilitiesDto.AgentKCapabilityPipelineDto();
        pipeline.setName("storyboard_film_go");
        workflow.setPipeline(pipeline);
        workflow.setPipelineRef("pipeline:contrib.storyboard_film_go");

        AgentKCapabilitiesDto live = new AgentKCapabilitiesDto();
        live.setCapabilities(Arrays.asList(workflow));

        assertThat(service.toFrontendView(live, "workflow", "pipeline:contrib.storyboard_film_go")
                .getCapabilities()).hasSize(1);
        assertThat(service.toFrontendView(live, "workflow", "storyboard_film_go")
                .getCapabilities().get(0).getId()).isEqualTo("contrib.storyboard_film_go");
        assertThat(service.toFrontendView(live, "workflow", "contrib.missing")
                .getCapabilities()).isEmpty();
    }
}
