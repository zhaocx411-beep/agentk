package com.kuaishou.agent.operation.backend.platform.model;

import java.time.LocalDateTime;

import lombok.Data;

/**
 * 平台持久化模型（表结构见 docs/sql/aop_platform_v1.sql）。
 * 不含离线评测 EvalRun/Case。
 */
public final class PlatformModels {

    private PlatformModels() {
    }

    @Data
    public static class AgentkCallLog {
        private long id;
        private String bizType;
        private long bizId;
        private String userId;
        private String tenantId;
        private String api;
        private String requestJson;
        private String status;
        private Integer httpStatus;
        private String error;
        private String agentkSessionId;
        private String agentkRunId;
        private String agentkJobId;
        private String agentkWorkflowExecId;
        private LocalDateTime createdAt;
        private LocalDateTime finishedAt;
    }

    @Data
    public static class Agent {
        private long id;
        private String name;
        private String description;
        private String ownerUserId;
        private String tenantId;
        private int latestVersion;
        private String status;
        /** 是否默认 Agent（v2） */
        private Boolean isDefault;
        /** private / public（官方示范标记；列表暂未放开） */
        private String visibility;
        private LocalDateTime createdAt;
        private LocalDateTime updatedAt;
    }

    @Data
    public static class AgentVersion {
        private long id;
        private long agentId;
        private int version;
        private String profileYaml;
        private String contentHash;
        private String status;
        /** 创建人 */
        private String createdBy;
        private LocalDateTime createdAt;
        private LocalDateTime publishedAt;
        /** 发布人（v2） */
        private String publishedBy;
    }

    @Data
    public static class Workflow {
        private long id;
        private String name;
        private String description;
        private String ownerUserId;
        private String tenantId;
        private int latestVersion;
        private String status;
        private Boolean isDefault;
        private String visibility;
        private LocalDateTime createdAt;
        private LocalDateTime updatedAt;
    }

    @Data
    public static class WorkflowVersion {
        private long id;
        private long workflowId;
        private int version;
        private String definitionJson;
        private String contentHash;
        private String status;
        /** 创建人 */
        private String createdBy;
        private LocalDateTime createdAt;
        private LocalDateTime publishedAt;
        /** 发布人（v2） */
        private String publishedBy;
    }

    @Data
    public static class ChatSession {
        private long id;
        private String publicId;
        private String userId;
        private String tenantId;
        private String agentkSessionId;
        private String title;
        private Long agentVersionId;
        private Long workflowVersionId;
        private String sessionKind;
        private String entrySource;
        private String specJson;
        private String profileYaml;
        private String profileVersion;
        private String effectiveModel;
        private String effectiveToolsJson;
        private String effectiveToolModelsJson;
        private String configSource;
        private String rejectReason;
        private String status;
        private String lastAgentkRunId;
        private int lastEventSeq;
        private String activeRunStatus;
        private LocalDateTime createdAt;
        private LocalDateTime updatedAt;
    }

    @Data
    public static class ChatRun {
        private long id;
        private long sessionId;
        private String agentkSessionId;
        private String agentkRunId;
        private String triggerKind;
        private String status;
        private String parentAgentkRunId;
        private int lastEventSeq;
        private String lastRoundId;
        private String metricsJson;
        private String error;
        private String model;
        private String dialect;
        private String profileVersion;
        /** 本轮 system prompt 指纹（round.started.promptSha256，12 位 hex）。 */
        private String promptFingerprint;
        private String toolsJson;
        private String toolModelsJson;
        private String workerId;
        private Boolean configMismatch;
        private LocalDateTime createdAt;
        private LocalDateTime startedAt;
        private LocalDateTime finishedAt;
    }

    @Data
    public static class RunEvent {
        private long id;
        private long sessionId;
        private long runId;
        private String agentkRunId;
        private String agentkSessionId;
        private String agentkEventId;
        private String roundId;
        private int seq;
        private String kind;
        private String payloadJson;
        private LocalDateTime createdAt;
        private String agentkCreatedAt;
    }

    @Data
    public static class ChatMessage {
        private long id;
        private long sessionId;
        private Long runId;
        private String agentkExecutionId;
        private String roundId;
        private Integer agentkItemSeq;
        private String role;
        private String contentJson;
        private String status;
        private LocalDateTime createdAt;
        private LocalDateTime updatedAt;
    }

    @Data
    public static class ChatJob {
        private long id;
        private long sessionId;
        private Long runId;
        private String agentkJobId;
        private String toolCallId;
        private String kind;
        private String status;
        private String wakeRunId;
        private LocalDateTime createdAt;
        private LocalDateTime updatedAt;
    }

    @Data
    public static class Dataset {
        private long id;
        private String name;
        private String description;
        private String ownerUserId;
        private String tenantId;
        private String keyColumn;
        private String inputMappingJson;
        private int latestSnapshotVersion;
        private String status;
        private LocalDateTime createdAt;
        private LocalDateTime updatedAt;
    }

    @Data
    public static class DatasetSnapshot {
        private long id;
        private long datasetId;
        private int version;
        private String columnsJson;
        private int rowCount;
        private String schemaJson;
        private String checksumSha256;
        private String sourceUri;
        private String snapshotUri;
        private String previewJson;
        private String createdBy;
        private LocalDateTime createdAt;
    }
}
