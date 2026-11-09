package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;

import lombok.Data;

@Data
@TableName("aop_chat_job")
public class ChatJob {
    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    private Long sessionId;
    private Long runId;
    private String agentkJobId;
    private String toolCallId;
    private String kind;
    private String status;
    private String wakeRunId;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;
}
