package com.kuaishou.agent.operation.backend.batcheval.model;

import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import com.fasterxml.jackson.annotation.JsonIgnore;

import lombok.Data;

/**
 * 批量评测内部存储模型。P0 由内存 Mapper 承载，后续替换为 MySQL Mapper 时
 * Service 和 Controller 无需感知存储实现变化。
 */
public final class BatchEvalModels {

    private BatchEvalModels() {
    }

    @Data
    public static class Dataset {
        private long id;
        private String name;
        private String version;
        private String datasetType;
        /** 用途：EVAL=离线跑数，TEMPLATE=pipeline 入口模版；持久化为 usage_type。 */
        private String usage;
        /** 模版归属的 pipeline 名，仅 TEMPLATE 用于筛选。 */
        private String pipelineName;
        private String description;
        private String creator;
        private int total;
        private LocalDateTime createdAt;
        private LocalDateTime updatedAt;
        private List<DatasetTask> tasks = new ArrayList<>();
    }

    @Data
    public static class DatasetTask {
        private long id;
        /** 所属数据集；列表按 usage/pipeline 跨集查询时回填 */
        private Long datasetId;
        private String taskKey;
        private String prompt;
        private String linkUrl;
        /** 参考图 URL 列表；持久化为 refer_images JSON。 */
        private List<String> referImages = new ArrayList<>();
        /** 结构化商品字段；持久化为 extra_fields JSON。 */
        private Map<String, Object> extraFields = new LinkedHashMap<>();
        /** 用例类目；空表示未标注。跑数 / 素材用数据集 usage 区分。 */
        private String category;
        /**
         * 多轮会话分组键；空表示单轮。提交时按 sessionMode 决定是否使用。
         */
        private String conversationKey;
        /** 同组内轮次，从 1 起；缺省时上传/落库前按出现顺序补齐。 */
        private Integer turnIndex;
        /** MyBatis 映射 refer_images 用。 */
        @JsonIgnore
        private String referImagesJson;
        /** MyBatis 映射 extra_fields 用。 */
        @JsonIgnore
        private String extraFieldsJson;
        private LocalDateTime createdAt;
        private LocalDateTime updatedAt;
    }

    @Data
    public static class OfflineJob {
        private long id;
        private String bizId;
        private long datasetId;
        /** 绑定的 aop_agent.id */
        private Long agentId;
        /** 提交时解析到的已发布 aop_agent_version.id */
        private Long agentVersionId;
        /** 提交时 agent 名称快照（展示用） */
        private String agentName;
        private String model;
        private String systemPrompt;
        private List<String> tools = new ArrayList<>();
        /** MyBatis 映射 tools_json 用，不参与 JSON 序列化 */
        @JsonIgnore
        private String toolsJson;
        /** 本次跑数使用的 pipeline 会话内名称；为空表示只跑 Agent Loop。 */
        private String pipelineName;
        /** pipeline 的 Catalog ref，如 pipeline:agentk.generate_brand_ad。 */
        private String pipelineRef;
        private String profileVersion;
        private String profileYaml;
        /**
         * 提交时会话策略快照，见 {@link com.kuaishou.agent.operation.backend.batcheval.support.OfflineJobSessionMode}。
         */
        private String sessionMode;
        private String status;
        private int total;
        private int errors;
        private String creator;
        private LocalDateTime createdAt;
        private LocalDateTime startedAt;
        private LocalDateTime finishedAt;
        private List<OfflineJobCell> cells = new ArrayList<>();
    }

    @Data
    public static class OfflineJobCell {
        private long id;
        /** 用例标识，对应 YAML tasks[].id / dataset_task.task_key */
        private String taskKey;
        /** 用户输入（提交时从 dataset_task 复制） */
        private String prompt;
        /** 提交时从 dataset_task 快照的会话分组键。 */
        private String conversationKey;
        /** 提交时从 dataset_task 快照的组内轮次。 */
        private Integer turnIndex;
        private String status;
        private String agentkSessionId;
        private String agentkRunId;
        /** agent-k pipeline run id（pex_xxx）；只跑 Loop 时为空。 */
        private String pipelineExecId;
        private String metricsJson;
        /** 本 cell 产出的 image/video URL 数组 JSON。 */
        private String outputUris;
        private String traceJson;
        private String error;
        private LocalDateTime startedAt;
        private LocalDateTime finishedAt;
    }
}
