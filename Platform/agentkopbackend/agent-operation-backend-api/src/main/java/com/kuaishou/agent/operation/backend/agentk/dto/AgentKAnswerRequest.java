package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import lombok.Data;

/**
 * {@code POST /v1/sessions/{id}/answers} 请求体。
 */
@Data
public class AgentKAnswerRequest {

    private String toolCallId;
    private List<AnswerItem> answers;

    @Data
    public static class AnswerItem {
        private String questionId;
        private List<String> selected;
    }
}
