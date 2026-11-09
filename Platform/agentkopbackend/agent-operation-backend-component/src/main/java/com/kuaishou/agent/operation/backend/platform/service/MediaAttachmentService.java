package com.kuaishou.agent.operation.backend.platform.service;

import java.util.List;
import java.util.Locale;
import java.util.stream.Collectors;

import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.extension.plugins.pagination.Page;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.MediaAttachmentMapper;
import com.kuaishou.agent.operation.backend.platform.dto.MediaAttachmentHistoryQuery;
import com.kuaishou.agent.operation.backend.platform.dto.MediaAttachmentHistoryView;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;
import com.kuaishou.agent.operation.backend.platform.entity.MediaAttachment;

/**
 * aop_media_attachment 查询（不依赖 BlobStore）。
 */
@Service
public class MediaAttachmentService {

    private final MediaAttachmentMapper mediaAttachmentMapper;

    public MediaAttachmentService(MediaAttachmentMapper mediaAttachmentMapper) {
        this.mediaAttachmentMapper = mediaAttachmentMapper;
    }

    /**
     * 当前用户上传历史：按创建时间倒序。
     */
    public PageResult<MediaAttachmentHistoryView> pageMine(String userId, MediaAttachmentHistoryQuery query) {
        if (!StringUtils.hasText(userId)) {
            throw new IllegalArgumentException("未登录或无法解析上传人");
        }
        MediaAttachmentHistoryQuery q = query == null ? new MediaAttachmentHistoryQuery() : query;
        long pageNo = q.getPage() == null || q.getPage() <= 0 ? 1L : q.getPage().longValue();
        long pageSize = q.getPageSize() == null || q.getPageSize() <= 0
                ? 20L : Math.min(q.getPageSize().longValue(), 100L);

        String mediaType = normalizeOptional(q.getMediaType());
        if (StringUtils.hasText(mediaType)
                && !"image".equals(mediaType) && !"video".equals(mediaType)
                && !"audio".equals(mediaType) && !"doc".equals(mediaType)) {
            throw new IllegalArgumentException("mediaType 仅支持 image / video / audio / doc");
        }
        String status = normalizeOptional(q.getStatus());
        if (StringUtils.hasText(status)
                && !MediaAttachment.STATUS_SUCCESS.equals(status)
                && !MediaAttachment.STATUS_FAILED.equals(status)) {
            throw new IllegalArgumentException("status 仅支持 success / failed");
        }

        LambdaQueryWrapper<MediaAttachment> wrapper = new LambdaQueryWrapper<MediaAttachment>()
                .eq(MediaAttachment::getUserId, userId.trim())
                .eq(StringUtils.hasText(mediaType), MediaAttachment::getMediaType, mediaType)
                .eq(StringUtils.hasText(status), MediaAttachment::getStatus, status)
                .orderByDesc(MediaAttachment::getCreatedAt)
                .orderByDesc(MediaAttachment::getId);

        Page<MediaAttachment> page = mediaAttachmentMapper.selectPage(new Page<>(pageNo, pageSize), wrapper);
        List<MediaAttachmentHistoryView> records = page.getRecords().stream()
                .map(this::toView)
                .collect(Collectors.toList());
        return PageResult.of(page.getCurrent(), page.getSize(), page.getTotal(), records);
    }

    private MediaAttachmentHistoryView toView(MediaAttachment row) {
        return MediaAttachmentHistoryView.builder()
                .id(row.getId())
                .userId(row.getUserId())
                .mediaType(row.getMediaType())
                .status(row.getStatus())
                .errorMessage(row.getErrorMessage())
                .blobKey(row.getBlobKey())
                .blobUrl(row.getBlobUrl())
                .cdnUrl(row.getCdnUrl())
                .createdAt(row.getCreatedAt())
                .build();
    }

    private static String normalizeOptional(String raw) {
        if (!StringUtils.hasText(raw)) {
            return null;
        }
        return raw.trim().toLowerCase(Locale.ROOT);
    }
}
