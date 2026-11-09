package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;

import lombok.Data;

@Data
@TableName("aop_agent")
public class Agent {
    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    private String name;
    private String description;
    private String ownerUserId;
    private String tenantId;
    private Integer latestVersion;
    private String status;
    private Boolean isDefault;
    /** private 仅 owner；public 全员可读（列表暂未放开，仅标记）。 */
    private String visibility;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;
}
