package com.kuaishou.agent.operation.backend.batcheval.dto;

import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import com.kuaishou.agent.operation.backend.batcheval.support.OfflineJobSessionMode;

import lombok.Data;

/**
 * 批量评测 API DTO 集合，避免 P0 阶段产生大量仅含字段的文件。
 */
public final class BatchEvalDtos {

    private BatchEvalDtos() {
    }

    @Data
    public static class DatasetSummaryView {
        private long id;
        private String name;
        private String version;
        private String datasetType;
        /** EVAL=离线跑数，TEMPLATE=pipeline 入口模版 */
        private String usage;
        /** 模版归属 pipeline，仅 TEMPLATE 有值 */
        private String pipelineName;
        private String description;
        private String creator;
        private int total;
        private long jobCount;
        private boolean referencedByJobs;
        private LocalDateTime createdAt;
        private LocalDateTime updatedAt;
        /** xlsx 上传时的列映射结果：规范列名 → 实际表头。 */
        private Map<String, String> columnMapping;
        /** xlsx 上传时前 3 行解析预览。 */
        private List<Map<String, Object>> parsePreview;
    }

    @Data
    public static class DatasetExportResult {
        private byte[] body;
        /** 实际导出格式：xlsx / json */
        private String format;
        private String filename;
        private String contentType;
    }

    @Data
    public static class DatasetTaskView {
        private long id;
        /** 所属数据集 ID；跨数据集筛选时便于前端回溯 */
        private Long datasetId;
        private String taskKey;
        private String prompt;
        private String linkUrl;
        private List<String> referImages = new ArrayList<>();
        private Map<String, Object> extraFields = new LinkedHashMap<>();
        /** 用例类目；未标注为 null。素材常见 code 见 {@code DatasetTaskCategories}，也允许自定义。 */
        private String category;
        /** 多轮会话分组键；空表示单轮。 */
        private String conversationKey;
        /** 同组内轮次，从 1 起。 */
        private Integer turnIndex;
        private LocalDateTime createdAt;
        private LocalDateTime updatedAt;
    }

    @Data
    public static class PageQueryRequest {
        private int current = 1;
        private int pageSize = 10;
    }

    @Data
    public static class DatasetIdRequest {
        private long datasetId;
    }

    @Data
    public static class BizIdRequest {
        private String bizId;
    }

    @Data
    public static class CellTraceQueryRequest {
        private String bizId;
        private String taskKey;
    }

    @Data
    public static class SubmitOfflineJobRequest {
        private long datasetId;
        /** 绑定的 aop_agent.id（官方 is_default 或当前用户个人 agent） */
        private long agentId;
        /**
         * 会话策略，见 {@link com.kuaishou.agent.operation.backend.batcheval.support.OfflineJobSessionMode}。
         * 未传时看 {@link #multiRoundConversation}。
         */
        private String sessionMode;
        /**
         * 兼容旧前端：true=SINGLE_SESSION，false/缺省=PER_TASK。
         * 与 {@link #sessionMode} 同时传时以 sessionMode 为准。
         */
        private Boolean multiRoundConversation;
    }

    @Data
    public static class OfflineJobView {
        private String bizId;
        private long datasetId;
        private Long agentId;
        private Long agentVersionId;
        private String agentName;
        /** 快照派生：profile.defaultModel */
        private String model;
        private String systemPrompt;
        private List<String> tools = new ArrayList<>();
        private String pipeline;
        /** {@link com.kuaishou.agent.operation.backend.batcheval.support.OfflineJobSessionMode} 的 code */
        private String sessionMode;
        private String status;
        private int total;
        private int errors;
        private String creator;
        private LocalDateTime createdAt;
        private LocalDateTime startedAt;
        private LocalDateTime finishedAt;
    }

    @Data
    public static class OfflineJobCellView {
        private String taskKey;
        private String prompt;
        private String conversationKey;
        private Integer turnIndex;
        private String status;
        private String agentkSessionId;
        private String agentkRunId;
        private String pipelineExecId;
        private String metricsJson;
        /** 本 cell 产出的媒体 URL，前端可直接渲染 */
        private List<String> outputUris = new ArrayList<>();
        private String error;
        private LocalDateTime startedAt;
        private LocalDateTime finishedAt;
    }

    /**
     * 离线跑数页配置：可选 agent（官方 + 当前用户个人，且须有已发布版本），
     * 以及提交时的 {@code sessionMode} 枚举。
     */
    @Data
    public static class AgentConfigView {
        private List<OfflineAgentOption> agents = new ArrayList<>();
        /** 提交 {@code sessionMode} 的可选值；未传时用 {@link #defaultSessionMode}。 */
        private List<OfflineJobSessionMode.View> sessionModes = new ArrayList<>();
        /** 未传 sessionMode / multiRoundConversation 时的默认策略。 */
        private String defaultSessionMode;
    }

    @Data
    public static class OfflineAgentOption {
        private long agentId;
        private String name;
        /** official / mine */
        private String source;
        /** 当前将用于跑数的已发布版本主键 */
        private long agentVersionId;
    }

    @Data
    public static class AgentPipelineView {
        /** 会话内 canonical name，如 generate_brand_ad */
        private String name;
        /** Profile 引用，如 pipeline:agentk.generate_brand_ad */
        private String ref;
        private String description;
        private String capabilityId;
        /** agentk / mock */
        private String source;
        /** 入参 JSON Schema，前端据此渲染表单并做必填校验 */
        private Map<String, Object> inputSchema = new LinkedHashMap<>();
    }

    @Data
    public static class AgentToolView {
        private String name;
        /** 动态能力工具的 Profile 引用，如 tool:contrib.x；第一方工具为 null */
        private String ref;
        private String description;
        private String mode;
        private String capabilityId;
        /** agentk / mock */
        private String source;
    }

    @Data
    public static class DatasetUploadRequest {
        private byte[] content;
        private String filename;
        private String description;
        private String datasetType;
        private String creator;
        /** 必须；写入数据集名称，不用文件名兜底 */
        private String name;
        /** 可选；xlsx 缺省 v1；JSON 非空时覆盖文件内 version */
        private String version;
        /** xlsx 无 prompt 列时必填（商品表兜底拼 prompt）；有 prompt 列或 JSON 可省略 */
        private String taskInstruction;
        /** 必填；EVAL(离线跑数) / TEMPLATE(素材与模版) */
        private String usage;
        /** TEMPLATE 必须；素材/模版归属的 pipeline 名 */
        private String pipelineName;
    }

    @Data
    public static class UpdateDatasetRequest {
        private String description;
        private String datasetType;
    }

    @Data
    public static class CreateDatasetTaskRequest {
        private String taskKey;
        private String prompt;
        private String linkUrl;
        private List<String> referImages = new ArrayList<>();
        private Map<String, Object> extraFields = new LinkedHashMap<>();
        /** 可选；空则未标注。已知中文展示名会映射到素材 code，其余原样落库。 */
        private String category;
        private String conversationKey;
        private Integer turnIndex;
    }

    @Data
    public static class UpdateDatasetTaskRequest {
        private String prompt;
        private String linkUrl;
        private List<String> referImages;
        private Map<String, Object> extraFields;
        /** 非 null 时更新；空字符串清除类目。 */
        private String category;
        /** 非 null 时更新；空字符串清除会话键。 */
        private String conversationKey;
        /** 非 null 时更新。 */
        private Integer turnIndex;
    }
}
