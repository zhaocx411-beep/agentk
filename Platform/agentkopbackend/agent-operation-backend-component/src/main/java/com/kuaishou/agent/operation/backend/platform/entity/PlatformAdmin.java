package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;

import lombok.Data;

/**
 * 平台管理员（DB 授权，可热更新，无需改配置发版）。
 */
@Data
@TableName("aop_platform_admin")
public class PlatformAdmin {
    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    /** AccessProxy username */
    private String userId;
    /** 角色：chat_reviewer（管理回看）/ ADMIN（全量菜单）/ SUPER_ADMIN（全量菜单 + 个人历史看他人 + 管理回看） */
    private String role;
    /** active / disabled */
    private String status;
    private String createdBy;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;
}
