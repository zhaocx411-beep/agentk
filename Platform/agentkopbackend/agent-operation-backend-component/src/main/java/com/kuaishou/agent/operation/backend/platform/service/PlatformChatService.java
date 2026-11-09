package com.kuaishou.agent.operation.backend.platform.service;

import java.util.Collections;
import java.util.List;
import java.util.Objects;
import java.util.stream.Collectors;

import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.metadata.IPage;
import com.kuaishou.agent.operation.backend.platform.dto.ChatSessionDetailView;
import com.kuaishou.agent.operation.backend.platform.dto.MineChatSessionQuery;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;
import com.kuaishou.agent.operation.backend.platform.dto.StreamCursorUpdateRequest;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.entity.AgentkCallLog;
import com.kuaishou.agent.operation.backend.platform.entity.ChatAsset;
import com.kuaishou.agent.operation.backend.platform.entity.ChatMessage;
import com.kuaishou.agent.operation.backend.platform.entity.ChatRun;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.entity.PipelineExec;
import com.kuaishou.agent.operation.backend.platform.entity.RunEvent;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.AdminSessionPageQuery;
import com.kuaishou.agent.operation.backend.platform.support.ChatSessionKinds;
import com.kuaishou.agent.operation.backend.platform.support.PlatformBadRequestException;

/**
 * 个人会话 / run / event / cursor 查询（不含作品集、不含 agent-k 回填）。
 */
@Service
public class PlatformChatService {

    private final ChatPersistService chatPersistService;
    private final AgentVersionService agentVersionService;
    private final WorkflowVersionService workflowVersionService;
    private final PlatformAssetAccessService assetAccessService;
    private final ChatSessionDetailAssembler sessionDetailAssembler;
    private final PlatformAdminService platformAdminService;
    private final BlobStoreUrlService blobStoreUrlService;

    public PlatformChatService(ChatPersistService chatPersistService,
            AgentVersionService agentVersionService,
            WorkflowVersionService workflowVersionService,
            PlatformAssetAccessService assetAccessService,
            ChatSessionDetailAssembler sessionDetailAssembler,
            PlatformAdminService platformAdminService,
            BlobStoreUrlService blobStoreUrlService) {
        this.chatPersistService = chatPersistService;
        this.agentVersionService = agentVersionService;
        this.workflowVersionService = workflowVersionService;
        this.assetAccessService = assetAccessService;
        this.sessionDetailAssembler = sessionDetailAssembler;
        this.platformAdminService = platformAdminService;
        this.blobStoreUrlService = blobStoreUrlService;
    }

    public List<ChatSession> listMine(String userId, Integer limit, Long agentLoopVersionId,
            Long workflowVersionId) {
        int lim = limit == null ? 50 : limit;
        boolean superAdmin = isSuperAdmin(userId);
        if (agentLoopVersionId != null && agentLoopVersionId > 0) {
            if (!superAdmin) {
                assetAccessService.assertReadableAgentVersion(userId, agentLoopVersionId);
            }
            if (superAdmin) {
                return chatPersistService.listByAgentVersion(agentLoopVersionId, lim);
            }
            return chatPersistService.listMineByAgentVersion(userId, agentLoopVersionId, lim);
        }
        if (workflowVersionId != null && workflowVersionId > 0) {
            if (!superAdmin) {
                assetAccessService.assertReadableWorkflowVersion(userId, workflowVersionId);
            }
            if (superAdmin) {
                return chatPersistService.listByWorkflowVersion(workflowVersionId, lim);
            }
            return chatPersistService.listMineByWorkflowVersion(userId, workflowVersionId, lim);
        }
        if (superAdmin) {
            AdminSessionPageQuery pageQuery = new AdminSessionPageQuery();
            pageQuery.setPage(1L);
            pageQuery.setPageSize((long) lim);
            return chatPersistService.pageForAdminReview(pageQuery).getRecords();
        }
        return chatPersistService.listMine(userId, lim);
    }

    public PageResult<ChatSession> queryMine(String userId, MineChatSessionQuery body) {
        if (body == null) {
            body = new MineChatSessionQuery();
        }
        boolean superAdmin = isSuperAdmin(userId);
        AdminSessionPageQuery pageQuery = new AdminSessionPageQuery();
        if (superAdmin) {
            if (StringUtils.hasText(body.getUserId())) {
                pageQuery.setUserId(body.getUserId().trim());
            }
        } else {
            pageQuery.setUserId(userId);
        }
        pageQuery.setStatus(body.getStatus());
        pageQuery.setTitleKeyword(body.getTitleKeyword());
        pageQuery.setCreatedFrom(body.getCreatedFrom());
        pageQuery.setCreatedTo(body.getCreatedTo());
        pageQuery.setPage(body.getPage() == null ? 1L : body.getPage().longValue());
        pageQuery.setPageSize(body.getPageSize() == null ? 20L : body.getPageSize().longValue());

        Long agentLoopVersionId = body.getAgentLoopVersionId();
        Long workflowVersionId = body.getWorkflowVersionId();
        Long agentLoopId = body.getAgentLoopId();
        Long workflowId = body.getWorkflowId();
        if (agentLoopVersionId != null && agentLoopVersionId > 0) {
            if (!superAdmin) {
                assetAccessService.assertReadableAgentVersion(userId, agentLoopVersionId);
            }
            pageQuery.setAgentVersionIds(Collections.singletonList(agentLoopVersionId));
            pageQuery.setSessionKind(firstNonBlank(
                    ChatSessionKinds.normalize(body.getSessionKind()), ChatSessionKinds.AGENT_LOOP));
        } else if (workflowVersionId != null && workflowVersionId > 0) {
            if (!superAdmin) {
                assetAccessService.assertReadableWorkflowVersion(userId, workflowVersionId);
            }
            pageQuery.setWorkflowVersionIds(Collections.singletonList(workflowVersionId));
            pageQuery.setSessionKind(firstNonBlank(
                    ChatSessionKinds.normalize(body.getSessionKind()), ChatSessionKinds.WORKFLOW));
        } else if (agentLoopId != null && agentLoopId > 0) {
            if (!superAdmin) {
                assetAccessService.assertReadableAgentLoop(userId, agentLoopId);
            }
            List<Long> versionIds = agentVersionService.listVersions(agentLoopId).stream()
                    .map(AgentVersion::getId)
                    .filter(Objects::nonNull)
                    .collect(Collectors.toList());
            pageQuery.setAgentVersionIds(versionIds);
            pageQuery.setSessionKind(firstNonBlank(
                    ChatSessionKinds.normalize(body.getSessionKind()), ChatSessionKinds.AGENT_LOOP));
        } else if (workflowId != null && workflowId > 0) {
            if (!superAdmin) {
                assetAccessService.assertReadableWorkflow(userId, workflowId);
            }
            List<Long> versionIds = workflowVersionService.listVersions(workflowId).stream()
                    .map(WorkflowVersion::getId)
                    .filter(Objects::nonNull)
                    .collect(Collectors.toList());
            pageQuery.setWorkflowVersionIds(versionIds);
            pageQuery.setSessionKind(firstNonBlank(
                    ChatSessionKinds.normalize(body.getSessionKind()), ChatSessionKinds.WORKFLOW));
        } else if (StringUtils.hasText(body.getSessionKind())) {
            String kind = ChatSessionKinds.normalize(body.getSessionKind());
            if (kind == null) {
                throw new PlatformBadRequestException(
                        "sessionKind 仅支持 agent_loop / workflow|pipeline / custom");
            }
            pageQuery.setSessionKind(kind);
        }

        IPage<ChatSession> page = chatPersistService.pageForAdminReview(pageQuery);
        return PageResult.of(page.getCurrent(), page.getSize(), page.getTotal(), page.getRecords());
    }

    public ChatSessionDetailView sessionDetail(String userId, long sessionId) {
        return sessionDetailAssembler.toSessionDetail(requireReadableSession(userId, sessionId));
    }

    public ChatSessionDetailView sessionDetailByPublicId(String userId, String publicId) {
        return sessionDetailAssembler.toSessionDetail(requireReadableByPublicId(userId, publicId));
    }

    public ChatSessionDetailView sessionDetailByAgentk(String userId, String agentkSessionId) {
        return sessionDetailAssembler.toSessionDetail(requireReadableByAgentk(userId, agentkSessionId));
    }

    public ChatSessionDetailView updateSessionTitle(String userId, String agentkSessionId, String title) {
        ChatSession session = chatPersistService.updateSessionTitleOwned(agentkSessionId, userId, title);
        return sessionDetailAssembler.toSessionDetail(session);
    }

    public List<ChatMessage> sessionMessages(String userId, long sessionId, Integer limit) {
        int lim = limit == null ? 200 : limit;
        ChatSession session = requireReadableSession(userId, sessionId);
        return chatPersistService.listMessages(session.getId(), lim);
    }

    public List<ChatAsset> myAssets(String userId, String assetType, Integer limit) {
        return chatPersistService.listAssetsOwned(userId, assetType, limit == null ? 200 : limit);
    }

    public List<ChatRun> sessionRuns(String userId, long sessionId, Integer limit) {
        int lim = limit == null ? 50 : limit;
        ChatSession session = requireReadableSession(userId, sessionId);
        return chatPersistService.listRuns(session.getId(), lim);
    }

    public List<PipelineExec> sessionPipelineExecs(String userId, long sessionId, Integer limit) {
        ChatSession session = requireReadableSession(userId, sessionId);
        return enrichPipelineExecs(chatPersistService.listPipelineExecsOwned(session.getId(), session.getUserId(),
                limit == null ? 50 : limit));
    }

    public List<PipelineExec> pipelineExecsByAgentkSession(String userId, String agentkSessionId, Integer limit) {
        requireReadableByAgentk(userId, agentkSessionId);
        return enrichPipelineExecs(chatPersistService.listPipelineExecsByAgentkSessionId(
                agentkSessionId, limit == null ? 50 : limit));
    }

    /**
     * 按 pipeline_name（如 KEffects taskType）分页查执行记录。
     * 普通用户仅本人；超管可查该 pipeline 下全部。
     */
    public PageResult<PipelineExec> pipelineExecsByPipelineName(String userId, String pipelineName,
            Integer page, Integer pageSize) {
        if (!StringUtils.hasText(pipelineName)) {
            throw new IllegalArgumentException("pipelineName is required");
        }
        String ownerFilter = isSuperAdmin(userId) ? null : userId;
        long pageNo = page == null || page <= 0 ? 1 : page;
        long size = pageSize == null || pageSize <= 0 ? 20 : pageSize;
        IPage<PipelineExec> result = chatPersistService.pagePipelineExecsByPipelineName(
                pipelineName.trim(), ownerFilter, pageNo, size);
        return PageResult.of(result.getCurrent(), result.getSize(), result.getTotal(),
                enrichPipelineExecs(result.getRecords()));
    }

    public PipelineExec pipelineExecDetail(String userId, long id) {
        if (isSuperAdmin(userId)) {
            PipelineExec exec = chatPersistService.requirePipelineExec(id);
            requireReadableSession(userId, exec.getSessionId());
            return enrichPipelineExec(exec);
        }
        return enrichPipelineExec(chatPersistService.requireOwnedPipelineExec(id, userId));
    }

    public List<AgentkCallLog> sessionCallLogs(String userId, long sessionId, Integer limit) {
        ChatSession session = requireReadableSession(userId, sessionId);
        return chatPersistService.listCallLogsBySessionOwned(session.getId(), session.getUserId(),
                limit == null ? 50 : limit);
    }

    public ChatRun runDetail(String userId, long runId) {
        if (isSuperAdmin(userId)) {
            ChatRun run = chatPersistService.requireRun(runId);
            requireReadableSession(userId, run.getSessionId());
            return run;
        }
        return chatPersistService.requireOwnedRun(runId, userId);
    }

    public ChatRun runDetailByAgentk(String userId, String agentkRunId) {
        if (isSuperAdmin(userId)) {
            ChatRun run = chatPersistService.requireRunByAgentkId(agentkRunId);
            requireReadableSession(userId, run.getSessionId());
            return run;
        }
        return chatPersistService.requireOwnedRunByAgentkId(agentkRunId, userId);
    }

    public List<RunEvent> runEvents(String userId, long runId, Integer limit) {
        int lim = limit == null ? 200 : limit;
        ChatRun run = runDetail(userId, runId);
        return chatPersistService.listRunEvents(run.getId(), lim);
    }

    public List<RunEvent> runEventsByAgentk(String userId, String agentkRunId, Integer limit) {
        int lim = limit == null ? 200 : limit;
        ChatRun run = runDetailByAgentk(userId, agentkRunId);
        return chatPersistService.listRunEvents(run.getId(), lim);
    }

    public List<AgentkCallLog> myCallLogs(String userId, Integer limit) {
        return chatPersistService.listCallLogsMine(userId, limit == null ? 50 : limit);
    }

    public void updateCursor(String userId, String agentkSessionId, StreamCursorUpdateRequest cursorUpdateRequest) {
        int seq = cursorUpdateRequest.getLastEventSeq() == null ? 0 : cursorUpdateRequest.getLastEventSeq();
        chatPersistService.updateStreamCursorOwned(userId, agentkSessionId,
                cursorUpdateRequest.getAgentkRunId(), seq, cursorUpdateRequest.getActiveRunStatus());
    }

    private ChatSession requireReadableSession(String userId, long sessionId) {
        if (isSuperAdmin(userId)) {
            return chatPersistService.requireSession(sessionId);
        }
        return chatPersistService.requireOwnedSession(sessionId, userId);
    }

    private ChatSession requireReadableByPublicId(String userId, String publicId) {
        if (isSuperAdmin(userId)) {
            return chatPersistService.findByPublicId(publicId)
                    .orElseThrow(() -> new IllegalArgumentException("session not found: " + publicId));
        }
        return chatPersistService.requireOwnedByPublicId(publicId, userId);
    }

    private ChatSession requireReadableByAgentk(String userId, String agentkSessionId) {
        if (isSuperAdmin(userId)) {
            return chatPersistService.findByAgentkSessionId(agentkSessionId)
                    .orElseThrow(() -> new IllegalArgumentException(
                            "session not found for agentkSessionId: " + agentkSessionId));
        }
        return chatPersistService.requireOwnedByAgentkSessionId(agentkSessionId, userId);
    }

    private boolean isSuperAdmin(String userId) {
        return platformAdminService.isSuperAdmin(userId);
    }

    private List<PipelineExec> enrichPipelineExecs(List<PipelineExec> execs) {
        if (execs == null || execs.isEmpty()) {
            return execs;
        }
        List<PipelineExec> enriched = new java.util.ArrayList<>(execs.size());
        for (PipelineExec exec : execs) {
            enriched.add(enrichPipelineExec(exec));
        }
        return enriched;
    }

    private PipelineExec enrichPipelineExec(PipelineExec exec) {
        if (exec == null || !StringUtils.hasText(exec.getOutputJson())) {
            return exec;
        }
        exec.setOutputJson(blobStoreUrlService.enrichOutputJson(exec.getOutputJson(), exec.getUpdatedAt()));
        return exec;
    }

    private static String firstNonBlank(String preferred, String fallback) {
        return StringUtils.hasText(preferred) ? preferred.trim() : fallback;
    }
}
