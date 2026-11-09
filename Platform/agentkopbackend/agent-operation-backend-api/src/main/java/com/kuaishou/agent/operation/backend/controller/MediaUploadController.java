package com.kuaishou.agent.operation.backend.controller;

import java.util.List;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.boot.autoconfigure.condition.ConditionalOnBean;
import org.springframework.http.MediaType;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RequestPart;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.multipart.MultipartFile;

import com.kuaishou.agent.operation.backend.agentk.AgentKIdentityResolver;
import com.kuaishou.agent.operation.backend.agentk.AgentKOutboundIdentity;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.dto.MediaUploadView;
import com.kuaishou.agent.operation.backend.platform.service.MediaUploadService;

/**
 * 对话素材上传：图片 / 视频 / 音频 / 文档 → BlobStore → CDN URL，并落库绑定上传人。
 */
@RestController
@RequestMapping("/agent/operation/backend/platform/media")
@ConditionalOnBean(MediaUploadService.class)
public class MediaUploadController {

    private static final Logger LOGGER = LoggerFactory.getLogger(MediaUploadController.class);

    private final MediaUploadService mediaUploadService;
    private final AgentKIdentityResolver identityResolver;

    public MediaUploadController(MediaUploadService mediaUploadService,
            AgentKIdentityResolver identityResolver) {
        this.mediaUploadService = mediaUploadService;
        this.identityResolver = identityResolver;
    }

    /**
     * 上传图片、视频、音频或文档附件。
     *
     * <p>表单字段：
     * <ul>
     *   <li>{@code file} — 必填，multipart 文件</li>
     *   <li>{@code mediaType} — 可选；缺省按 Content-Type / 扩展名推断：
     *     {@code image}（png/jpg/webp/gif）、
     *     {@code video}（mp4/mov/webm/avi/mpeg）、
     *     {@code audio}（mp3/m4a/wav/aac/ogg/flac）、
     *     {@code doc}（pdf/doc/docx/xls/xlsx/ppt/pptx/txt/md/csv/rtf）</li>
     * </ul>
     */
    @PostMapping(value = "/upload", consumes = MediaType.MULTIPART_FORM_DATA_VALUE)
    public Message<MediaUploadView> upload(
            @RequestPart("file") MultipartFile file,
            @RequestParam(value = "mediaType", required = false) String mediaType) {
        long start = System.currentTimeMillis();
        AgentKOutboundIdentity identity = identityResolver.resolve();
        String userId = identity != null ? identity.getUserId() : "";
        String name = file != null ? file.getOriginalFilename() : null;
        long size = file != null ? file.getSize() : 0;
        LOGGER.info("媒体上传开始 userId={} filename={} size={} mediaType={}",
                userId, name, size, mediaType);
        try {
            MediaUploadView view = mediaUploadService.upload(file, mediaType, userId);
            LOGGER.info("媒体上传完成 id={} userId={} status={} mediaType={} blobKey={} costMs={}",
                    view.getId(), userId, view.getStatus(), view.getMediaType(), view.getBlobKey(),
                    System.currentTimeMillis() - start);
            return Message.ok(view);
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, e.getMessage());
        } catch (IllegalStateException e) {
            LOGGER.error("媒体上传失败 userId={} filename={}", userId, name, e);
            return Message.error(ResultCode.SERVICE_BUSY, e.getMessage());
        }
    }

    /**
     * 批量上传图片、视频、音频或文档附件。
     *
     * <p>表单字段：
     * <ul>
     *   <li>{@code files} — 必填，multipart 文件数组</li>
     *   <li>{@code mediaType} — 可选；不传时每个文件分别按 Content-Type / 扩展名推断</li>
     * </ul>
     */
    @PostMapping(value = "/upload/batch", consumes = MediaType.MULTIPART_FORM_DATA_VALUE)
    public Message<List<MediaUploadView>> uploadBatch(
            @RequestPart("files") MultipartFile[] files,
            @RequestParam(value = "mediaType", required = false) String mediaType) {
        long start = System.currentTimeMillis();
        AgentKOutboundIdentity identity = identityResolver.resolve();
        String userId = identity != null ? identity.getUserId() : "";
        int count = files == null ? 0 : files.length;
        LOGGER.info("批量媒体上传开始 userId={} count={} mediaType={}", userId, count, mediaType);
        try {
            List<MediaUploadView> views = mediaUploadService.uploadBatch(files, mediaType, userId);
            LOGGER.info("批量媒体上传完成 userId={} count={} costMs={}",
                    userId, views.size(), System.currentTimeMillis() - start);
            return Message.ok(views);
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, e.getMessage());
        } catch (IllegalStateException e) {
            LOGGER.error("批量媒体上传失败 userId={} count={}", userId, count, e);
            return Message.error(ResultCode.SERVICE_BUSY, e.getMessage());
        }
    }
}
