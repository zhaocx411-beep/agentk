package com.kuaishou.agent.operation.backend.platform.service;

import java.util.Collections;
import java.util.List;
import java.util.Objects;
import java.util.stream.Collectors;

import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.metadata.IPage;
import com.kuaishou.agent.operation.backend.platform.dto.AdminChatSessionQuery;
import com.kuaishou.agent.operation.backend.platform.dto.ChatCaseReviewView;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;
import com.kuaishou.agent.operation.backend.platform.entity.Agent;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.entity.ChatAsset;
import com.kuaishou.agent.operation.backend.platform.entity.ChatRun;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.entity.PipelineExec;
import com.kuaishou.agent.operation.backend.platform.entity.RunEvent;
import com.kuaishou.agent.operation.backend.platform.entity.Workflow;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.AdminSessionPageQuery;
import com.kuaishou.agent.operation.backend.platform.support.ChatSessionKinds;
import com.kuaishou.agent.operation.backend.platform.support.PlatformAccessException;

/**
 * 管理/资产 owner 回看会话 Case。
 */
@Service
public class AdminChatReviewService {

    private final ChatPersistService chatPersistService;
    private final AgentVersionService agentVersionService;
    private final WorkflowVersionService workflowVersionService;
    private final PlatformAdminService platformAdminService;
    private final ChatSessionDetailAssembler sessionDetailAssembler;
    private final BlobStoreUrlService blobStoreUrlService;
    private final BlobStoreOfficeUrlService officeUrlService;
    private final boolean permissionStub;

    public AdminChatReviewService(ChatPersistService chatPersistService,
            AgentVersionService agentVersionService,
            WorkflowVersionService workflowVersionService,
            PlatformAdminService platformAdminService,
            ChatSessionDetailAssembler sessionDetailAssembler,
            BlobStoreUrlService blobStoreUrlService,
            BlobStoreOfficeUrlService officeUrlService,
            @Value("${agent.operation.backend.auth.permission-stub:false}") boolean permissionStub) {
        this.chatPersistService = chatPersistService;
        this.agentVersionService = agentVersionService;
        this.workflowVersionService = workflowVersionService;
        this.platformAdminService = platformAdminService;
        this.sessionDetailAssembler = sessionDetailAssembler;
        this.blobStoreUrlService = blobStoreUrlService;
        this.officeUrlService = officeUrlService;
        this.permissionStub = permissionStub;
    }

    public PageResult<ChatSession> listSessions(String userId, AdminChatSessionQuery body) {
        if (body == null) {
            body = new AdminChatSessionQuery();
        }
        AdminSessionPageQuery pageQuery = new AdminSessionPageQuery();
        pageQuery.setUserId(body.getUserId());
        pageQuery.setStatus(body.getStatus());
        pageQuery.setTitleKeyword(body.getTitleKeyword());
        pageQuery.setCreatedFrom(body.getCreatedFrom());
        pageQuery.setCreatedTo(body.getCreatedTo());
        pageQuery.setPage(body.getPage() == null ? 1L : body.getPage().longValue());
        pageQuery.setPageSize(body.getPageSize() == null ? 20L : body.getPageSize().longValue());
        applySessionIdFilter(pageQuery, body.getSessionId());

        Long agentLoopVersionId = body.getAgentLoopVersionId();
        Long workflowVersionId = body.getWorkflowVersionId();
        Long agentLoopId = body.getAgentLoopId();
        Long workflowId = body.getWorkflowId();
        if (agentLoopVersionId != null && agentLoopVersionId > 0) {
            assertCanReviewAgentVersion(userId, agentLoopVersionId);
            pageQuery.setAgentVersionIds(Collections.singletonList(agentLoopVersionId));
            pageQuery.setSessionKind(firstNonBlank(
                    ChatSessionKinds.normalize(body.getSessionKind()), ChatSessionKinds.AGENT_LOOP));
        } else if (workflowVersionId != null && workflowVersionId > 0) {
            assertCanReviewWorkflowVersion(userId, workflowVersionId);
            pageQuery.setWorkflowVersionIds(Collections.singletonList(workflowVersionId));
            pageQuery.setSessionKind(firstNonBlank(
                    ChatSessionKinds.normalize(body.getSessionKind()), ChatSessionKinds.WORKFLOW));
        } else if (agentLoopId != null && agentLoopId > 0) {
            assertCanReviewAgentLoop(userId, agentLoopId);
            List<Long> versionIds = agentVersionService.listVersions(agentLoopId).stream()
                    .map(AgentVersion::getId)
                    .filter(Objects::nonNull)
                    .collect(Collectors.toList());
            if (versionIds.isEmpty()) {
                return PageResult.empty(pageQuery.getPage(), pageQuery.getPageSize());
            }
            pageQuery.setAgentVersionIds(versionIds);
            pageQuery.setSessionKind(firstNonBlank(
                    ChatSessionKinds.normalize(body.getSessionKind()), ChatSessionKinds.AGENT_LOOP));
        } else if (workflowId != null && workflowId > 0) {
            assertCanReviewWorkflow(userId, workflowId);
            List<Long> versionIds = workflowVersionService.listVersions(workflowId).stream()
                    .map(WorkflowVersion::getId)
                    .filter(Objects::nonNull)
                    .collect(Collectors.toList());
            if (versionIds.isEmpty()) {
                return PageResult.empty(pageQuery.getPage(), pageQuery.getPageSize());
            }
            pageQuery.setWorkflowVersionIds(versionIds);
            pageQuery.setSessionKind(firstNonBlank(
                    ChatSessionKinds.normalize(body.getSessionKind()), ChatSessionKinds.WORKFLOW));
        } else {
            assertGlobalAdmin(userId);
            if (StringUtils.hasText(body.getSessionKind())) {
                String kind = ChatSessionKinds.normalize(body.getSessionKind());
                if (kind != null) {
                    pageQuery.setSessionKind(kind);
                }
            }
        }

        IPage<ChatSession> page = chatPersistService.pageForAdminReview(pageQuery);
        return PageResult.of(page.getCurrent(), page.getSize(), page.getTotal(), page.getRecords());
    }

    public List<ChatRun> sessionRunsByAgentk(String userId, String agentkSessionId, Integer limit) {
        ChatSession session = chatPersistService.findByAgentkSessionId(agentkSessionId)
                .orElseThrow(() -> new IllegalArgumentException(
                        "session not found for agentkSessionId: " + agentkSessionId));
        assertCanReviewSession(userId, session);
        return chatPersistService.listRuns(session.getId(), limit == null ? 100 : limit);
    }

    public ChatCaseReviewView sessionCaseByAgentk(String userId, String agentkSessionId, String sessionKind,
            Integer messageLimit, Integer runLimit, Integer callLogLimit, Integer eventLimit) {
        ChatSession session = chatPersistService.findByAgentkSessionId(agentkSessionId)
                .orElseThrow(() -> new IllegalArgumentException(
                        "session not found for agentkSessionId: " + agentkSessionId));
        assertCanReviewSession(userId, session);
        long sessionId = session.getId();
        String kind = resolveCaseSessionKind(sessionKind, session);
        int processLimit = runLimit == null ? 100 : runLimit;
        int eventsLimit = eventLimit == null ? 2000 : Math.min(Math.max(eventLimit, 1), 5000);
        ChatCaseReviewView view = new ChatCaseReviewView();
        view.setSession(sessionDetailAssembler.toSessionDetail(session));
        view.setCallLogs(chatPersistService.listCallLogsBySession(sessionId,
                callLogLimit == null ? 100 : callLogLimit));
        view.setEvents(rewriteEventUrls(chatPersistService.listSessionEvents(
                sessionId, session.getAgentkSessionId(), eventsLimit)));
        if (!ChatSessionKinds.WORKFLOW.equals(kind)) {
            view.setRuns(chatPersistService.listRuns(sessionId, processLimit));
            view.setMessages(chatPersistService.listMessages(sessionId,
                    messageLimit == null ? 500 : messageLimit));
        }
        view.setPipelineExecs(enrichPipelineExecs(chatPersistService.listPipelineExecsOwned(sessionId,
                session.getUserId(), processLimit)));
        view.setAssets(rewriteAssetUrls(chatPersistService.listAssetsBySession(sessionId, processLimit)));
        return view;
    }

    public PageResult<RunEvent> sessionEventsByAgentk(String userId, String agentkSessionId,
            Integer page, Integer pageSize) {
        ChatSession session = chatPersistService.findByAgentkSessionId(agentkSessionId)
                .orElseThrow(() -> new IllegalArgumentException(
                        "session not found for agentkSessionId: " + agentkSessionId));
        assertCanReviewSession(userId, session);
        IPage<RunEvent> result = chatPersistService.pageSessionEventsByAgentk(
                session.getAgentkSessionId(),
                page == null ? 1L : page.longValue(),
                pageSize == null ? 20L : pageSize.longValue());
        return PageResult.of(result.getCurrent(), result.getSize(), result.getTotal(), result.getRecords());
    }

    public List<RunEvent> runEvents(String userId, long runId, Integer limit) {
        ChatRun run = chatPersistService.requireRun(runId);
        ChatSession session = chatPersistService.requireSession(run.getSessionId());
        assertCanReviewSession(userId, session);
        return chatPersistService.listRunEvents(runId, limit == null ? 500 : limit);
    }

    public List<RunEvent> runEventsByAgentk(String userId, String agentkRunId, Integer limit) {
        ChatRun run = chatPersistService.requireRunByAgentkId(agentkRunId);
        ChatSession session = chatPersistService.requireSession(run.getSessionId());
        assertCanReviewSession(userId, session);
        return chatPersistService.listRunEvents(run.getId(), limit == null ? 500 : limit);
    }

    private void assertCanReviewSession(String userId, ChatSession session) {
        if (isGlobalAdmin(userId)) {
            return;
        }
        if (Objects.equals(empty(session.getUserId()), empty(userId))) {
            return;
        }
        if (session.getAgentVersionId() != null) {
            AgentVersion av = agentVersionService.findVersion(session.getAgentVersionId());
            if (av != null) {
                Agent agent = agentVersionService.findAgent(av.getAgentId());
                if (agent != null && Objects.equals(empty(agent.getOwnerUserId()), empty(userId))) {
                    return;
                }
            }
        }
        if (session.getWorkflowVersionId() != null) {
            WorkflowVersion wv = workflowVersionService.findVersion(session.getWorkflowVersionId());
            if (wv != null) {
                Workflow wf = workflowVersionService.findWorkflow(wv.getWorkflowId());
                if (wf != null && Objects.equals(empty(wf.getOwnerUserId()), empty(userId))) {
                    return;
                }
            }
        }
        throw new PlatformAccessException("admin chat review forbidden");
    }

    private void assertCanReviewAgentVersion(String userId, long agentVersionId) {
        if (isGlobalAdmin(userId)) {
            return;
        }
        AgentVersion av = agentVersionService.findVersion(agentVersionId);
        if (av == null) {
            throw new IllegalArgumentException("agent version not found: " + agentVersionId);
        }
        assertCanReviewAgentLoop(userId, av.getAgentId());
    }

    private void assertCanReviewWorkflowVersion(String userId, long workflowVersionId) {
        if (isGlobalAdmin(userId)) {
            return;
        }
        WorkflowVersion wv = workflowVersionService.findVersion(workflowVersionId);
        if (wv == null) {
            throw new IllegalArgumentException("workflow version not found: " + workflowVersionId);
        }
        assertCanReviewWorkflow(userId, wv.getWorkflowId());
    }

    private void assertCanReviewAgentLoop(String userId, long agentLoopId) {
        if (isGlobalAdmin(userId)) {
            return;
        }
        Agent agent = agentVersionService.findAgent(agentLoopId);
        if (agent == null) {
            throw new IllegalArgumentException("agent loop not found: " + agentLoopId);
        }
        if (!Objects.equals(empty(agent.getOwnerUserId()), empty(userId))) {
            throw new PlatformAccessException("not owner of agent loop: " + agentLoopId);
        }
    }

    private void assertCanReviewWorkflow(String userId, long workflowId) {
        if (isGlobalAdmin(userId)) {
            return;
        }
        Workflow workflow = workflowVersionService.findWorkflow(workflowId);
        if (workflow == null) {
            throw new IllegalArgumentException("workflow not found: " + workflowId);
        }
        if (!Objects.equals(empty(workflow.getOwnerUserId()), empty(userId))) {
            throw new PlatformAccessException("not owner of workflow: " + workflowId);
        }
    }

    private void assertGlobalAdmin(String userId) {
        if (!isGlobalAdmin(userId)) {
            throw new PlatformAccessException(
                    "global session list requires admin; pass agentLoopId/workflowId to scope as owner");
        }
    }

    private boolean isGlobalAdmin(String userId) {
        if (permissionStub) {
            return true;
        }
        return platformAdminService.isGlobalAdmin(userId);
    }

    private static String resolveCaseSessionKind(String requested, ChatSession session) {
        if (StringUtils.hasText(requested)) {
            String kind = ChatSessionKinds.normalize(requested);
            if (!ChatSessionKinds.AGENT_LOOP.equals(kind) && !ChatSessionKinds.WORKFLOW.equals(kind)) {
                throw new IllegalArgumentException("sessionKind 仅支持 agent_loop / workflow");
            }
            return kind;
        }
        return ChatSessionKinds.normalize(session.getSessionKind());
    }

    /**
     * sessionId 仅支持 agent-k {@code ses_xxx}。
     */
    private static void applySessionIdFilter(AdminSessionPageQuery pageQuery, String sessionId) {
        if (!StringUtils.hasText(sessionId) || pageQuery == null) {
            return;
        }
        String trimmed = sessionId.trim();
        if (!trimmed.startsWith("ses_")) {
            throw new IllegalArgumentException(
                    "sessionId 仅支持 agent-k ses_xxx，当前值: " + trimmed);
        }
        pageQuery.setAgentkSessionId(trimmed);
    }

    private static String empty(String s) {
        return s == null ? "" : s;
    }

    private static String firstNonBlank(String preferred, String fallback) {
        return StringUtils.hasText(preferred) ? preferred.trim() : fallback;
    }

    private List<PipelineExec> enrichPipelineExecs(List<PipelineExec> execs) {
        if (execs == null || execs.isEmpty()) {
            return execs;
        }
        for (PipelineExec exec : execs) {
            if (exec != null && StringUtils.hasText(exec.getOutputJson())) {
                exec.setOutputJson(blobStoreUrlService.enrichOutputJson(
                        exec.getOutputJson(), exec.getUpdatedAt()));
            }
        }
        return execs;
    }

    private List<ChatAsset> rewriteAssetUrls(List<ChatAsset> assets) {
        if (assets == null || assets.isEmpty()) {
            return assets;
        }
        for (ChatAsset asset : assets) {
            if (asset != null) {
                asset.setAssetUrl(officeUrlService.toOfficeUrl(asset.getAssetUrl()));
            }
        }
        return assets;
    }

    private List<RunEvent> rewriteEventUrls(List<RunEvent> events) {
        if (events == null || events.isEmpty()) {
            return events;
        }
        for (RunEvent event : events) {
            if (event != null && StringUtils.hasText(event.getPayloadJson())) {
                event.setPayloadJson(officeUrlService.toOfficeUrl(event.getPayloadJson()));
            }
        }
        return events;
    }
}
