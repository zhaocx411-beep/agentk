package com.kuaishou.agent.operation.backend.agentk.dto;

import com.fasterxml.jackson.annotation.JsonAlias;
import com.fasterxml.jackson.annotation.JsonProperty;

import lombok.Data;

/**
 * 对齐 agent-k {@code MediaAttachment}：本轮用户媒体。
 */
@Data
public class AgentKMediaAttachment {

    /** image / video / audio */
    private String type;

    private String uri;

    @JsonProperty("mimeType")
    @JsonAlias("mime_type")
    private String mimeType;
}
