package com.kuaishou.agent.operation.backend.platform.dto;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * 平台维护的 Session Profile 模板（只读下发；建会话使用后才会写入 aop_chat_session）。
 */
@Data
public class SessionProfileTemplateView {
    /** 如 pipeline_only_default / agent_loop_official_default */
    private String templateKey;
    private String profileVersion;
    private String description;
    /**
     * 说明：模板接口本身不落库；若用于 POST /agentk/sessions 建会话，
     * 组装后的 profile 会写入 aop_chat_session.profile_yaml。
     */
    private Boolean persistedOnCreateSession;
    private JsonNode profile;
}
