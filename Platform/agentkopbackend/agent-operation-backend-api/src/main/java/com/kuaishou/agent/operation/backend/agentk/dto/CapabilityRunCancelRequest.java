package com.kuaishou.agent.operation.backend.agentk.dto;

import lombok.Data;

/** 取消 capability 执行：body.sessionId 为 agent-k {@code ses_*}，不是平台主键。 */
@Data
public class CapabilityRunCancelRequest {

    private String sessionId;
}
