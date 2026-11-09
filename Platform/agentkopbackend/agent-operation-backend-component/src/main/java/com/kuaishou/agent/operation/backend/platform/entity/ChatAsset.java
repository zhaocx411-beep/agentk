package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableField;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;

import lombok.Data;

@Data
@TableName("aop_asset")
public class ChatAsset {
    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    private String publicId;
    private Long sessionId;
    @TableField("execution_id")
    private Long runId;
    @TableField("session_message_id")
    private Long messageId;
    private String userId;
    @TableField(exist = false)
    private String tenantId;
    /** image / video */
    private String assetType;
    private String assetUrl;
    /** 资产归属的用户提示词，供作品集卡片展示和检索。 */
    private String promptText;
    /** agent_loop / pipeline / capability / api */
    private String sourceKind;
    /** online=在线问答 offline=离线批量跑数 */
    private String source;
    private String sourceName;
    /**
     * 会话绑定的智能体名（冗余，仅 aop_agent.name）。
     * Workflow 名不写入本列；历史行可能为空或曾混入 workflow 名，列表展示以 session 联查为准。
     */
    private String agentName;
    @TableField("event_seq")
    private Integer agentkEventSeq;
    private String roundId;
    private String agentkSessionId;
    @TableField("agentk_execution_id")
    private String agentkRunId;
    @TableField("agentk_pipeline_exec_id")
    private String workflowExecId;
    private String status;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;
}
