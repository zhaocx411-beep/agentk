package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;

import lombok.Data;

/**
 * 平台多媒体上传附件：绑定上传人与 Blob/CDN 地址，记录成功/失败。
 */
@Data
@TableName("aop_media_attachment")
public class MediaAttachment {

    public static final String STATUS_SUCCESS = "success";
    public static final String STATUS_FAILED = "failed";

    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    /** 上传人（AccessProxy username）。 */
    private String userId;
    /** image / video / audio / doc */
    private String mediaType;
    /** success / failed */
    private String status;
    /** 失败原因；成功为空。 */
    private String errorMessage;
    /** BlobStore 对象键；失败未写存储时可为空。 */
    private String blobKey;
    /** BlobStore 直链。 */
    private String blobUrl;
    /** CDN 签发 URL。 */
    private String cdnUrl;
    private LocalDateTime createdAt;
}
