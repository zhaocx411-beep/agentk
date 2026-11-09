package com.kuaishou.agent.operation.backend.platform.agent100.dto;

import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.List;

import lombok.Data;

/**
 * Agent100 最近执行历史。
 */
@Data
public class Agent100HistoryView {

    private String agentId;
    private List<Item> items = new ArrayList<>();
    private int count;

    @Data
    public static class Item {
        private String userId;
        private String sessionId;
        private String executionId;
        private String pipeline;
        private String title;
        private String status;
        private String error;
        private boolean hasOutput;
        private LocalDateTime createdAt;
        private LocalDateTime updatedAt;
    }
}
