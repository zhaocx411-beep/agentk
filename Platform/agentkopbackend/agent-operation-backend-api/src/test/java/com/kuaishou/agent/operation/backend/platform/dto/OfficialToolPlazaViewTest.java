package com.kuaishou.agent.operation.backend.platform.dto;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.Collections;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityViewDto;

class OfficialToolPlazaViewTest {

    @Test
    void emptyPlazaHasZeroTools() {
        OfficialToolPlazaView view = OfficialToolPlazaView.empty();
        assertThat(view.getSource()).isEqualTo("empty");
        assertThat(view.getTotal()).isZero();
        assertThat(view.getTools()).isEmpty();
    }

    @Test
    void fromCapabilitiesShouldUseBareCapabilityId() {
        AgentKCapabilityViewDto cap = new AgentKCapabilityViewDto();
        cap.setKind("activity");
        cap.setName("generate_speech_qwen");
        cap.setId("agentk.generate_speech_qwen");
        cap.setToolRef("tool:agentk.generate_speech_qwen");
        cap.setDescription("通义语音");

        OfficialToolPlazaView view = OfficialToolPlazaView.fromActivityCapabilities(
                Collections.singletonList(cap));
        assertThat(view.getTools()).hasSize(1);
        assertThat(view.getTools().get(0).getName()).isEqualTo("generate_speech_qwen");
        assertThat(view.getTools().get(0).getKind()).isEqualTo("activity");
        assertThat(view.getTools().get(0).getCapability()).isEqualTo("agentk.generate_speech_qwen");
    }

    @Test
    void fromCapabilitiesShouldKeepWorkflowsWithKind() {
        AgentKCapabilityViewDto workflow = new AgentKCapabilityViewDto();
        workflow.setKind("workflow");
        workflow.setName("generate_brand_ad");
        workflow.setId("agentk.generate_brand_ad");

        OfficialToolPlazaView view = OfficialToolPlazaView.fromActivityCapabilities(
                Collections.singletonList(workflow));
        assertThat(view.getTools()).hasSize(1);
        assertThat(view.getTools().get(0).getKind()).isEqualTo("workflow");
        assertThat(view.getTools().get(0).getCapability()).isEqualTo("agentk.generate_brand_ad");
    }

    @Test
    void fromCapabilitiesShouldFallbackToCapabilityIdWhenToolRefMissing() {
        AgentKCapabilityViewDto cap = new AgentKCapabilityViewDto();
        cap.setKind("activity");
        cap.setName("generate_image");
        cap.setId("agentk.generate_image");

        OfficialToolPlazaView view = OfficialToolPlazaView.fromActivityCapabilities(
                Collections.singletonList(cap));
        assertThat(view.getTools().get(0).getCapability()).isEqualTo("agentk.generate_image");
    }
}
