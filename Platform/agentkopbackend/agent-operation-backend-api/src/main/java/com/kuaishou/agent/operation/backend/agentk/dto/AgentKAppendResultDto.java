package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import lombok.Data;

/**
 * 追加型操作（发消息 / 作答）回执，与 agent-k {@code AppendResult} 对齐。
 */
@Data
public class AgentKAppendResultDto {

    private String sessionId;
    /** agent-k execution id；订流 {@code GET /stream?executionId=} 必传。 */
    private String executionId;
    /** running / completed / failed / cancelled */
    private String status;
    private List<AgentKListEventsResponse.AgentKEventDto> events;
}
