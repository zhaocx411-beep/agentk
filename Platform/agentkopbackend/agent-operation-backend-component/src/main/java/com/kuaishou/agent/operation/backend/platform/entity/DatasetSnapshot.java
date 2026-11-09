package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;

import lombok.Data;

@Data
@TableName("aop_dataset_snapshot")
public class DatasetSnapshot {
    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    private Long datasetId;
    private Integer version;
    private String columnsJson;
    private Integer rowCount;
    private String schemaJson;
    private String checksumSha256;
    private String sourceUri;
    private String snapshotUri;
    private String previewJson;
    private String createdBy;
    private LocalDateTime createdAt;
}
