package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableField;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;

import lombok.Data;

@Data
@TableName("aop_session_message")
public class ChatMessage {
    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    private Long sessionId;
    @TableField("execution_id")
    private Long runId;
    private String agentkExecutionId;
    private String roundId;
    @TableField("event_seq")
    private Integer agentkItemSeq;
    private String role;
    private String contentJson;
    private String status;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;
}
