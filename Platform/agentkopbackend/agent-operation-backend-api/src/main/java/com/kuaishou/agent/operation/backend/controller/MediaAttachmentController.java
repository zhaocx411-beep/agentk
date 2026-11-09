package com.kuaishou.agent.operation.backend.controller;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import com.kuaishou.agent.operation.backend.agentk.AgentKIdentityResolver;
import com.kuaishou.agent.operation.backend.agentk.AgentKOutboundIdentity;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.dto.MediaAttachmentHistoryQuery;
import com.kuaishou.agent.operation.backend.platform.dto.MediaAttachmentHistoryView;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;
import com.kuaishou.agent.operation.backend.platform.service.MediaAttachmentService;

/**
 * 多媒体上传附件查询（aop_media_attachment）。
 */
@RestController
@RequestMapping("/agent/operation/backend/platform/media")
public class MediaAttachmentController {

    private static final Logger LOGGER = LoggerFactory.getLogger(MediaAttachmentController.class);

    private final MediaAttachmentService mediaAttachmentService;
    private final AgentKIdentityResolver identityResolver;

    public MediaAttachmentController(MediaAttachmentService mediaAttachmentService,
            AgentKIdentityResolver identityResolver) {
        this.mediaAttachmentService = mediaAttachmentService;
        this.identityResolver = identityResolver;
    }

    /**
     * 我的上传历史分页：强制当前登录用户，按创建时间倒序。
     *
     * <p>可选筛选：{@code mediaType}（image/video/audio/doc）、{@code status}（success/failed）。
     */
    @PostMapping("/my-uploads/query")
    public Message<PageResult<MediaAttachmentHistoryView>> myUploadsQuery(
            @RequestBody(required = false) MediaAttachmentHistoryQuery body) {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        String userId = identity != null ? identity.getUserId() : null;
        try {
            PageResult<MediaAttachmentHistoryView> page = mediaAttachmentService.pageMine(userId, body);
            LOGGER.info("上传历史查询 userId={} page={} pageSize={} total={}",
                    userId, page.getPage(), page.getPageSize(), page.getTotal());
            return Message.ok(page);
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, e.getMessage());
        }
    }
}
