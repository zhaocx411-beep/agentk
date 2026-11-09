package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import com.fasterxml.jackson.annotation.JsonAlias;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * Pipeline 执行快照，与 agent-k {@code pipelineExecDTO} 对齐。
 */
@Data
public class AgentKWorkflowExecDto {

    private String id;
    private String sessionId;
    private String name;
    /** Lab：agent-k capability id，如 {@code agentk.edit_image}。 */
    private String capabilityId;
    private String status;
    @JsonProperty("args")
    @JsonAlias("inputs")
    private JsonNode args;
    private JsonNode output;
    /** 从 output 中单独投影出的展示用图片/视频产物。 */
    private List<AgentKMediaArtifactDto> mediaArtifacts;
    private List<String> imageUrls;
    private List<String> videoUrls;
    private String profileVersion;
    private String error;
    private String createdAt;
    private String finishedAt;
}
