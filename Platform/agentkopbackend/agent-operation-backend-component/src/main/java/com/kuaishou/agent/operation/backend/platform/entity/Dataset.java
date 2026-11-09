package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;

import lombok.Data;

@Data
@TableName("aop_dataset")
public class Dataset {
    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    private String name;
    private String description;
    private String ownerUserId;
    private String tenantId;
    private String keyColumn;
    private String inputMappingJson;
    private Integer latestSnapshotVersion;
    private String status;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;
}
