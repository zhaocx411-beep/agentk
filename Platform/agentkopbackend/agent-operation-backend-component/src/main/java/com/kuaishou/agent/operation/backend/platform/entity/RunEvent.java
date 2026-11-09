package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableField;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;
import com.kuaishou.agent.operation.backend.platform.support.ChatEntrySources;

import lombok.Data;

@Data
@TableName("aop_session_event")
public class RunEvent {
    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    private Long sessionId;
    @TableField("execution_id")
    private Long runId;
    @TableField("agentk_execution_id")
    private String agentkRunId;
    private String agentkSessionId;
    private String agentkEventId;
    private String roundId;
    private Integer seq;
    private String kind;
    private String payloadJson;
    private LocalDateTime createdAt;
    private String agentkCreatedAt;
    /**
     * 冗余 {@code aop_chat_session.entry_source}，见 {@link ChatEntrySources}。
     * <p>
     * 默认值不能省：列是 {@code NOT NULL}，而 {@code RunEventMapper.insertEvent} 走显式列插入，
     * 不设值就会写入 NULL 被 MySQL 严格模式打回。
     */
    private String entrySource = ChatEntrySources.ONLINE;
}
