package com.kuaishou.agent.operation.backend.agentk.grpc;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.Arrays;
import java.util.Collections;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKAppendResultDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto;
import com.kuaishou.klingai.agentk.api.v1.Capability;
import com.kuaishou.klingai.agentk.api.v1.CapabilityTool;

class AgentKProtoMapperTest {

    @Test
    void toAppendResultIncludesExecutionIdAndStatus() {
        AgentKAppendResultDto dto = AgentKProtoMapper.toAppendResult(
                "ses_1", "exe_abc", "running", Collections.emptyList());

        assertThat(dto.getSessionId()).isEqualTo("ses_1");
        assertThat(dto.getExecutionId()).isEqualTo("exe_abc");
        assertThat(dto.getStatus()).isEqualTo("running");
        assertThat(dto.getEvents()).isEmpty();
    }

    @Test
    void toCapabilitiesDtoMarksCoreWorkflowEvenWhenToolDeclared() {
        Capability workflow = Capability.newBuilder()
                .setId("agentk.kling_text_to_video")
                .setDescription("可灵文生视频")
                .setTool(CapabilityTool.newBuilder().setDescription("可灵文生视频").build())
                .build();
        Capability activity = Capability.newBuilder()
                .setId("agentk.edit_image")
                .setDescription("参考图生图")
                .setTool(CapabilityTool.newBuilder().setDescription("参考图生图").build())
                .build();

        AgentKCapabilitiesDto dto = AgentKProtoMapper.toCapabilitiesDto(
                Arrays.asList(workflow, activity), new ObjectMapper());

        assertThat(dto.getCapabilities()).hasSize(2);
        assertThat(dto.getCapabilities().get(0).getKind()).isEqualTo("workflow");
        assertThat(dto.getCapabilities().get(0).getPipelineRef())
                .isEqualTo("pipeline:agentk.kling_text_to_video");
        assertThat(dto.getCapabilities().get(0).getToolRef())
                .isEqualTo("tool:agentk.kling_text_to_video");
        assertThat(dto.getCapabilities().get(1).getKind()).isEqualTo("activity");
        assertThat(dto.getCapabilities().get(1).getPipeline()).isNull();
    }
}
