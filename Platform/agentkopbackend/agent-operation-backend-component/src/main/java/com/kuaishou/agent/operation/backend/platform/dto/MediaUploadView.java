package com.kuaishou.agent.operation.backend.platform.dto;

import lombok.Builder;
import lombok.Data;

/**
 * 媒体上传结果：落库 id + BlobStore 键 + 直链 + CDN URL。
 */
@Data
@Builder
public class MediaUploadView {

    /** aop_media_attachment.id；落库失败时可为空。 */
    private Long id;
    /** 上传人。 */
    private String userId;
    /** success / failed */
    private String status;
    /** 失败原因；成功为空。 */
    private String errorMessage;
    /** BlobStore 对象键（同 blobKey）。 */
    private String fileId;
    /** BlobStore 对象键。 */
    private String blobKey;
    /** image / video / audio / doc */
    private String mediaType;
    /** BlobStore 直链。 */
    private String blobUrl;
    /** BlobStore 直链（兼容旧字段 fileUrl）。 */
    private String fileUrl;
    /** CDN 调度签发后的公网可访问 URL。 */
    private String cdnUrl;
    private String contentType;
    private long size;
    private String originalFilename;
}
