package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableField;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;

import lombok.Data;

@Data
@TableName("aop_session")
public class ChatSession {
    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    private String publicId;
    private String userId;
    /** 新表无此列；保留字段避免旧调用方编译失败。 */
    @TableField(exist = false)
    private String tenantId;
    private String agentkSessionId;
    private String title;
    private Long agentVersionId;
    private Long workflowVersionId;
    /**
     * 业务入口：{@code agent_loop} / {@code workflow} / {@code custom}。
     * 个人历史按此区分，与 config_source（内存字段，库表已删）无关。
     */
    private String sessionKind;
    /**
     * 会话入口，只区分在线 / 离线，与 {@code sessionKind} 正交。
     * 空或不传按 {@code online}；{@code offline_batch} 是离线跑数。
     * 官方快速入口也是 {@code online}，不是第三种值。历史行可能为空，读时同样当 online。
     */
    private String entrySource;
    @TableField(exist = false)
    private String specJson;
    /**
     * 建会话 profile 快照 JSON（库列 {@code profile_json}）。
     * API 读写均按 JSON object。
     */
    @com.fasterxml.jackson.annotation.JsonRawValue
    @com.fasterxml.jackson.databind.annotation.JsonDeserialize(
            using = com.kuaishou.agent.operation.backend.platform.support.JsonNodeToStringDeserializer.class)
    @TableField("profile_json")
    private String profileYaml;
    private String profileVersion;
    private String effectiveModel;
    private String effectiveToolsJson;
    private String effectiveToolModelsJson;
    /** 库表已删；调用方仍可读写，不落库。最新 execution 请查 aop_execution。 */
    @TableField(exist = false)
    private String configSource;
    @TableField(exist = false)
    private String rejectReason;
    /** 反引号：旧版 ShardingSphere 解析裸 status 会 NPE */
    @TableField("`status`")
    private String status;
    @TableField(exist = false)
    private String lastAgentkRunId;
    /**
     * 会话最近一次 agent-k executionId（exe_xxx），列表接口回填；不落库。
     * 与 {@link #lastAgentkRunId} 同值，便于前端按 executionId 语义取值。
     */
    @TableField(exist = false)
    private String lastExecutionId;
    @TableField(exist = false)
    private Integer lastEventSeq;
    @TableField(exist = false)
    private String activeRunStatus;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;
}
