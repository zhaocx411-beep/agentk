package com.kuaishou.agent.operation.backend.platform.dto;

import java.time.LocalDateTime;

import lombok.Builder;
import lombok.Data;

/**
 * 上传历史单条（对应 aop_media_attachment）。
 */
@Data
@Builder
public class MediaAttachmentHistoryView {

    private Long id;
    private String userId;
    /** image / video / audio / doc */
    private String mediaType;
    /** success / failed */
    private String status;
    private String errorMessage;
    private String blobKey;
    private String blobUrl;
    private String cdnUrl;
    private LocalDateTime createdAt;
}
