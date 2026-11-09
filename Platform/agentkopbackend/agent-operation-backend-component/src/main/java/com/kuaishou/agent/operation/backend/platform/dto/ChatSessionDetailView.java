package com.kuaishou.agent.operation.backend.platform.dto;

import java.time.LocalDateTime;

import lombok.Data;

/**
 * 平台会话详情：用户归属 + AgentLoop/Workflow 版本绑定 + 生效配置快照。
 */
@Data
public class ChatSessionDetailView {
    private Long id;
    private String publicId;
    private String userId;
    private String tenantId;
    private String agentkSessionId;
    private String title;
    private String status;
    private String lastAgentkRunId;
    /** 与 lastAgentkRunId 同义：最近一次 agent-k executionId（exe_xxx）。 */
    private String lastExecutionId;
    private Integer lastEventSeq;
    private String activeRunStatus;
    private String profileVersion;
    private String effectiveModel;
    private String effectiveToolsJson;
    private String effectiveToolModelsJson;
    private String configSource;
    private String rejectReason;
    /** agent_loop / workflow / custom */
    private String sessionKind;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;

    /** 绑定的 AgentLoop 版本摘要（可空）。 */
    private VersionBindingView agentLoopVersion;
    /** 绑定的 Workflow 版本摘要（可空）。 */
    private VersionBindingView workflowVersion;

    @Data
    public static class VersionBindingView {
        private Long versionId;
        private Long parentId;
        private Integer version;
        private String status;
        private String profileVersionLabel;
        private String name;
        private String contentHash;
        private LocalDateTime publishedAt;
        private String publishedBy;
    }
}
