package com.kuaishou.agent.operation.backend.agentk.dto;

import lombok.Data;

/**
 * Capability Lab 单次执行结果：临时 session + pipeline execution。
 */
@Data
public class CapabilityRunResponse {

    /** agent-k session id，形如 {@code ses_xxx}，不是平台表主键。 */
    private String sessionId;
    /** agent-k execution id。 */
    private String executionId;
    private String capabilityId;
    private String pipeline;
    private String status;
    private AgentKWorkflowExecDto execution;
}
