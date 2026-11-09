package com.kuaishou.agent.operation.backend.platform.agent100;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.platform.agent100.narrato.NarratoDefinition;

class Agent100RegistryTest {

    @Test
    void catalogContainsNarratoOnly() {
        assertThat(Agent100Registry.all()).hasSize(1);
        assertThat(Agent100Registry.knownIds()).containsExactly(NarratoDefinition.AGENT_ID);

        Agent100Definition narrato = Agent100Registry.require(NarratoDefinition.AGENT_ID);
        assertThat(narrato.getName()).isEqualTo("Narrato");
        assertThat(narrato.getDefaultModel()).isEqualTo(NarratoDefinition.DEFAULT_MODEL);
        assertThat(narrato.pipelineNames()).containsExactly(
                NarratoDefinition.PIPELINE_VIDEO,
                NarratoDefinition.PIPELINE_SCRIPT,
                NarratoDefinition.PIPELINE_RENDER);
        assertThat(narrato.capabilityIds()).containsExactly(
                NarratoDefinition.CAPABILITY_CREATE,
                NarratoDefinition.CAPABILITY_GENERATE,
                NarratoDefinition.CAPABILITY_RENDER);
    }

    @Test
    void findPipelineByName() {
        Agent100Definition narrato = Agent100Registry.require("narrato");
        Agent100PipelineDef video = narrato.findPipeline("documentary_video");
        assertThat(video).isNotNull();
        assertThat(video.getDisplayName()).isEqualTo("一键成片");
        assertThat(video.getCapability()).isEqualTo(NarratoDefinition.CAPABILITY_CREATE);
        assertThat(video.getTemporalType()).isEqualTo(NarratoDefinition.CREATE_WORKFLOW_TYPE);
        assertThat(video.getContractVersion()).isEqualTo(NarratoDefinition.CONTRACT_VERSION);
        assertThat(narrato.findPipeline("unknown")).isNull();
    }

    @Test
    void requireUnknownAgentThrows() {
        assertThatThrownBy(() -> Agent100Registry.require("missing"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("unknown Agent100 id");
    }
}
