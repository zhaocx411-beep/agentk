package com.kuaishou.agent.operation.backend.platform.service.impl;

import java.sql.SQLIntegrityConstraintViolationException;
import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.Optional;
import java.util.Set;
import java.util.stream.Collectors;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.baomidou.mybatisplus.core.metadata.IPage;
import com.baomidou.mybatisplus.extension.plugins.pagination.Page;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.batcheval.mapper.persistence.BatchevalOfflineJobCellMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.AgentMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.AgentVersionMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.AgentkCallLogMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.ChatAssetMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.ChatJobMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.ChatMessageMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.ChatRunMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.ChatSessionMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.PipelineExecMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.RunEventMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.WorkflowMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.WorkflowVersionMapper;
import com.kuaishou.agent.operation.backend.platform.entity.Agent;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.entity.AgentkCallLog;
import com.kuaishou.agent.operation.backend.platform.entity.ChatAsset;
import com.kuaishou.agent.operation.backend.platform.entity.ChatJob;
import com.kuaishou.agent.operation.backend.platform.entity.ChatMessage;
import com.kuaishou.agent.operation.backend.platform.entity.ChatRun;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.entity.PipelineExec;
import com.kuaishou.agent.operation.backend.platform.entity.RunEvent;
import com.kuaishou.agent.operation.backend.platform.entity.Workflow;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;
import com.kuaishou.agent.operation.backend.platform.service.ChatAssetPortfolioService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.support.ChatAssetSources;
import com.kuaishou.agent.operation.backend.platform.support.ChatEntrySources;
import com.kuaishou.agent.operation.backend.platform.support.ChatSessionKinds;
import com.kuaishou.agent.operation.backend.platform.support.ChatSessionTitles;
import com.kuaishou.agent.operation.backend.platform.support.PlatformAccessException;
import com.kuaishou.agent.operation.backend.platform.support.PlatformDateTimes;
import com.kuaishou.agent.operation.backend.platform.support.PlatformIds;

/**
 * Chat 落库实现（MyBatis-Plus）。
 */
@Service
public class ChatPersistServiceImpl implements ChatPersistService {

    private static final Logger LOGGER = LoggerFactory.getLogger(ChatPersistServiceImpl.class);

    /** 与 {@code asset_url(191)} 唯一索引前缀长度一致。 */
    private static final int ASSET_URL_UNIQUE_PREFIX_LEN = 191;

    /** 无上游 seq 时撞 (agentk_session_id, seq) 后最多重试次数。 */
    private static final int MAX_SESSION_SEQ_RETRY = 8;

    /** 与 {@link com.kuaishou.agent.operation.backend.batcheval.service.impl.OfflineArtifactRecorder} 登记入口一致。 */
    private static final String ENTRY_SOURCE_OFFLINE_BATCH = ChatEntrySources.OFFLINE_BATCH;

    private static final int DEFAULT_PURGE_BATCH_SIZE = 500;
    private static final int MAX_PURGE_BATCH_SIZE = 5000;

    private final ChatSessionMapper chatSessionMapper;
    private final ChatRunMapper chatRunMapper;
    private final ChatMessageMapper chatMessageMapper;
    private final ChatAssetMapper chatAssetMapper;
    private final ChatJobMapper chatJobMapper;
    private final PipelineExecMapper pipelineExecMapper;
    private final AgentkCallLogMapper callLogMapper;
    private final RunEventMapper runEventMapper;
    private final AgentMapper agentMapper;
    private final AgentVersionMapper agentVersionMapper;
    private final WorkflowMapper workflowMapper;
    private final WorkflowVersionMapper workflowVersionMapper;
    private final ObjectMapper objectMapper;
    private final BatchevalOfflineJobCellMapper offlineJobCellMapper;

    public ChatPersistServiceImpl(ChatSessionMapper chatSessionMapper, ChatRunMapper chatRunMapper,
            ChatMessageMapper chatMessageMapper, ChatAssetMapper chatAssetMapper, ChatJobMapper chatJobMapper,
            PipelineExecMapper pipelineExecMapper, AgentkCallLogMapper callLogMapper, RunEventMapper runEventMapper,
            AgentMapper agentMapper, AgentVersionMapper agentVersionMapper,
            WorkflowMapper workflowMapper, WorkflowVersionMapper workflowVersionMapper,
            ObjectMapper objectMapper, BatchevalOfflineJobCellMapper offlineJobCellMapper) {
        this.chatSessionMapper = chatSessionMapper;
        this.chatRunMapper = chatRunMapper;
        this.chatMessageMapper = chatMessageMapper;
        this.chatAssetMapper = chatAssetMapper;
        this.chatJobMapper = chatJobMapper;
        this.pipelineExecMapper = pipelineExecMapper;
        this.callLogMapper = callLogMapper;
        this.runEventMapper = runEventMapper;
        this.agentMapper = agentMapper;
        this.agentVersionMapper = agentVersionMapper;
        this.workflowMapper = workflowMapper;
        this.workflowVersionMapper = workflowVersionMapper;
        this.objectMapper = objectMapper;
        this.offlineJobCellMapper = offlineJobCellMapper;
    }

    @Override
    public CreateSessionBegin beginCreateSession(String userId, String tenantId, String title,
            Long agentVersionId, Long workflowVersionId, String specJson, String profileYaml) {
        return beginCreateSession(userId, tenantId, title, agentVersionId, workflowVersionId, specJson,
                profileYaml, null);
    }

    @Override
    public CreateSessionBegin beginCreateSession(String userId, String tenantId, String title,
            Long agentVersionId, Long workflowVersionId, String specJson, String profileYaml,
            EffectiveSnapshot effective) {
        LocalDateTime now = LocalDateTime.now();
        ChatSession session = new ChatSession();
        session.setPublicId(PlatformIds.newPublicId("pcs_"));
        session.setUserId(empty(userId));
        session.setTenantId(empty(tenantId));
        session.setTitle(ChatSessionTitles.resolveCreateTitle(title));
        session.setAgentVersionId(agentVersionId);
        session.setWorkflowVersionId(workflowVersionId);
        String sessionKind = effective != null && StringUtils.hasText(effective.getSessionKind())
                ? effective.getSessionKind().trim()
                : ChatSessionKinds.resolve(null, agentVersionId, workflowVersionId);
        session.setSessionKind(sessionKind);
        session.setEntrySource(ChatEntrySources.resolve(
                effective != null ? effective.getEntrySource() : null, null));
        session.setSpecJson(specJson);
        session.setProfileYaml(profileYaml);
        session.setProfileVersion(effective != null ? empty(effective.getProfileVersion()) : "");
        session.setEffectiveModel(effective != null ? empty(effective.getModel()) : "");
        session.setEffectiveToolsJson(effective != null ? effective.getToolsJson() : null);
        session.setEffectiveToolModelsJson(effective != null ? effective.getToolModelsJson() : null);
        session.setConfigSource(effective != null ? empty(effective.getConfigSource()) : "");
        session.setRejectReason("");
        session.setStatus("creating");
        session.setLastEventSeq(0);
        session.setActiveRunStatus("idle");
        session.setCreatedAt(now);
        session.setUpdatedAt(now);
        chatSessionMapper.insert(session);

        AgentkCallLog log = newCallLog("chat_session", session.getId(), userId, tenantId, "POST /v1/sessions");
        Map<String, Object> request = new HashMap<>();
        request.put("title", empty(title));
        request.put("agentLoopVersionId", agentVersionId == null ? "" : String.valueOf(agentVersionId));
        request.put("workflowVersionId", workflowVersionId == null ? "" : String.valueOf(workflowVersionId));
        request.put("sessionKind", sessionKind);
        request.put("entrySource", session.getEntrySource());
        request.put("hasProfile", StringUtils.hasText(profileYaml));
        request.put("hasSpec", StringUtils.hasText(specJson));
        request.put("configSource", session.getConfigSource());
        request.put("effectiveModel", session.getEffectiveModel());
        log.setRequestJson(safeJson(request));
        callLogMapper.insert(log);

        CreateSessionBegin begin = new CreateSessionBegin();
        begin.setSession(session);
        begin.setCallLog(log);
        return begin;
    }

    @Override
    public void completeCreateSession(CreateSessionBegin begin, String agentkSessionId, String profileVersion,
            boolean success, Integer httpStatus, String error) {
        ChatSession session = begin.getSession();
        LocalDateTime now = LocalDateTime.now();
        session.setUpdatedAt(now);
        if (success && StringUtils.hasText(agentkSessionId)) {
            session.setAgentkSessionId(agentkSessionId);
            session.setProfileVersion(empty(profileVersion));
            session.setStatus("active");
        } else {
            session.setStatus("failed");
        }
        chatSessionMapper.updateById(session);

        AgentkCallLog log = begin.getCallLog();
        log.setStatus(success ? "success" : "failed");
        log.setHttpStatus(httpStatus);
        log.setError(error);
        log.setAgentkSessionId(agentkSessionId);
        log.setFinishedAt(now);
        callLogMapper.updateById(log);
    }

    @Override
    public void reviseCreateSessionAttempt(CreateSessionBegin begin, String specJson, String profileYaml,
            EffectiveSnapshot effective) {
        if (begin == null || begin.getSession() == null) {
            throw new IllegalArgumentException("reviseCreateSessionAttempt: begin.session required");
        }
        ChatSession session = begin.getSession();
        LocalDateTime now = LocalDateTime.now();
        session.setSpecJson(specJson);
        session.setProfileYaml(profileYaml);
        if (effective != null) {
            session.setProfileVersion(empty(effective.getProfileVersion()));
            session.setEffectiveModel(empty(effective.getModel()));
            session.setEffectiveToolsJson(effective.getToolsJson());
            session.setEffectiveToolModelsJson(effective.getToolModelsJson());
            if (StringUtils.hasText(effective.getConfigSource())) {
                session.setConfigSource(effective.getConfigSource().trim());
            }
        }
        session.setRejectReason("");
        session.setStatus("creating");
        session.setUpdatedAt(now);
        chatSessionMapper.updateById(session);

        AgentkCallLog log = begin.getCallLog();
        if (log != null) {
            Map<String, Object> request = new HashMap<>();
            request.put("title", empty(session.getTitle()));
            request.put("agentLoopVersionId",
                    session.getAgentVersionId() == null ? "" : String.valueOf(session.getAgentVersionId()));
            request.put("workflowVersionId",
                    session.getWorkflowVersionId() == null ? "" : String.valueOf(session.getWorkflowVersionId()));
            request.put("sessionKind", empty(session.getSessionKind()));
            request.put("entrySource", empty(session.getEntrySource()));
            request.put("hasProfile", StringUtils.hasText(profileYaml));
            request.put("hasSpec", StringUtils.hasText(specJson));
            request.put("configSource", empty(session.getConfigSource()));
            request.put("effectiveModel", empty(session.getEffectiveModel()));
            request.put("emptyToolsRetry", true);
            log.setRequestJson(safeJson(request));
            log.setStatus("pending");
            log.setHttpStatus(null);
            log.setError(null);
            log.setFinishedAt(null);
            callLogMapper.updateById(log);
        }
    }

    @Override
    public ChatSession updateSessionTitleOwned(String agentkSessionId, String userId, String title) {
        ChatSession session = requireOwnedByAgentkSessionId(agentkSessionId, userId);
        String resolved = ChatSessionTitles.resolveManualTitle(title);
        LocalDateTime now = LocalDateTime.now();
        session.setTitle(resolved);
        session.setUpdatedAt(now);
        chatSessionMapper.updateById(session);
        return session;
    }

    @Override
    public PostMessageBegin beginPostMessage(ChatSession session, String text) {
        LocalDateTime now = LocalDateTime.now();
        maybeDeriveTitleFromFirstUserMessage(session, text);
        ChatRun run = new ChatRun();
        run.setSessionId(session.getId());
        run.setAgentkSessionId(session.getAgentkSessionId());
        run.setTriggerKind("user_message");
        run.setStatus("pending");
        run.setLastEventSeq(0);
        run.setCreatedAt(now);
        run.setStartedAt(now);
        chatRunMapper.insert(run);

        ChatMessage msg = new ChatMessage();
        msg.setSessionId(session.getId());
        msg.setRunId(run.getId());
        msg.setAgentkExecutionId(run.getAgentkRunId());
        msg.setRole("user");
        msg.setContentJson(safeJson(Collections.singletonMap("text", empty(text))));
        msg.setStatus("local");
        msg.setCreatedAt(now);
        msg.setUpdatedAt(now);
        chatMessageMapper.insert(msg);

        AgentkCallLog log = newCallLog("chat_run", run.getId(), session.getUserId(), session.getTenantId(),
                "POST /v1/sessions/{id}/messages");
        log.setAgentkSessionId(session.getAgentkSessionId());
        log.setRequestJson(safeJson(Collections.singletonMap("textLen", text == null ? 0 : text.length())));
        callLogMapper.insert(log);

        session.setActiveRunStatus("running");
        session.setUpdatedAt(now);
        chatSessionMapper.updateById(session);

        PostMessageBegin begin = new PostMessageBegin();
        begin.setSession(session);
        begin.setRun(run);
        begin.setMessage(msg);
        begin.setCallLog(log);
        return begin;
    }

    /**
     * 仅当标题仍为占位「新对话」且本条为会话内首条 user 消息时，用输入派生标题。
     */
    private void maybeDeriveTitleFromFirstUserMessage(ChatSession session, String text) {
        if (session == null || !ChatSessionTitles.isDefaultPlaceholder(session.getTitle())) {
            return;
        }
        // 本仓库 MP 版本 selectCount 返回 Integer（见 PlatformAdminServiceImpl 等）
        Integer priorUserMessages = chatMessageMapper.selectCount(new LambdaQueryWrapper<ChatMessage>()
                .eq(ChatMessage::getSessionId, session.getId())
                .eq(ChatMessage::getRole, "user"));
        if (priorUserMessages != null && priorUserMessages > 0) {
            return;
        }
        String derived = ChatSessionTitles.deriveFromUserInput(text);
        if (!derived.equals(session.getTitle())) {
            session.setTitle(derived);
        }
    }

    @Override
    public void completePostMessage(PostMessageBegin begin, String agentkRunId, String status,
            boolean success, Integer httpStatus, String error) {
        ChatSession session = begin.getSession();
        ChatRun run = begin.getRun();
        LocalDateTime now = LocalDateTime.now();
        if (success && StringUtils.hasText(agentkRunId)) {
            run.setAgentkRunId(agentkRunId);
            run.setAgentkSessionId(session.getAgentkSessionId());
            run.setStatus(StringUtils.hasText(status) ? status : "queued");
            session.setLastAgentkRunId(agentkRunId);
            session.setLastEventSeq(0);
            session.setActiveRunStatus("running");
            ChatMessage localUser = begin.getMessage();
            if (localUser != null) {
                localUser.setAgentkExecutionId(agentkRunId);
                localUser.setStatus("submitted");
                localUser.setUpdatedAt(now);
                chatMessageMapper.updateById(localUser);
            }
        } else {
            run.setStatus("failed");
            run.setError(error);
            run.setFinishedAt(now);
            session.setActiveRunStatus("idle");
            ChatMessage localUser = begin.getMessage();
            if (localUser != null) {
                localUser.setStatus("failed");
                localUser.setUpdatedAt(now);
                chatMessageMapper.updateById(localUser);
            }
        }
        chatRunMapper.updateById(run);
        session.setUpdatedAt(now);
        chatSessionMapper.updateById(session);

        AgentkCallLog log = begin.getCallLog();
        log.setStatus(success ? "success" : "failed");
        log.setHttpStatus(httpStatus);
        log.setError(error);
        log.setAgentkSessionId(session.getAgentkSessionId());
        log.setAgentkRunId(agentkRunId);
        log.setFinishedAt(now);
        callLogMapper.updateById(log);
    }

    @Override
    public void updateStreamCursor(String agentkSessionId, String agentkRunId, int lastEventSeq,
            String activeRunStatus) {
        Optional<ChatSession> opt = findByAgentkSessionId(agentkSessionId);
        if (!opt.isPresent()) {
            return;
        }
        applyStreamCursor(opt.get(), agentkRunId, lastEventSeq, activeRunStatus);
    }

    @Override
    public void updateStreamCursorOwned(String userId, String agentkSessionId, String agentkRunId,
            int lastEventSeq, String activeRunStatus) {
        ChatSession session = requireOwnedByAgentkSessionId(agentkSessionId, userId);
        applyStreamCursor(session, agentkRunId, lastEventSeq, activeRunStatus);
    }

    private void applyStreamCursor(ChatSession session, String agentkRunId, int lastEventSeq,
            String activeRunStatus) {
        if (StringUtils.hasText(agentkRunId)) {
            session.setLastAgentkRunId(agentkRunId);
            findRunByAgentkId(agentkRunId).ifPresent(run -> {
                int cur = run.getLastEventSeq() == null ? 0 : run.getLastEventSeq();
                if (lastEventSeq > cur) {
                    run.setLastEventSeq(lastEventSeq);
                }
                if (StringUtils.hasText(activeRunStatus)) {
                    run.setStatus(activeRunStatus);
                    if ("completed".equals(activeRunStatus) || "failed".equals(activeRunStatus)) {
                        run.setFinishedAt(LocalDateTime.now());
                    }
                }
                chatRunMapper.updateById(run);
            });
        }
        int sessionSeq = session.getLastEventSeq() == null ? 0 : session.getLastEventSeq();
        if (lastEventSeq > sessionSeq) {
            session.setLastEventSeq(lastEventSeq);
        }
        if (StringUtils.hasText(activeRunStatus)) {
            if ("completed".equals(activeRunStatus) || "failed".equals(activeRunStatus)
                    || "idle".equals(activeRunStatus)) {
                session.setActiveRunStatus("idle");
            } else {
                session.setActiveRunStatus(activeRunStatus);
            }
        }
        session.setUpdatedAt(LocalDateTime.now());
        chatSessionMapper.updateById(session);
    }

    @Override
    public void upsertJob(String agentkSessionId, String agentkRunId, String agentkJobId,
            String toolCallId, String kind, String status, String wakeRunId) {
        Optional<ChatSession> opt = findByAgentkSessionId(agentkSessionId);
        if (!opt.isPresent()) {
            return;
        }
        ChatSession session = opt.get();
        Long runId = findRunByAgentkId(agentkRunId).map(ChatRun::getId).orElse(null);
        ChatJob job = new ChatJob();
        job.setSessionId(session.getId());
        job.setRunId(runId);
        job.setAgentkJobId(agentkJobId);
        job.setToolCallId(empty(toolCallId));
        job.setKind(empty(kind));
        job.setStatus(StringUtils.hasText(status) ? status : "submitted");
        job.setWakeRunId(wakeRunId);
        LocalDateTime now = LocalDateTime.now();
        job.setCreatedAt(now);
        job.setUpdatedAt(now);
        chatJobMapper.insert(job);
        if ("submitted".equals(job.getStatus())) {
            session.setActiveRunStatus("waiting_job");
            session.setUpdatedAt(now);
            chatSessionMapper.updateById(session);
        }
    }

    @Override
    public Optional<ChatSession> findByAgentkSessionId(String agentkSessionId) {
        if (!StringUtils.hasText(agentkSessionId)) {
            return Optional.empty();
        }
        return Optional.ofNullable(chatSessionMapper.selectOne(new LambdaQueryWrapper<ChatSession>()
                .eq(ChatSession::getAgentkSessionId, agentkSessionId)
                .last("LIMIT 1")));
    }

    @Override
    public Optional<ChatSession> findById(long sessionId) {
        return Optional.ofNullable(chatSessionMapper.selectById(sessionId));
    }

    @Override
    public Optional<ChatSession> findByPublicId(String publicId) {
        if (!StringUtils.hasText(publicId)) {
            return Optional.empty();
        }
        return Optional.ofNullable(chatSessionMapper.selectOne(new LambdaQueryWrapper<ChatSession>()
                .eq(ChatSession::getPublicId, publicId)
                .last("LIMIT 1")));
    }

    @Override
    public ChatSession requireOwnedSession(long sessionId, String userId) {
        ChatSession session = chatSessionMapper.selectById(sessionId);
        if (session == null) {
            throw new IllegalArgumentException("session not found: " + sessionId);
        }
        assertOwner(session, userId);
        return session;
    }

    @Override
    public ChatSession requireOwnedByPublicId(String publicId, String userId) {
        ChatSession session = findByPublicId(publicId)
                .orElseThrow(() -> new IllegalArgumentException("session not found: " + publicId));
        assertOwner(session, userId);
        return session;
    }

    @Override
    public ChatSession requireOwnedByAgentkSessionId(String agentkSessionId, String userId) {
        ChatSession session = findByAgentkSessionId(agentkSessionId)
                .orElseThrow(() -> new IllegalArgumentException(
                        "session not found for agentkSessionId: " + agentkSessionId));
        assertOwner(session, userId);
        return session;
    }

    @Override
    public List<ChatSession> listMine(String userId, int limit) {
        return chatSessionMapper.selectList(new LambdaQueryWrapper<ChatSession>()
                .eq(ChatSession::getUserId, empty(userId))
                .orderByDesc(ChatSession::getUpdatedAt)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public List<ChatSession> listMineByAgentVersion(String userId, long agentVersionId, int limit) {
        return chatSessionMapper.selectList(new LambdaQueryWrapper<ChatSession>()
                .eq(ChatSession::getUserId, empty(userId))
                .eq(ChatSession::getAgentVersionId, agentVersionId)
                .orderByDesc(ChatSession::getUpdatedAt)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public List<ChatSession> listMineByWorkflowVersion(String userId, long workflowVersionId, int limit) {
        return chatSessionMapper.selectList(new LambdaQueryWrapper<ChatSession>()
                .eq(ChatSession::getUserId, empty(userId))
                .eq(ChatSession::getWorkflowVersionId, workflowVersionId)
                .orderByDesc(ChatSession::getUpdatedAt)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public List<ChatSession> listByAgentVersion(long agentVersionId, int limit) {
        return chatSessionMapper.selectList(new LambdaQueryWrapper<ChatSession>()
                .eq(ChatSession::getAgentVersionId, agentVersionId)
                .orderByDesc(ChatSession::getUpdatedAt)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public List<ChatSession> listByAgentVersions(List<Long> agentVersionIds, int limit) {
        if (agentVersionIds == null || agentVersionIds.isEmpty()) {
            return Collections.emptyList();
        }
        return chatSessionMapper.selectList(new LambdaQueryWrapper<ChatSession>()
                .in(ChatSession::getAgentVersionId, agentVersionIds)
                .orderByDesc(ChatSession::getUpdatedAt)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public List<ChatSession> listByWorkflowVersion(long workflowVersionId, int limit) {
        return chatSessionMapper.selectList(new LambdaQueryWrapper<ChatSession>()
                .eq(ChatSession::getWorkflowVersionId, workflowVersionId)
                .orderByDesc(ChatSession::getUpdatedAt)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public List<ChatSession> listByWorkflowVersions(List<Long> workflowVersionIds, int limit) {
        if (workflowVersionIds == null || workflowVersionIds.isEmpty()) {
            return Collections.emptyList();
        }
        return chatSessionMapper.selectList(new LambdaQueryWrapper<ChatSession>()
                .in(ChatSession::getWorkflowVersionId, workflowVersionIds)
                .orderByDesc(ChatSession::getUpdatedAt)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public IPage<ChatSession> pageForAdminReview(AdminSessionPageQuery query) {
        long pageNo = query.getPage() <= 0 ? 1 : query.getPage();
        long pageSize = query.getPageSize() <= 0 ? 20 : Math.min(query.getPageSize(), 100);
        List<Long> agentVersionIds = query.getAgentVersionIds();
        List<Long> workflowVersionIds = query.getWorkflowVersionIds();
        boolean hasAgent = agentVersionIds != null && !agentVersionIds.isEmpty();
        boolean hasWorkflow = workflowVersionIds != null && !workflowVersionIds.isEmpty();

        LambdaQueryWrapper<ChatSession> wrapper = new LambdaQueryWrapper<>();
        // 版本维度均可空：空则全局分页（由 Controller 限制仅 admin）
        if (hasAgent) {
            wrapper.in(ChatSession::getAgentVersionId, agentVersionIds);
        } else if (hasWorkflow) {
            wrapper.in(ChatSession::getWorkflowVersionId, workflowVersionIds);
        }
        if (StringUtils.hasText(query.getUserId())) {
            wrapper.eq(ChatSession::getUserId, query.getUserId().trim());
        }
        if (query.getSessionId() != null && query.getSessionId() > 0) {
            wrapper.eq(ChatSession::getId, query.getSessionId());
        }
        if (StringUtils.hasText(query.getAgentkSessionId())) {
            wrapper.eq(ChatSession::getAgentkSessionId, query.getAgentkSessionId().trim());
        }
        if (!query.isIncludeOfflineBatch()) {
            // 默认排除离线跑数登记的会话（entry_source=offline_batch）
            wrapper.and(w -> w.isNull(ChatSession::getEntrySource)
                    .or()
                    .eq(ChatSession::getEntrySource, "")
                    .or()
                    .ne(ChatSession::getEntrySource, ENTRY_SOURCE_OFFLINE_BATCH));
        }
        if (StringUtils.hasText(query.getSessionKind())) {
            wrapper.eq(ChatSession::getSessionKind, query.getSessionKind().trim());
        }
        if (StringUtils.hasText(query.getStatus())) {
            wrapper.eq(ChatSession::getStatus, query.getStatus().trim());
        }
        if (StringUtils.hasText(query.getTitleKeyword())) {
            wrapper.like(ChatSession::getTitle, query.getTitleKeyword().trim());
        }
        if (query.getCreatedFrom() != null) {
            wrapper.ge(ChatSession::getCreatedAt, query.getCreatedFrom());
        }
        if (query.getCreatedTo() != null) {
            wrapper.le(ChatSession::getCreatedAt, query.getCreatedTo());
        }
        wrapper.orderByDesc(ChatSession::getUpdatedAt);
        IPage<ChatSession> page = chatSessionMapper.selectPage(new Page<>(pageNo, pageSize), wrapper);
        fillLatestExecutionIds(page.getRecords());
        return page;
    }

    /** 批量回填会话最近一次 agent-k executionId（aop_execution.agentk_execution_id）。 */
    private void fillLatestExecutionIds(List<ChatSession> sessions) {
        if (sessions == null || sessions.isEmpty()) {
            return;
        }
        List<Long> sessionIds = sessions.stream()
                .map(ChatSession::getId)
                .filter(Objects::nonNull)
                .distinct()
                .collect(Collectors.toList());
        if (sessionIds.isEmpty()) {
            return;
        }
        List<ChatRun> runs = chatRunMapper.selectList(new LambdaQueryWrapper<ChatRun>()
                .in(ChatRun::getSessionId, sessionIds)
                .isNotNull(ChatRun::getAgentkRunId)
                .ne(ChatRun::getAgentkRunId, "")
                .orderByDesc(ChatRun::getId));
        Map<Long, String> latestBySession = new HashMap<>();
        for (ChatRun run : runs) {
            if (run == null || run.getSessionId() == null || !StringUtils.hasText(run.getAgentkRunId())) {
                continue;
            }
            latestBySession.putIfAbsent(run.getSessionId(), run.getAgentkRunId().trim());
        }
        for (ChatSession session : sessions) {
            if (session == null || session.getId() == null) {
                continue;
            }
            String executionId = latestBySession.get(session.getId());
            if (StringUtils.hasText(executionId)) {
                session.setLastAgentkRunId(executionId);
                session.setLastExecutionId(executionId);
            }
        }
    }

    @Override
    public ChatSession requireSession(long sessionId) {
        ChatSession session = chatSessionMapper.selectById(sessionId);
        if (session == null) {
            throw new IllegalArgumentException("session not found: " + sessionId);
        }
        return session;
    }

    @Override
    public List<ChatMessage> listMessages(long sessionId, int limit) {
        return chatMessageMapper.selectList(new LambdaQueryWrapper<ChatMessage>()
                .eq(ChatMessage::getSessionId, sessionId)
                .orderByAsc(ChatMessage::getId)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public List<ChatMessage> listMessagesOwned(long sessionId, String userId, int limit) {
        requireOwnedSession(sessionId, userId);
        return listMessages(sessionId, limit);
    }

    @Override
    public List<ChatAsset> listAssetsOwned(String userId, String assetType, int limit) {
        LambdaQueryWrapper<ChatAsset> q = new LambdaQueryWrapper<ChatAsset>()
                .eq(ChatAsset::getUserId, empty(userId))
                .orderByDesc(ChatAsset::getId)
                .last("LIMIT " + Math.max(limit, 1));
        if (StringUtils.hasText(assetType)) {
            q.eq(ChatAsset::getAssetType, assetType.trim());
        }
        return chatAssetMapper.selectList(q);
    }

    @Override
    public List<ChatAsset> listAssetsBySession(long sessionId, int limit) {
        if (sessionId <= 0) {
            return new ArrayList<>();
        }
        List<ChatAsset> rows = chatAssetMapper.selectList(new LambdaQueryWrapper<ChatAsset>()
                .eq(ChatAsset::getSessionId, sessionId)
                .orderByAsc(ChatAsset::getCreatedAt)
                .orderByAsc(ChatAsset::getId)
                .last("LIMIT " + Math.max(limit, 1)));
        return rows == null ? new ArrayList<>() : rows;
    }

    @Override
    public IPage<ChatAsset> pageAssetsOwned(String userId, ChatAssetPageQuery query) {
        ChatAssetPageQuery q = query == null ? new ChatAssetPageQuery() : query;
        long pageNo = q.getPage() <= 0 ? 1 : q.getPage();
        long pageSize = q.getPageSize() <= 0 ? 20 : Math.min(q.getPageSize(), 100);
        LambdaQueryWrapper<ChatAsset> wrapper = new LambdaQueryWrapper<ChatAsset>()
                .eq(ChatAsset::getUserId, empty(userId))
                .eq(ChatAsset::getStatus, "synced")
                .orderByDesc(ChatAsset::getCreatedAt);
        applyAssetPageFilters(wrapper, q);
        return chatAssetMapper.selectPage(new Page<>(pageNo, pageSize), wrapper);
    }

    @Override
    public IPage<ChatAsset> pageAssets(ChatAssetPageQuery query) {
        ChatAssetPageQuery q = query == null ? new ChatAssetPageQuery() : query;
        long pageNo = q.getPage() <= 0 ? 1 : q.getPage();
        long pageSize = q.getPageSize() <= 0 ? 20 : Math.min(q.getPageSize(), 100);
        LambdaQueryWrapper<ChatAsset> wrapper = new LambdaQueryWrapper<ChatAsset>()
                .eq(ChatAsset::getStatus, "synced")
                .orderByDesc(ChatAsset::getCreatedAt);
        applyAssetPageFilters(wrapper, q);
        return chatAssetMapper.selectPage(new Page<>(pageNo, pageSize), wrapper);
    }

    @Override
    public List<ChatAsset> listAssetsForPortfolioGrouping(ChatAssetPageQuery query, int maxRows) {
        ChatAssetPageQuery q = query == null ? new ChatAssetPageQuery() : query;
        int limit = Math.max(1, Math.min(maxRows <= 0 ? 2000 : maxRows, 5000));
        LambdaQueryWrapper<ChatAsset> wrapper = new LambdaQueryWrapper<ChatAsset>()
                .eq(ChatAsset::getStatus, "synced")
                .orderByDesc(ChatAsset::getCreatedAt)
                .orderByDesc(ChatAsset::getId)
                .last("LIMIT " + limit);
        applyAssetPageFilters(wrapper, q);
        return chatAssetMapper.selectList(wrapper);
    }

    private void applyAssetPageFilters(LambdaQueryWrapper<ChatAsset> wrapper, ChatAssetPageQuery q) {
        wrapper.in(ChatAsset::getAssetType, "image", "video");
        applyExcludeFailedRunAssets(wrapper);
        if (StringUtils.hasText(q.getUserId())) {
            wrapper.eq(ChatAsset::getUserId, q.getUserId().trim());
        }
        if (StringUtils.hasText(q.getAssetType())) {
            wrapper.eq(ChatAsset::getAssetType, q.getAssetType().trim());
        }
        if (StringUtils.hasText(q.getSourceKind())) {
            wrapper.eq(ChatAsset::getSourceKind, q.getSourceKind().trim());
        }
        if (StringUtils.hasText(q.getSource())) {
            wrapper.eq(ChatAsset::getSource, ChatAssetSources.normalize(q.getSource()));
        }
        if (q.getSessionId() != null && q.getSessionId() > 0) {
            wrapper.eq(ChatAsset::getSessionId, q.getSessionId());
        }
        if (q.getRunId() != null && q.getRunId() > 0) {
            wrapper.eq(ChatAsset::getRunId, q.getRunId());
        }
        if (StringUtils.hasText(q.getAgentkRunId())) {
            wrapper.eq(ChatAsset::getAgentkRunId, q.getAgentkRunId().trim());
        }
        if (StringUtils.hasText(q.getWorkflowExecId())) {
            wrapper.eq(ChatAsset::getWorkflowExecId, q.getWorkflowExecId().trim());
        }
        if (StringUtils.hasText(q.getPromptKeyword())) {
            wrapper.like(ChatAsset::getPromptText, q.getPromptKeyword().trim());
        }
        // 智能体名筛选：精确匹配 aop_asset.agent_name
        if (StringUtils.hasText(q.getAgentName())) {
            wrapper.eq(ChatAsset::getAgentName, q.getAgentName().trim());
        }
        // Workflow 名筛选：仅 session_kind=workflow 的 aop_workflow.name
        if (StringUtils.hasText(q.getWorkflowName())) {
            List<Long> sessionIds = listSessionIdsByBoundWorkflowName(q.getWorkflowName());
            if (sessionIds.isEmpty()) {
                wrapper.eq(ChatAsset::getId, -1L);
            } else if (q.getSessionId() != null && q.getSessionId() > 0) {
                if (!sessionIds.contains(q.getSessionId())) {
                    wrapper.eq(ChatAsset::getId, -1L);
                }
            } else {
                wrapper.in(ChatAsset::getSessionId, sessionIds);
            }
        }
    }

    @Override
    public ChatAsset requireOwnedAsset(long assetId, String userId) {
        ChatAsset asset = chatAssetMapper.selectById(assetId);
        if (asset == null || !empty(userId).equals(empty(asset.getUserId()))) {
            throw new IllegalArgumentException("asset not found or not owned: " + assetId);
        }
        return asset;
    }

    @Override
    public ChatAsset requireAsset(long assetId) {
        ChatAsset asset = chatAssetMapper.selectById(assetId);
        if (asset == null || !"synced".equals(empty(asset.getStatus()))
                || !isPublishableAssetType(asset.getAssetType())
                || isAssetFromFailedRun(asset)) {
            throw new IllegalArgumentException("asset not found: " + assetId);
        }
        return asset;
    }

    @Override
    public Map<Long, ChatSession> mapSessionsByIds(List<Long> sessionIds) {
        if (sessionIds == null || sessionIds.isEmpty()) {
            return new HashMap<>();
        }
        List<ChatSession> sessions = chatSessionMapper.selectBatchIds(sessionIds);
        Map<Long, ChatSession> result = new LinkedHashMap<>();
        for (ChatSession session : sessions) {
            if (session != null && session.getId() != null) {
                result.put(session.getId(), session);
            }
        }
        return result;
    }

    @Override
    public Optional<ChatRun> findRun(long runId) {
        if (runId <= 0) {
            return Optional.empty();
        }
        return Optional.ofNullable(chatRunMapper.selectById(runId));
    }

    @Override
    public List<ChatAsset> listAssetsInSameRound(ChatAsset anchor) {
        if (anchor == null || anchor.getSessionId() == null) {
            return Collections.emptyList();
        }
        LambdaQueryWrapper<ChatAsset> wrapper = new LambdaQueryWrapper<ChatAsset>()
                .eq(ChatAsset::getStatus, "synced")
                .eq(ChatAsset::getSessionId, anchor.getSessionId())
                .in(ChatAsset::getAssetType, "image", "video");
        applyExcludeFailedRunAssets(wrapper);
        wrapper.orderByAsc(ChatAsset::getAgentkEventSeq)
                .orderByAsc(ChatAsset::getId);
        if (ChatAssetPortfolioService.groupsByWorkflowExec(anchor)) {
            wrapper.eq(ChatAsset::getSourceKind, anchor.getSourceKind().trim())
                    .eq(ChatAsset::getWorkflowExecId, anchor.getWorkflowExecId().trim());
        } else if (StringUtils.hasText(anchor.getAgentkRunId())) {
            wrapper.eq(ChatAsset::getAgentkRunId, anchor.getAgentkRunId().trim());
        } else if (anchor.getRunId() != null && anchor.getRunId() > 0) {
            wrapper.eq(ChatAsset::getRunId, anchor.getRunId());
        } else {
            wrapper.eq(ChatAsset::getId, anchor.getId());
        }
        return chatAssetMapper.selectList(wrapper);
    }


    /** 优先用写入方显式 agentName；pipeline 回落 workflow 名；否则从 session.agent_version_id 反查。 */
    private String resolveAssetAgentName(SyncedAsset asset, ChatSession session) {
        if (asset != null && StringUtils.hasText(asset.getAgentName())) {
            return asset.getAgentName().trim();
        }
        if (asset != null && "pipeline".equals(empty(asset.getSourceKind()))) {
            String workflowName = resolveSessionWorkflowName(session);
            if (StringUtils.hasText(workflowName)) {
                return workflowName.trim();
            }
        }
        return resolveSessionAgentName(session);
    }

    @Override
    public String resolveSessionAgentName(ChatSession session) {
        if (session == null) {
            return "";
        }
        return lookupAgentNameByVersionId(session.getAgentVersionId());
    }

    @Override
    public String resolveSessionWorkflowName(ChatSession session) {
        if (session == null) {
            return "";
        }
        if (!ChatSessionKinds.WORKFLOW.equals(ChatSessionKinds.normalize(session.getSessionKind()))) {
            return "";
        }
        return lookupWorkflowNameByVersionId(session.getWorkflowVersionId());
    }

    @Override
    public List<Long> listSessionIdsByBoundAgentName(String agentName) {
        if (!StringUtils.hasText(agentName)) {
            return Collections.emptyList();
        }
        String name = agentName.trim();
        List<Agent> agents = agentMapper.selectList(new LambdaQueryWrapper<Agent>()
                .eq(Agent::getName, name));
        if (agents.isEmpty()) {
            return Collections.emptyList();
        }
        List<Long> agentIds = agents.stream()
                .map(Agent::getId)
                .filter(Objects::nonNull)
                .collect(Collectors.toList());
        if (agentIds.isEmpty()) {
            return Collections.emptyList();
        }
        List<AgentVersion> versions = agentVersionMapper.selectList(new LambdaQueryWrapper<AgentVersion>()
                .in(AgentVersion::getAgentId, agentIds)
                .select(AgentVersion::getId));
        Set<Long> agentVersionIds = new HashSet<>();
        for (AgentVersion version : versions) {
            if (version != null && version.getId() != null) {
                agentVersionIds.add(version.getId());
            }
        }
        if (agentVersionIds.isEmpty()) {
            return Collections.emptyList();
        }
        List<ChatSession> sessions = chatSessionMapper.selectList(new LambdaQueryWrapper<ChatSession>()
                .in(ChatSession::getAgentVersionId, agentVersionIds)
                // agentName 筛选不包含 workflow 入口；workflow 用 workflowName
                .and(w -> w.isNull(ChatSession::getSessionKind)
                        .or()
                        .ne(ChatSession::getSessionKind, ChatSessionKinds.WORKFLOW))
                .select(ChatSession::getId));
        List<Long> ids = new ArrayList<>(sessions.size());
        for (ChatSession session : sessions) {
            if (session != null && session.getId() != null) {
                ids.add(session.getId());
            }
        }
        return ids;
    }

    @Override
    public List<Long> listSessionIdsByBoundWorkflowName(String workflowName) {
        if (!StringUtils.hasText(workflowName)) {
            return Collections.emptyList();
        }
        String name = workflowName.trim();
        List<Workflow> workflows = workflowMapper.selectList(new LambdaQueryWrapper<Workflow>()
                .eq(Workflow::getName, name));
        if (workflows.isEmpty()) {
            return Collections.emptyList();
        }
        List<Long> workflowIds = workflows.stream()
                .map(Workflow::getId)
                .filter(Objects::nonNull)
                .collect(Collectors.toList());
        if (workflowIds.isEmpty()) {
            return Collections.emptyList();
        }
        List<WorkflowVersion> versions = workflowVersionMapper.selectList(new LambdaQueryWrapper<WorkflowVersion>()
                .in(WorkflowVersion::getWorkflowId, workflowIds)
                .select(WorkflowVersion::getId));
        Set<Long> workflowVersionIds = new HashSet<>();
        for (WorkflowVersion version : versions) {
            if (version != null && version.getId() != null) {
                workflowVersionIds.add(version.getId());
            }
        }
        if (workflowVersionIds.isEmpty()) {
            return Collections.emptyList();
        }
        List<ChatSession> sessions = chatSessionMapper.selectList(new LambdaQueryWrapper<ChatSession>()
                .eq(ChatSession::getSessionKind, ChatSessionKinds.WORKFLOW)
                .in(ChatSession::getWorkflowVersionId, workflowVersionIds)
                .select(ChatSession::getId));
        List<Long> ids = new ArrayList<>(sessions.size());
        for (ChatSession session : sessions) {
            if (session != null && session.getId() != null) {
                ids.add(session.getId());
            }
        }
        return ids;
    }

    @Override
    public List<String> listBoundAgentNames(String userId, boolean publicOnly) {
        return distinctNames(chatAssetMapper.listDistinctBoundAgentNames(
                StringUtils.hasText(userId) ? userId.trim() : null, publicOnly));
    }

    @Override
    public List<String> listBoundWorkflowNames(String userId) {
        return distinctNames(chatAssetMapper.listDistinctBoundWorkflowNames(
                StringUtils.hasText(userId) ? userId.trim() : null));
    }

    private static List<String> distinctNames(List<String> names) {
        if (names == null || names.isEmpty()) {
            return Collections.emptyList();
        }
        List<String> result = new ArrayList<>(names.size());
        Set<String> seen = new HashSet<>();
        for (String name : names) {
            if (!StringUtils.hasText(name)) {
                continue;
            }
            String trimmed = name.trim();
            if (seen.add(trimmed)) {
                result.add(trimmed);
            }
        }
        return result;
    }

    private String lookupAgentNameByVersionId(Long agentVersionId) {
        if (agentVersionId == null || agentVersionId <= 0) {
            return "";
        }
        AgentVersion version = agentVersionMapper.selectById(agentVersionId);
        if (version == null || version.getAgentId() == null) {
            return "";
        }
        Agent agent = agentMapper.selectById(version.getAgentId());
        return agent == null ? "" : empty(agent.getName());
    }

    private String lookupWorkflowNameByVersionId(Long workflowVersionId) {
        if (workflowVersionId == null || workflowVersionId <= 0) {
            return "";
        }
        WorkflowVersion version = workflowVersionMapper.selectById(workflowVersionId);
        if (version == null || version.getWorkflowId() == null) {
            return "";
        }
        Workflow workflow = workflowMapper.selectById(version.getWorkflowId());
        return workflow == null ? "" : empty(workflow.getName());
    }

    @Override
    public List<ChatSession> listSessionsForAssetBackfill(String userId, int limit, int offset) {
        int lim = Math.max(limit, 1);
        int off = Math.max(offset, 0);
        return chatSessionMapper.selectList(new LambdaQueryWrapper<ChatSession>()
                .eq(ChatSession::getUserId, empty(userId))
                .ne(ChatSession::getAgentkSessionId, "")
                .isNotNull(ChatSession::getAgentkSessionId)
                .orderByDesc(ChatSession::getUpdatedAt)
                .last("LIMIT " + lim + " OFFSET " + off));
    }

    @Override
    public List<ChatSession> listSessionsForGlobalAssetBackfill(int limit, int offset) {
        int lim = Math.max(limit, 1);
        int off = Math.max(offset, 0);
        return chatSessionMapper.selectList(new LambdaQueryWrapper<ChatSession>()
                .ne(ChatSession::getAgentkSessionId, "")
                .isNotNull(ChatSession::getAgentkSessionId)
                .orderByDesc(ChatSession::getUpdatedAt)
                .last("LIMIT " + lim + " OFFSET " + off));
    }

    @Override
    public List<ChatRun> listRuns(long sessionId, int limit) {
        return chatRunMapper.selectList(new LambdaQueryWrapper<ChatRun>()
                .eq(ChatRun::getSessionId, sessionId)
                .orderByDesc(ChatRun::getId)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public List<ChatRun> listRunsOwned(long sessionId, String userId, int limit) {
        requireOwnedSession(sessionId, userId);
        return listRuns(sessionId, limit);
    }

    @Override
    public ChatRun requireRun(long runId) {
        ChatRun run = chatRunMapper.selectById(runId);
        if (run == null) {
            throw new IllegalArgumentException("run not found: " + runId);
        }
        return run;
    }

    @Override
    public ChatRun requireRunByAgentkId(String agentkRunId) {
        return findRunByAgentkId(agentkRunId)
                .orElseThrow(() -> new IllegalArgumentException("run not found: " + agentkRunId));
    }

    @Override
    public Set<String> listFailedAgentkRunIds(Collection<String> agentkRunIds) {
        if (agentkRunIds == null || agentkRunIds.isEmpty()) {
            return Collections.emptySet();
        }
        List<String> ids = agentkRunIds.stream()
                .filter(StringUtils::hasText)
                .map(String::trim)
                .distinct()
                .collect(Collectors.toList());
        if (ids.isEmpty()) {
            return Collections.emptySet();
        }
        List<ChatRun> rows = chatRunMapper.selectList(new LambdaQueryWrapper<ChatRun>()
                .in(ChatRun::getAgentkRunId, ids)
                .eq(ChatRun::getStatus, "failed"));
        if (rows == null || rows.isEmpty()) {
            return Collections.emptySet();
        }
        Set<String> failed = new HashSet<>();
        for (ChatRun run : rows) {
            if (run != null && StringUtils.hasText(run.getAgentkRunId())) {
                failed.add(run.getAgentkRunId().trim());
            }
        }
        return failed;
    }

    @Override
    public List<RunEvent> listRunEvents(long runId, int limit) {
        return runEventMapper.selectList(new LambdaQueryWrapper<RunEvent>()
                .eq(RunEvent::getRunId, runId)
                .orderByAsc(RunEvent::getSeq)
                .orderByAsc(RunEvent::getId)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public List<RunEvent> listSessionEvents(long sessionId, String agentkSessionId, int limit) {
        LambdaQueryWrapper<RunEvent> q = new LambdaQueryWrapper<RunEvent>();
        if (sessionId > 0 && StringUtils.hasText(agentkSessionId)) {
            q.and(w -> w.eq(RunEvent::getSessionId, sessionId)
                    .or()
                    .eq(RunEvent::getAgentkSessionId, agentkSessionId.trim()));
        } else if (sessionId > 0) {
            q.eq(RunEvent::getSessionId, sessionId);
        } else if (StringUtils.hasText(agentkSessionId)) {
            q.eq(RunEvent::getAgentkSessionId, agentkSessionId.trim());
        } else {
            return new ArrayList<>();
        }
        List<RunEvent> rows = runEventMapper.selectList(q
                .orderByAsc(RunEvent::getSeq)
                .orderByAsc(RunEvent::getId)
                .last("LIMIT " + Math.max(limit, 1)));
        return rows == null ? new ArrayList<>() : rows;
    }

    @Override
    public IPage<RunEvent> pageSessionEventsByAgentk(String agentkSessionId, long page, long pageSize) {
        long pageNo = page <= 0 ? 1 : page;
        long size = pageSize <= 0 ? 20 : Math.min(pageSize, 100);
        if (!StringUtils.hasText(agentkSessionId)) {
            return new Page<>(pageNo, size, 0);
        }
        return runEventMapper.selectPage(new Page<>(pageNo, size), new LambdaQueryWrapper<RunEvent>()
                .eq(RunEvent::getAgentkSessionId, agentkSessionId.trim())
                .orderByDesc(RunEvent::getSeq)
                .orderByDesc(RunEvent::getId));
    }

    @Override
    public IPage<RunEvent> pageSessionEventsInWriteOrder(String agentkSessionId, long page, long pageSize) {
        return pageSessionEventsInWriteOrder(agentkSessionId, null, page, pageSize);
    }

    @Override
    public IPage<RunEvent> pageSessionEventsInWriteOrder(String agentkSessionId, String agentkRunId,
            long page, long pageSize) {
        long pageNo = page <= 0 ? 1 : page;
        long size = pageSize <= 0 ? 200 : Math.min(pageSize, 2000);
        if (!StringUtils.hasText(agentkSessionId)) {
            return new Page<>(pageNo, size, 0);
        }
        LambdaQueryWrapper<RunEvent> query = new LambdaQueryWrapper<RunEvent>()
                .eq(RunEvent::getAgentkSessionId, agentkSessionId.trim());
        if (StringUtils.hasText(agentkRunId)) {
            query.eq(RunEvent::getAgentkRunId, agentkRunId.trim());
        }
        return runEventMapper.selectPage(new Page<>(pageNo, size), query.orderByAsc(RunEvent::getId));
    }

    @Override
    public List<AgentkCallLog> listCallLogsBySession(long sessionId, int limit) {
        ChatSession session = requireSession(sessionId);
        LambdaQueryWrapper<AgentkCallLog> q = new LambdaQueryWrapper<AgentkCallLog>()
                .orderByDesc(AgentkCallLog::getId)
                .last("LIMIT " + Math.max(limit, 1));
        if (StringUtils.hasText(session.getAgentkSessionId())) {
            q.and(w -> w.eq(AgentkCallLog::getAgentkSessionId, session.getAgentkSessionId())
                    .or(inner -> inner.eq(AgentkCallLog::getBizType, "chat_session")
                            .eq(AgentkCallLog::getBizId, session.getId())));
        } else {
            q.eq(AgentkCallLog::getBizType, "chat_session").eq(AgentkCallLog::getBizId, session.getId());
        }
        return callLogMapper.selectList(q);
    }

    @Override
    public ChatRun requireOwnedRun(long runId, String userId) {
        ChatRun run = chatRunMapper.selectById(runId);
        if (run == null) {
            throw new IllegalArgumentException("run not found: " + runId);
        }
        requireOwnedSession(run.getSessionId(), userId);
        return run;
    }

    @Override
    public ChatRun requireOwnedRunByAgentkId(String agentkRunId, String userId) {
        ChatRun run = findRunByAgentkId(agentkRunId)
                .orElseThrow(() -> new IllegalArgumentException("run not found: " + agentkRunId));
        requireOwnedSession(run.getSessionId(), userId);
        return run;
    }

    @Override
    public List<RunEvent> listRunEventsOwned(long runId, String userId, int limit) {
        ChatRun run = requireOwnedRun(runId, userId);
        return runEventMapper.selectList(new LambdaQueryWrapper<RunEvent>()
                .eq(RunEvent::getRunId, run.getId())
                .orderByAsc(RunEvent::getSeq)
                .orderByAsc(RunEvent::getId)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public List<RunEvent> listRunEventsByAgentkRunOwned(String agentkRunId, String userId, int limit) {
        ChatRun run = requireOwnedRunByAgentkId(agentkRunId, userId);
        return runEventMapper.selectList(new LambdaQueryWrapper<RunEvent>()
                .eq(RunEvent::getRunId, run.getId())
                .orderByAsc(RunEvent::getSeq)
                .orderByAsc(RunEvent::getId)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public List<AgentkCallLog> listCallLogsMine(String userId, int limit) {
        return callLogMapper.selectList(new LambdaQueryWrapper<AgentkCallLog>()
                .eq(AgentkCallLog::getUserId, empty(userId))
                .orderByDesc(AgentkCallLog::getId)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public List<AgentkCallLog> listCallLogsBySessionOwned(long sessionId, String userId, int limit) {
        ChatSession session = requireOwnedSession(sessionId, userId);
        LambdaQueryWrapper<AgentkCallLog> q = new LambdaQueryWrapper<AgentkCallLog>()
                .orderByDesc(AgentkCallLog::getId)
                .last("LIMIT " + Math.max(limit, 1));
        if (StringUtils.hasText(session.getAgentkSessionId())) {
            q.and(w -> w.eq(AgentkCallLog::getAgentkSessionId, session.getAgentkSessionId())
                    .or(inner -> inner.eq(AgentkCallLog::getBizType, "chat_session")
                            .eq(AgentkCallLog::getBizId, session.getId())));
        } else {
            q.eq(AgentkCallLog::getBizType, "chat_session").eq(AgentkCallLog::getBizId, session.getId());
        }
        return callLogMapper.selectList(q);
    }

    private void assertOwner(ChatSession session, String userId) {
        if (!Objects.equals(empty(session.getUserId()), empty(userId))) {
            throw new PlatformAccessException("session not owned by current user");
        }
    }

    @Override
    public void persistRunEvent(String agentkSessionId, String agentkRunId, int seq, String kind,
            JsonNode payload, String agentkCreatedAt) {
        persistRunEvent(agentkSessionId, agentkRunId, seq, kind, payload, agentkCreatedAt, null, null, null);
    }

    @Override
    public void persistRunEvent(String agentkSessionId, String agentkRunId, int seq, String kind,
            JsonNode payload, String agentkCreatedAt, String roundId, String agentkEventId,
            String eventExecutionId) {
        persistRunEvent(agentkSessionId, agentkRunId, seq, kind, payload, agentkCreatedAt, roundId,
                agentkEventId, eventExecutionId, null);
    }

    @Override
    public void persistRunEvent(String agentkSessionId, String agentkRunId, int seq, String kind,
            JsonNode payload, String agentkCreatedAt, String roundId, String agentkEventId,
            String eventExecutionId, String entrySource) {
        if (!StringUtils.hasText(kind)) {
            return;
        }
        String executionId = StringUtils.hasText(eventExecutionId) ? eventExecutionId.trim()
                : (StringUtils.hasText(agentkRunId) ? agentkRunId.trim() : null);
        Optional<ChatRun> runOpt = StringUtils.hasText(executionId)
                ? findRunByAgentkId(executionId) : Optional.empty();
        ChatSession session = null;
        if (runOpt.isPresent()) {
            session = chatSessionMapper.selectById(runOpt.get().getSessionId());
        }
        if (session == null && StringUtils.hasText(agentkSessionId)) {
            session = findByAgentkSessionId(agentkSessionId).orElse(null);
        }
        if (session == null) {
            return;
        }
        ChatRun run = runOpt.orElse(null);
        String resolvedEntrySource = ChatEntrySources.resolve(entrySource, session.getEntrySource());
        String sessionKey = empty(StringUtils.hasText(agentkSessionId)
                ? agentkSessionId : session.getAgentkSessionId());
        String eventId = blankToNull(agentkEventId);
        RunEvent existing;
        if (eventId != null) {
            existing = runEventMapper.selectOne(new LambdaQueryWrapper<RunEvent>()
                    .eq(RunEvent::getAgentkSessionId, sessionKey)
                    .eq(RunEvent::getAgentkEventId, eventId)
                    .last("LIMIT 1"));
        } else if (seq > 0) {
            existing = runEventMapper.selectOne(new LambdaQueryWrapper<RunEvent>()
                    .eq(RunEvent::getAgentkSessionId, sessionKey)
                    .eq(RunEvent::getSeq, seq)
                    .last("LIMIT 1"));
        } else {
            existing = null;
        }
        if (existing == null) {
            insertRunEventWithSeqRetry(session, run, sessionKey, executionId, eventId, roundId,
                    seq, kind, payload, agentkCreatedAt, resolvedEntrySource);
        } else {
            boolean dirty = false;
            if (!StringUtils.hasText(existing.getAgentkEventId()) && StringUtils.hasText(agentkEventId)) {
                existing.setAgentkEventId(agentkEventId.trim());
                dirty = true;
            }
            if (!StringUtils.hasText(existing.getRoundId()) && StringUtils.hasText(roundId)) {
                existing.setRoundId(roundId.trim());
                dirty = true;
            }
            if (!StringUtils.hasText(existing.getAgentkRunId()) && StringUtils.hasText(executionId)) {
                existing.setAgentkRunId(executionId);
                dirty = true;
            }
            if ((existing.getRunId() == null || existing.getRunId() == 0L) && run != null) {
                existing.setRunId(run.getId());
                dirty = true;
            }
            // 只补空，不覆盖：入口在会话建成后不会变，重写只会白白产生更新
            if (!StringUtils.hasText(existing.getEntrySource())) {
                existing.setEntrySource(resolvedEntrySource);
                dirty = true;
            }
            String payloadJson = payload == null ? null : safeJson(payload);
            if (!Objects.equals(existing.getKind(), kind)) {
                existing.setKind(kind);
                dirty = true;
            }
            if (!Objects.equals(existing.getPayloadJson(), payloadJson)) {
                existing.setPayloadJson(payloadJson);
                dirty = true;
            }
            if (!Objects.equals(existing.getAgentkCreatedAt(), empty(agentkCreatedAt))) {
                existing.setAgentkCreatedAt(empty(agentkCreatedAt));
                dirty = true;
            }
            if (seq > 0 && !Objects.equals(existing.getSeq(), seq)) {
                existing.setSeq(seq);
                dirty = true;
            }
            if (dirty) {
                runEventMapper.updateById(existing);
            }
        }
        if (run != null && ("round.started".equals(kind) || "run.started".equals(kind)) && payload != null) {
            applyRunStarted(session, run, payload, roundId);
        } else if (run != null && StringUtils.hasText(roundId)
                && !roundId.trim().equals(empty(run.getLastRoundId()))) {
            run.setLastRoundId(roundId.trim());
            chatRunMapper.updateById(run);
        }
    }

    private static String blankToNull(String value) {
        return StringUtils.hasText(value) ? value.trim() : null;
    }

    /** SDK 无原生 seq 时，按会话已落库最大序号 +1；插入撞唯一键时由调用方重试。 */
    private int nextSessionSeq(String agentkSessionId) {
        RunEvent latest = runEventMapper.selectOne(new LambdaQueryWrapper<RunEvent>()
                .eq(RunEvent::getAgentkSessionId, empty(agentkSessionId))
                .orderByDesc(RunEvent::getSeq)
                .orderByDesc(RunEvent::getId)
                .last("LIMIT 1"));
        int max = latest == null || latest.getSeq() == null ? 0 : latest.getSeq();
        return Math.max(max, 0) + 1;
    }

    /**
     * seq 未由上游给出时用 max+1；并发下可能撞 {@code (agentk_session_id, seq)} 唯一键，捕获后重取序号再插。
     * 同一 {@code agentk_event_id} 被并发插入则直接跳过，避免重试生成重复事件。
     */
    private void insertRunEventWithSeqRetry(ChatSession session, ChatRun run, String sessionKey,
            String executionId, String eventId, String roundId, int seq, String kind,
            JsonNode payload, String agentkCreatedAt, String entrySource) {
        boolean generateSeq = seq <= 0;
        int persistSeq = generateSeq ? nextSessionSeq(sessionKey) : seq;
        for (int attempt = 1; attempt <= MAX_SESSION_SEQ_RETRY; attempt++) {
            RunEvent event = new RunEvent();
            event.setSessionId(session.getId());
            event.setRunId(run == null ? 0L : run.getId());
            event.setAgentkRunId(executionId);
            event.setAgentkSessionId(sessionKey);
            event.setAgentkEventId(eventId);
            event.setRoundId(empty(roundId));
            event.setSeq(persistSeq);
            event.setKind(kind);
            event.setPayloadJson(payload == null ? null : safeJson(payload));
            event.setCreatedAt(LocalDateTime.now());
            event.setAgentkCreatedAt(empty(agentkCreatedAt));
            event.setEntrySource(ChatEntrySources.resolve(entrySource, null));
            try {
                runEventMapper.insert(event);
                return;
            } catch (Exception e) {
                if (!isDuplicateKey(e)) {
                    LOGGER.warn("insertRunEvent skipped sessionId={} seq={} eventId={} reason={}",
                            sessionKey, persistSeq, eventId, e.toString());
                    return;
                }
                if (eventId != null) {
                    LOGGER.info("insertRunEvent duplicate eventId sessionId={} eventId={}",
                            sessionKey, eventId);
                    return;
                }
                if (!generateSeq || attempt >= MAX_SESSION_SEQ_RETRY) {
                    LOGGER.warn("insertRunEvent duplicate seq sessionId={} seq={} attempts={}",
                            sessionKey, persistSeq, attempt);
                    return;
                }
                persistSeq = nextSessionSeq(sessionKey);
            }
        }
    }

    private static boolean isDuplicateKey(Throwable error) {
        for (Throwable t = error; t != null; t = t.getCause()) {
            if (t instanceof DuplicateKeyException
                    || t instanceof SQLIntegrityConstraintViolationException) {
                return true;
            }
            String message = t.getMessage();
            if (message != null) {
                String lower = message.toLowerCase();
                if (lower.contains("duplicate entry") || lower.contains("duplicate key")) {
                    return true;
                }
            }
        }
        return false;
    }

    @Override
    public long countOfflineBatchEvents(List<String> agentkSessionIds) {
        List<String> sessionKeys = normalizeSessionKeys(agentkSessionIds);
        if (sessionKeys.isEmpty()) {
            return 0L;
        }
        // 本仓库 MP 版本 selectCount 返回 Integer
        Integer count = runEventMapper.selectCount(new LambdaQueryWrapper<RunEvent>()
                .in(RunEvent::getAgentkSessionId, sessionKeys)
                .eq(RunEvent::getEntrySource, ChatEntrySources.OFFLINE_BATCH));
        return count == null ? 0L : count.longValue();
    }

    @Override
    @Transactional
    public int purgeOfflineBatchPersisted(String jobBizId, List<String> agentkSessionIds, int batchSize) {
        int limit = batchSize <= 0 ? DEFAULT_PURGE_BATCH_SIZE : Math.min(batchSize, MAX_PURGE_BATCH_SIZE);
        List<String> sessionKeys = normalizeSessionKeys(agentkSessionIds);

        // 事件是量级最大的一步：单独先删干净，每次调用总量卡在 limit 行内，避免长事务持锁
        int eventsDeleted = 0;
        int remaining = limit;
        for (String key : sessionKeys) {
            if (remaining <= 0) {
                break;
            }
            int deleted = runEventMapper.delete(new LambdaQueryWrapper<RunEvent>()
                    .eq(RunEvent::getAgentkSessionId, key)
                    .eq(RunEvent::getEntrySource, ChatEntrySources.OFFLINE_BATCH)
                    .last("LIMIT " + remaining));
            eventsDeleted += deleted;
            remaining -= deleted;
        }
        if (eventsDeleted > 0) {
            return eventsDeleted;
        }

        // 事件清空后再动产物 / 调用日志 / 会话：都是每 cell 个位到几十行，一批即可删完
        int deleted = 0;
        if (!sessionKeys.isEmpty()) {
            deleted += chatAssetMapper.delete(new LambdaQueryWrapper<ChatAsset>()
                    .in(ChatAsset::getAgentkSessionId, sessionKeys)
                    .eq(ChatAsset::getSource, ChatAssetSources.OFFLINE));
            deleted += callLogMapper.delete(new LambdaQueryWrapper<AgentkCallLog>()
                    .in(AgentkCallLog::getAgentkSessionId, sessionKeys));
        }
        List<Long> sessionIds = resolveOfflineSessionIds(jobBizId, sessionKeys);
        if (sessionIds.isEmpty()) {
            return deleted;
        }
        deleted += chatAssetMapper.delete(new LambdaQueryWrapper<ChatAsset>()
                .in(ChatAsset::getSessionId, sessionIds));
        deleted += callLogMapper.delete(new LambdaQueryWrapper<AgentkCallLog>()
                .eq(AgentkCallLog::getBizType, "chat_session")
                .in(AgentkCallLog::getBizId, sessionIds));
        deleted += chatSessionMapper.delete(new LambdaQueryWrapper<ChatSession>()
                .in(ChatSession::getId, sessionIds));
        return deleted;
    }

    /** 只认离线入口会话：按 cell 的 agent-k sessionId，外加标题前缀为 {@code jobBizId/} 的漏网会话。 */
    private List<Long> resolveOfflineSessionIds(String jobBizId, List<String> sessionKeys) {
        LinkedHashMap<Long, ChatSession> sessions = new LinkedHashMap<>();
        if (!sessionKeys.isEmpty()) {
            rememberSessions(sessions, chatSessionMapper.selectList(new LambdaQueryWrapper<ChatSession>()
                    .eq(ChatSession::getEntrySource, ENTRY_SOURCE_OFFLINE_BATCH)
                    .in(ChatSession::getAgentkSessionId, sessionKeys)));
        }
        if (StringUtils.hasText(jobBizId)) {
            rememberSessions(sessions, chatSessionMapper.selectList(new LambdaQueryWrapper<ChatSession>()
                    .eq(ChatSession::getEntrySource, ENTRY_SOURCE_OFFLINE_BATCH)
                    .likeRight(ChatSession::getTitle, jobBizId.trim() + "/")));
        }
        return new ArrayList<>(sessions.keySet());
    }

    private static List<String> normalizeSessionKeys(List<String> agentkSessionIds) {
        List<String> sessionKeys = new ArrayList<>();
        if (agentkSessionIds == null) {
            return sessionKeys;
        }
        for (String id : agentkSessionIds) {
            if (StringUtils.hasText(id) && !sessionKeys.contains(id.trim())) {
                sessionKeys.add(id.trim());
            }
        }
        return sessionKeys;
    }

    private static void rememberSessions(LinkedHashMap<Long, ChatSession> into, List<ChatSession> found) {
        if (found == null) {
            return;
        }
        for (ChatSession session : found) {
            if (session != null && session.getId() != null) {
                into.putIfAbsent(session.getId(), session);
            }
        }
    }

    @Override
    public void upsertAssets(String agentkSessionId, String agentkRunId, List<SyncedAsset> assets) {
        if (assets == null || assets.isEmpty() || !StringUtils.hasText(agentkSessionId)) {
            return;
        }
        Optional<ChatSession> sessionOpt = findByAgentkSessionId(agentkSessionId);
        if (!sessionOpt.isPresent()) {
            return;
        }
        ChatSession session = sessionOpt.get();
        ChatRun run = StringUtils.hasText(agentkRunId) ? findRunByAgentkId(agentkRunId).orElse(null) : null;
        LocalDateTime now = LocalDateTime.now();
        Set<String> seenInBatch = new HashSet<>();
        List<ChatAsset> existingRows = loadSessionAssets(session.getId());
        for (SyncedAsset asset : assets) {
            if (asset == null || !isPublishableAssetType(asset.getAssetType())
                    || !StringUtils.hasText(asset.getAssetUrl())) {
                continue;
            }
            String batchKey = assetUniqueKey(session.getId(), asset);
            if (!seenInBatch.add(batchKey)) {
                continue;
            }
            try {
                upsertOneAsset(session, run, asset, agentkSessionId, agentkRunId, now, existingRows);
            } catch (RuntimeException e) {
                if (!isDuplicateKeyException(e)) {
                    throw e;
                }
                LOGGER.warn("skip duplicate chat asset sessionId={} sourceKind={} urlPrefix={}",
                        session.getId(), empty(asset.getSourceKind()), urlUniquePrefix(asset.getAssetUrl()));
            }
        }
    }

    @Override
    public int upsertStandaloneAssets(String userId, String tenantId, long sessionId,
            String agentkSessionId, List<SyncedAsset> assets) {
        if (assets == null || assets.isEmpty() || sessionId <= 0) {
            return 0;
        }
        LocalDateTime now = LocalDateTime.now();
        String owner = empty(userId);
        String tenant = empty(tenantId);
        String sessionKey = empty(agentkSessionId);
        Set<String> seenInBatch = new HashSet<>();
        int written = 0;
        List<ChatAsset> existingRows = loadSessionAssets(sessionId);
        for (SyncedAsset asset : assets) {
            if (asset == null || !isPublishableAssetType(asset.getAssetType())
                    || !StringUtils.hasText(asset.getAssetUrl())) {
                continue;
            }
            String batchKey = assetUniqueKey(sessionId, asset);
            if (!seenInBatch.add(batchKey)) {
                continue;
            }
            try {
                if (upsertOneStandaloneAsset(owner, tenant, sessionId, sessionKey, asset, now, existingRows)) {
                    written++;
                }
            } catch (RuntimeException e) {
                if (!isDuplicateKeyException(e)) {
                    throw e;
                }
                LOGGER.warn("skip duplicate standalone chat asset sessionId={} sourceKind={} urlPrefix={}",
                        sessionId, empty(asset.getSourceKind()), urlUniquePrefix(asset.getAssetUrl()));
            }
        }
        return written;
    }

    private boolean upsertOneStandaloneAsset(String userId, String tenantId, long sessionId,
            String agentkSessionId, SyncedAsset asset, LocalDateTime now, List<ChatAsset> existingRows) {
        ChatAsset existing = findExistingAsset(sessionId, asset, existingRows);
        if (existing == null) {
            ChatAsset row = new ChatAsset();
            row.setPublicId(PlatformIds.newPublicId("ast_"));
            row.setSessionId(sessionId);
            row.setRunId(null);
            row.setMessageId(null);
            row.setUserId(userId);
            row.setAssetType(asset.getAssetType().trim());
            row.setAssetUrl(asset.getAssetUrl().trim());
            row.setPromptText(empty(asset.getPromptText()));
            row.setSourceKind(empty(asset.getSourceKind()));
            row.setSource(ChatAssetSources.normalize(asset.getSource()));
            row.setSourceName(empty(asset.getSourceName()));
            row.setAgentName(resolveStandaloneAgentName(asset));
            row.setAgentkEventSeq(asset.getSeq());
            row.setRoundId(empty(asset.getRoundId()));
            row.setAgentkSessionId(blankToNull(agentkSessionId));
            row.setAgentkRunId(null);
            row.setWorkflowExecId(blankToNull(asset.getWorkflowExecId()));
            row.setStatus("synced");
            row.setCreatedAt(now);
            row.setUpdatedAt(now);
            try {
                chatAssetMapper.insert(row);
                existingRows.add(row);
                return true;
            } catch (RuntimeException duplicate) {
                if (!isDuplicateKeyException(duplicate)) {
                    throw duplicate;
                }
                ChatAsset duplicateRow = queryExistingAsset(sessionId, asset);
                if (duplicateRow != null) {
                    mergeStandaloneAssetFields(duplicateRow, asset, agentkSessionId, now);
                    chatAssetMapper.updateById(duplicateRow);
                    existingRows.add(duplicateRow);
                    return true;
                }
                throw duplicate;
            }
        }
        mergeStandaloneAssetFields(existing, asset, agentkSessionId, now);
        chatAssetMapper.updateById(existing);
        return true;
    }

    private void mergeStandaloneAssetFields(ChatAsset existing, SyncedAsset asset,
            String agentkSessionId, LocalDateTime now) {
        if (StringUtils.hasText(agentkSessionId)) {
            existing.setAgentkSessionId(agentkSessionId);
        }
        existing.setPromptText(preferCreationPrompt(existing.getPromptText(), asset.getPromptText()));
        existing.setSourceName(empty(asset.getSourceName()));
        String agentName = resolveStandaloneAgentName(asset);
        if (StringUtils.hasText(agentName)) {
            existing.setAgentName(agentName);
        }
        if (StringUtils.hasText(asset.getWorkflowExecId())) {
            existing.setWorkflowExecId(asset.getWorkflowExecId().trim());
        }
        if (!StringUtils.hasText(existing.getSource())
                || ChatAssetSources.ONLINE.equals(existing.getSource())) {
            existing.setSource(ChatAssetSources.normalize(asset.getSource()));
        }
        existing.setStatus("synced");
        existing.setUpdatedAt(now);
    }

    @Override
    public PipelineExec upsertPipelineExec(String agentkSessionId, SyncedPipelineExec exec) {
        if (exec == null || !StringUtils.hasText(exec.getAgentkExecId()) || !StringUtils.hasText(agentkSessionId)) {
            return null;
        }
        Optional<ChatSession> sessionOpt = findByAgentkSessionId(agentkSessionId.trim());
        if (!sessionOpt.isPresent()) {
            LOGGER.warn("upsertPipelineExec skipped: platform session missing agentkSessionId={}", agentkSessionId);
            return null;
        }
        ChatSession session = sessionOpt.get();
        Long workflowId = resolveWorkflowIdFromVersionId(session.getWorkflowVersionId());
        PipelineExec row = upsertPipelineExecRow(
                empty(session.getUserId()),
                empty(session.getTenantId()),
                session.getId(),
                empty(session.getAgentkSessionId()),
                workflowId,
                session.getWorkflowVersionId(),
                session.getAgentVersionId(),
                exec);
        if (row != null) {
            session.setUpdatedAt(LocalDateTime.now());
            chatSessionMapper.updateById(session);
        }
        return row;
    }

    @Override
    public PipelineExec upsertStandalonePipelineExec(String userId, String tenantId, long sessionId,
            String agentkSessionId, SyncedPipelineExec exec) {
        if (exec == null || !StringUtils.hasText(exec.getAgentkExecId()) || sessionId <= 0) {
            return null;
        }
        // KEffects 等 API：taskType 即 aop_workflow.agentk_id
        Long workflowId = resolveWorkflowIdByAgentkId(exec.getPipelineName());
        if (workflowId == null && StringUtils.hasText(exec.getPipelineName())) {
            LOGGER.warn("upsertStandalonePipelineExec: aop_workflow not found for agentk_id={}",
                    exec.getPipelineName());
        }
        return upsertPipelineExecRow(
                empty(userId),
                empty(tenantId),
                sessionId,
                empty(agentkSessionId),
                workflowId,
                null,
                null,
                exec);
    }

    private PipelineExec upsertPipelineExecRow(String userId, String tenantId, long sessionId,
            String agentkSessionId, Long workflowId, Long workflowVersionId, Long agentVersionId,
            SyncedPipelineExec exec) {
        LocalDateTime now = LocalDateTime.now();
        String agentkExecId = exec.getAgentkExecId().trim();
        PipelineExec existing = pipelineExecMapper.selectOne(new LambdaQueryWrapper<PipelineExec>()
                .eq(PipelineExec::getAgentkExecId, agentkExecId)
                .last("LIMIT 1"));
        String argsJson = jsonOrNull(exec.getArgs());
        String outputJson = jsonOrNull(exec.getOutput());
        LocalDateTime startedAt = parseDateTime(exec.getStartedAt());
        LocalDateTime finishedAt = parseDateTime(exec.getFinishedAt());
        if (existing == null) {
            PipelineExec row = new PipelineExec();
            row.setPublicId(PlatformIds.newPublicId("ppe_"));
            row.setTenantId(tenantId);
            row.setUserId(userId);
            row.setSessionId(sessionId);
            row.setWorkflowId(workflowId);
            row.setWorkflowVersionId(workflowVersionId);
            row.setAgentVersionId(agentVersionId);
            row.setAgentkSessionId(agentkSessionId);
            row.setAgentkExecId(agentkExecId);
            row.setPipelineName(empty(exec.getPipelineName()));
            row.setCapabilityId(empty(exec.getCapabilityId()));
            row.setArgsJson(argsJson);
            row.setOutputJson(outputJson);
            row.setProfileVersion(empty(exec.getProfileVersion()));
            row.setStatus(StringUtils.hasText(exec.getStatus()) ? exec.getStatus().trim() : "running");
            row.setError(exec.getError());
            row.setStartedAt(startedAt != null ? startedAt : now);
            row.setFinishedAt(finishedAt);
            row.setCreatedAt(now);
            row.setUpdatedAt(now);
            pipelineExecMapper.insert(row);
            LOGGER.info("pipeline exec inserted id={} agentkExecId={} pipeline={} workflowId={} status={}",
                    row.getId(), agentkExecId, row.getPipelineName(), workflowId, row.getStatus());
            return row;
        }
        if (StringUtils.hasText(userId) && !StringUtils.hasText(existing.getUserId())) {
            existing.setUserId(userId);
        }
        if (StringUtils.hasText(agentkSessionId) && !StringUtils.hasText(existing.getAgentkSessionId())) {
            existing.setAgentkSessionId(agentkSessionId);
        }
        if (workflowId != null && (existing.getWorkflowId() == null || existing.getWorkflowId() == 0L)) {
            existing.setWorkflowId(workflowId);
        }
        if (workflowVersionId != null
                && (existing.getWorkflowVersionId() == null || existing.getWorkflowVersionId() == 0L)) {
            existing.setWorkflowVersionId(workflowVersionId);
        }
        if (agentVersionId != null
                && (existing.getAgentVersionId() == null || existing.getAgentVersionId() == 0L)) {
            existing.setAgentVersionId(agentVersionId);
        }
        if (StringUtils.hasText(exec.getPipelineName())) {
            existing.setPipelineName(exec.getPipelineName().trim());
        }
        if (StringUtils.hasText(exec.getCapabilityId()) && !StringUtils.hasText(existing.getCapabilityId())) {
            existing.setCapabilityId(exec.getCapabilityId().trim());
        }
        if (StringUtils.hasText(argsJson)) {
            existing.setArgsJson(argsJson);
        }
        if (StringUtils.hasText(outputJson)) {
            existing.setOutputJson(outputJson);
        }
        if (StringUtils.hasText(exec.getProfileVersion())) {
            existing.setProfileVersion(exec.getProfileVersion().trim());
        }
        if (StringUtils.hasText(exec.getStatus())) {
            existing.setStatus(exec.getStatus().trim());
        }
        if (exec.getError() != null) {
            existing.setError(exec.getError());
        }
        if (startedAt != null && existing.getStartedAt() == null) {
            existing.setStartedAt(startedAt);
        }
        if (finishedAt != null) {
            existing.setFinishedAt(finishedAt);
        } else if (isTerminalPipelineStatus(existing.getStatus()) && existing.getFinishedAt() == null) {
            existing.setFinishedAt(now);
        }
        existing.setUpdatedAt(now);
        pipelineExecMapper.updateById(existing);
        LOGGER.info("pipeline exec updated id={} agentkExecId={} workflowId={} status={} hasArgs={} hasOutput={}",
                existing.getId(), agentkExecId, existing.getWorkflowId(), existing.getStatus(),
                StringUtils.hasText(existing.getArgsJson()), StringUtils.hasText(existing.getOutputJson()));
        return existing;
    }

    /** taskType / pipelineName → {@code aop_workflow.id}（按 agentk_id 精确匹配）。 */
    private Long resolveWorkflowIdByAgentkId(String agentkId) {
        if (!StringUtils.hasText(agentkId)) {
            return null;
        }
        Workflow workflow = workflowMapper.selectOne(new LambdaQueryWrapper<Workflow>()
                .eq(Workflow::getAgentkId, agentkId.trim())
                .orderByDesc(Workflow::getIsDefault)
                .orderByAsc(Workflow::getId)
                .last("LIMIT 1"));
        return workflow == null ? null : workflow.getId();
    }

    private Long resolveWorkflowIdFromVersionId(Long workflowVersionId) {
        if (workflowVersionId == null || workflowVersionId <= 0L) {
            return null;
        }
        WorkflowVersion version = workflowVersionMapper.selectById(workflowVersionId);
        return version == null ? null : version.getWorkflowId();
    }

    @Override
    public List<PipelineExec> listPipelineExecsOwned(long sessionId, String userId, int limit) {
        requireOwnedSession(sessionId, userId);
        return pipelineExecMapper.selectList(new LambdaQueryWrapper<PipelineExec>()
                .eq(PipelineExec::getSessionId, sessionId)
                .orderByDesc(PipelineExec::getCreatedAt)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public List<PipelineExec> listPipelineExecsByAgentkSessionId(String agentkSessionId, int limit) {
        if (!StringUtils.hasText(agentkSessionId)) {
            throw new IllegalArgumentException("agentkSessionId required");
        }
        return pipelineExecMapper.selectList(new LambdaQueryWrapper<PipelineExec>()
                .eq(PipelineExec::getAgentkSessionId, agentkSessionId.trim())
                .orderByDesc(PipelineExec::getCreatedAt)
                .last("LIMIT " + Math.max(limit, 1)));
    }

    @Override
    public IPage<PipelineExec> pagePipelineExecsByPipelineName(String pipelineName, String userId,
            long page, long pageSize) {
        if (!StringUtils.hasText(pipelineName)) {
            throw new IllegalArgumentException("pipelineName required");
        }
        long pageNo = page <= 0 ? 1 : page;
        long size = pageSize <= 0 ? 20 : Math.min(pageSize, 100);
        LambdaQueryWrapper<PipelineExec> wrapper = new LambdaQueryWrapper<PipelineExec>()
                .eq(PipelineExec::getPipelineName, pipelineName.trim())
                .orderByDesc(PipelineExec::getCreatedAt)
                .orderByDesc(PipelineExec::getId);
        if (StringUtils.hasText(userId)) {
            wrapper.eq(PipelineExec::getUserId, userId.trim());
        }
        return pipelineExecMapper.selectPage(new Page<>(pageNo, size), wrapper);
    }

    @Override
    public IPage<PipelineExec> pageCapabilityLabExecs(String capabilityId, String userId, String status,
            long page, long pageSize) {
        long pageNo = page <= 0 ? 1 : page;
        long size = pageSize <= 0 ? 20 : Math.min(pageSize, 100);
        LambdaQueryWrapper<PipelineExec> wrapper = new LambdaQueryWrapper<PipelineExec>()
                .eq(PipelineExec::getPipelineName, "capability_run")
                .orderByDesc(PipelineExec::getCreatedAt)
                .orderByDesc(PipelineExec::getId);
        if (StringUtils.hasText(capabilityId)) {
            wrapper.eq(PipelineExec::getCapabilityId, capabilityId.trim());
        }
        if (StringUtils.hasText(userId)) {
            wrapper.eq(PipelineExec::getUserId, userId.trim());
        }
        if (StringUtils.hasText(status)) {
            wrapper.eq(PipelineExec::getStatus, status.trim());
        }
        return pipelineExecMapper.selectPage(new Page<>(pageNo, size), wrapper);
    }

    @Override
    public List<PipelineExec> listPipelineExecsByPipelineNames(Collection<String> pipelineNames, String userId,
            int limit) {
        if (pipelineNames == null || pipelineNames.isEmpty()) {
            return Collections.emptyList();
        }
        List<String> names = pipelineNames.stream()
                .filter(StringUtils::hasText)
                .map(String::trim)
                .distinct()
                .collect(Collectors.toList());
        if (names.isEmpty()) {
            return Collections.emptyList();
        }
        LambdaQueryWrapper<PipelineExec> wrapper = new LambdaQueryWrapper<PipelineExec>()
                .in(PipelineExec::getPipelineName, names)
                .orderByDesc(PipelineExec::getCreatedAt)
                .orderByDesc(PipelineExec::getId)
                .last("LIMIT " + Math.max(limit, 1));
        if (StringUtils.hasText(userId)) {
            wrapper.eq(PipelineExec::getUserId, userId.trim());
        }
        List<PipelineExec> rows = pipelineExecMapper.selectList(wrapper);
        return rows == null ? Collections.emptyList() : rows;
    }

    @Override
    public List<String> listCapabilityLabCapabilityIds(String userId) {
        QueryWrapper<PipelineExec> wrapper = new QueryWrapper<PipelineExec>()
                .select("DISTINCT capability_id")
                .eq("pipeline_name", "capability_run")
                .ne("capability_id", "")
                .isNotNull("capability_id")
                .orderByAsc("capability_id");
        if (StringUtils.hasText(userId)) {
            wrapper.eq("user_id", userId.trim());
        }
        List<PipelineExec> rows = pipelineExecMapper.selectList(wrapper);
        if (rows == null || rows.isEmpty()) {
            return Collections.emptyList();
        }
        return rows.stream()
                .map(PipelineExec::getCapabilityId)
                .filter(StringUtils::hasText)
                .collect(Collectors.toList());
    }

    @Override
    public PipelineExec requireOwnedPipelineExec(long id, String userId) {
        PipelineExec exec = pipelineExecMapper.selectById(id);
        if (exec == null) {
            throw new IllegalArgumentException("pipeline exec not found: " + id);
        }
        requireOwnedSession(exec.getSessionId(), userId);
        return exec;
    }

    @Override
    public PipelineExec requireOwnedPipelineExecByAgentkId(String agentkExecId, String userId) {
        if (!StringUtils.hasText(agentkExecId)) {
            throw new IllegalArgumentException("agentkExecId required");
        }
        PipelineExec exec = pipelineExecMapper.selectOne(new LambdaQueryWrapper<PipelineExec>()
                .eq(PipelineExec::getAgentkExecId, agentkExecId.trim())
                .last("LIMIT 1"));
        if (exec == null) {
            throw new IllegalArgumentException("pipeline exec not found: " + agentkExecId);
        }
        requireOwnedSession(exec.getSessionId(), userId);
        return exec;
    }

    @Override
    public PipelineExec requirePipelineExec(long id) {
        PipelineExec exec = pipelineExecMapper.selectById(id);
        if (exec == null) {
            throw new IllegalArgumentException("pipeline exec not found: " + id);
        }
        return exec;
    }

    @Override
    public PipelineExec requirePipelineExecByAgentkId(String agentkExecId) {
        if (!StringUtils.hasText(agentkExecId)) {
            throw new IllegalArgumentException("agentkExecId required");
        }
        PipelineExec exec = pipelineExecMapper.selectOne(new LambdaQueryWrapper<PipelineExec>()
                .eq(PipelineExec::getAgentkExecId, agentkExecId.trim())
                .last("LIMIT 1"));
        if (exec == null) {
            throw new IllegalArgumentException("pipeline exec not found: " + agentkExecId);
        }
        return exec;
    }

    @Override
    public List<PipelineExec> listPipelineExecsByAgentkExecIds(java.util.Collection<String> agentkExecIds) {
        if (agentkExecIds == null || agentkExecIds.isEmpty()) {
            return Collections.emptyList();
        }
        List<String> ids = new ArrayList<>();
        Set<String> seen = new HashSet<>();
        for (String id : agentkExecIds) {
            if (!StringUtils.hasText(id)) {
                continue;
            }
            String trimmed = id.trim();
            if (seen.add(trimmed)) {
                ids.add(trimmed);
            }
        }
        if (ids.isEmpty()) {
            return Collections.emptyList();
        }
        List<PipelineExec> rows = pipelineExecMapper.selectList(new LambdaQueryWrapper<PipelineExec>()
                .in(PipelineExec::getAgentkExecId, ids));
        Map<String, PipelineExec> byId = new HashMap<>();
        if (rows != null) {
            for (PipelineExec row : rows) {
                if (row != null && StringUtils.hasText(row.getAgentkExecId())) {
                    byId.putIfAbsent(row.getAgentkExecId().trim(), row);
                }
            }
        }
        List<PipelineExec> ordered = new ArrayList<>();
        for (String id : ids) {
            PipelineExec row = byId.get(id);
            if (row != null) {
                ordered.add(row);
            }
        }
        return ordered;
    }

    @Override
    public void refreshAssetUrl(long assetId, String assetUrl) {
        if (assetId <= 0 || !StringUtils.hasText(assetUrl)) {
            return;
        }
        ChatAsset patch = new ChatAsset();
        patch.setId(assetId);
        patch.setAssetUrl(assetUrl.trim());
        patch.setUpdatedAt(LocalDateTime.now());
        chatAssetMapper.updateById(patch);
    }

    @Override
    public void batchRefreshAssetUrls(Map<Long, String> assetIdToUrl) {
        if (assetIdToUrl == null || assetIdToUrl.isEmpty()) {
            return;
        }
        Map<Long, String> patch = new LinkedHashMap<>();
        for (Map.Entry<Long, String> entry : assetIdToUrl.entrySet()) {
            if (entry.getKey() == null || entry.getKey() <= 0 || !StringUtils.hasText(entry.getValue())) {
                continue;
            }
            patch.put(entry.getKey(), entry.getValue().trim());
        }
        if (patch.isEmpty()) {
            return;
        }
        chatAssetMapper.batchRefreshAssetUrls(patch);
    }

    private void upsertOneAsset(ChatSession session, ChatRun run, SyncedAsset asset,
            String agentkSessionId, String agentkRunId, LocalDateTime now, List<ChatAsset> existingRows) {
        ChatAsset existing = findExistingAsset(session.getId(), asset, existingRows);
        if (existing == null) {
            ChatAsset row = new ChatAsset();
            row.setPublicId(PlatformIds.newPublicId("ast_"));
            row.setSessionId(session.getId());
            row.setRunId(run == null ? null : run.getId());
            row.setMessageId(findMessageId(session.getId(), asset.getSeq()));
            row.setUserId(empty(session.getUserId()));
            row.setAssetType(asset.getAssetType().trim());
            row.setAssetUrl(asset.getAssetUrl().trim());
            row.setPromptText(empty(asset.getPromptText()));
            row.setSourceKind(empty(asset.getSourceKind()));
            row.setSource(resolveAssetSource(asset, agentkSessionId, agentkRunId));
            row.setSourceName(empty(asset.getSourceName()));
            row.setAgentName(resolveAssetAgentName(asset, session));
            row.setAgentkEventSeq(asset.getSeq());
            row.setRoundId(empty(asset.getRoundId()));
            if (!StringUtils.hasText(row.getRoundId()) && run != null) {
                row.setRoundId(empty(run.getLastRoundId()));
            }
            row.setAgentkSessionId(empty(agentkSessionId));
            row.setAgentkRunId(blankToNull(agentkRunId));
            row.setWorkflowExecId(blankToNull(asset.getWorkflowExecId()));
            row.setStatus("synced");
            row.setCreatedAt(now);
            row.setUpdatedAt(now);
            try {
                chatAssetMapper.insert(row);
                existingRows.add(row);
            } catch (RuntimeException duplicate) {
                if (!isDuplicateKeyException(duplicate)) {
                    throw duplicate;
                }
                ChatAsset duplicateRow = queryExistingAsset(session.getId(), asset);
                if (duplicateRow != null) {
                    mergeAssetFields(duplicateRow, asset, run, session, agentkSessionId, agentkRunId, now);
                    chatAssetMapper.updateById(duplicateRow);
                    existingRows.add(duplicateRow);
                    return;
                }
                throw duplicate;
            }
        } else {
            mergeAssetFields(existing, asset, run, session, agentkSessionId, agentkRunId, now);
            chatAssetMapper.updateById(existing);
        }
    }

    private void mergeAssetFields(ChatAsset existing, SyncedAsset asset, ChatRun run, ChatSession session,
            String agentkSessionId, String agentkRunId, LocalDateTime now) {
        existing.setRunId(existing.getRunId() != null ? existing.getRunId() : run == null ? null : run.getId());
        if (existing.getMessageId() == null) {
            existing.setMessageId(findMessageId(existing.getSessionId(), asset.getSeq()));
        }
        if (asset.getSeq() != null) {
            existing.setAgentkEventSeq(asset.getSeq());
        }
        if (StringUtils.hasText(agentkRunId)) {
            existing.setAgentkRunId(agentkRunId);
        }
        if (StringUtils.hasText(agentkSessionId)) {
            existing.setAgentkSessionId(agentkSessionId);
        }
        if (StringUtils.hasText(asset.getRoundId())) {
            existing.setRoundId(asset.getRoundId().trim());
        } else if (!StringUtils.hasText(existing.getRoundId()) && run != null) {
            existing.setRoundId(empty(run.getLastRoundId()));
        }
        existing.setPromptText(preferCreationPrompt(existing.getPromptText(), asset.getPromptText()));
        existing.setSourceName(empty(asset.getSourceName()));
        if (StringUtils.hasText(asset.getSourceKind())) {
            existing.setSourceKind(asset.getSourceKind().trim());
        }
        existing.setAgentName(resolveAssetAgentName(asset, session));
        if (!StringUtils.hasText(existing.getSource())
                || ChatAssetSources.ONLINE.equals(existing.getSource())) {
            existing.setSource(resolveAssetSource(asset, agentkSessionId, agentkRunId));
        } else if (StringUtils.hasText(asset.getSource())) {
            existing.setSource(ChatAssetSources.normalize(asset.getSource()));
        }
        existing.setStatus("synced");
        existing.setUpdatedAt(now);
    }

    /** API 任务（KEffects）智能体名只用 taskType，不要拼产物字段。 */
    private static String resolveStandaloneAgentName(SyncedAsset asset) {
        if (asset != null && StringUtils.hasText(asset.getAgentName())) {
            return stripOutputFieldSuffix(asset.getAgentName().trim());
        }
        if (asset != null && StringUtils.hasText(asset.getSourceName())) {
            return stripOutputFieldSuffix(asset.getSourceName().trim());
        }
        return "";
    }

    static String stripOutputFieldSuffix(String name) {
        if (!StringUtils.hasText(name)) {
            return "";
        }
        String text = name.trim();
        int idx = text.lastIndexOf(':');
        if (idx <= 0) {
            return text;
        }
        return text.substring(0, idx).trim();
    }

    private ChatAsset findExistingAsset(Long sessionId, SyncedAsset asset, List<ChatAsset> cached) {
        ChatAsset hit = matchExistingAsset(cached, sessionId, asset);
        if (hit != null) {
            return hit;
        }
        return null;
    }

    private ChatAsset queryExistingAsset(Long sessionId, SyncedAsset asset) {
        String incomingUrl = asset.getAssetUrl().trim();
        LambdaQueryWrapper<ChatAsset> q = new LambdaQueryWrapper<ChatAsset>()
                .eq(ChatAsset::getSessionId, sessionId)
                .eq(ChatAsset::getSourceKind, empty(asset.getSourceKind()));
        if (StringUtils.hasText(asset.getWorkflowExecId())) {
            q.eq(ChatAsset::getWorkflowExecId, asset.getWorkflowExecId().trim());
        } else {
            q.isNull(ChatAsset::getWorkflowExecId);
        }
        for (ChatAsset row : chatAssetMapper.selectList(q)) {
            if (urlMatchesUniqueKey(row.getAssetUrl(), incomingUrl)) {
                return row;
            }
        }
        return null;
    }

    private List<ChatAsset> loadSessionAssets(Long sessionId) {
        if (sessionId == null) {
            return new ArrayList<>();
        }
        List<ChatAsset> rows = chatAssetMapper.selectList(new LambdaQueryWrapper<ChatAsset>()
                .eq(ChatAsset::getSessionId, sessionId));
        return rows == null ? new ArrayList<>() : new ArrayList<>(rows);
    }

    private static ChatAsset matchExistingAsset(List<ChatAsset> cached, Long sessionId, SyncedAsset asset) {
        if (cached == null || cached.isEmpty() || sessionId == null || asset == null
                || !StringUtils.hasText(asset.getAssetUrl())) {
            return null;
        }
        String incomingUrl = asset.getAssetUrl().trim();
        String sourceKind = empty(asset.getSourceKind());
        boolean hasExec = StringUtils.hasText(asset.getWorkflowExecId());
        String execId = hasExec ? asset.getWorkflowExecId().trim() : null;
        for (ChatAsset row : cached) {
            if (row == null || row.getSessionId() == null || !sessionId.equals(row.getSessionId())) {
                continue;
            }
            if (!Objects.equals(sourceKind, row.getSourceKind())) {
                boolean capabilityReplacingPipeline = "capability".equals(sourceKind)
                        && "pipeline".equals(empty(row.getSourceKind()));
                if (!capabilityReplacingPipeline) {
                    continue;
                }
            }
            if (hasExec) {
                if (!execId.equals(row.getWorkflowExecId())) {
                    continue;
                }
            } else if (row.getWorkflowExecId() != null) {
                continue;
            }
            if (urlMatchesUniqueKey(row.getAssetUrl(), incomingUrl)) {
                return row;
            }
        }
        return null;
    }

    private static String assetUniqueKey(Long sessionId, SyncedAsset asset) {
        return sessionId + "|" + empty(asset.getSourceKind()) + "|"
                + (StringUtils.hasText(asset.getWorkflowExecId()) ? asset.getWorkflowExecId().trim() : "")
                + "|" + urlUniquePrefix(asset.getAssetUrl());
    }

    private static String urlUniquePrefix(String url) {
        if (!StringUtils.hasText(url)) {
            return "";
        }
        String trimmed = url.trim();
        return trimmed.length() <= ASSET_URL_UNIQUE_PREFIX_LEN
                ? trimmed : trimmed.substring(0, ASSET_URL_UNIQUE_PREFIX_LEN);
    }

    private static boolean urlMatchesUniqueKey(String existingUrl, String incomingUrl) {
        if (!StringUtils.hasText(existingUrl) || !StringUtils.hasText(incomingUrl)) {
            return false;
        }
        String left = existingUrl.trim();
        String right = incomingUrl.trim();
        if (left.equals(right)) {
            return true;
        }
        return urlUniquePrefix(left).equals(urlUniquePrefix(right));
    }

    private static boolean isDuplicateKeyException(Throwable e) {
        for (Throwable t = e; t != null; t = t.getCause()) {
            if (t instanceof org.springframework.dao.DuplicateKeyException) {
                return true;
            }
            String message = t.getMessage();
            if (message != null && (message.contains("Duplicate entry")
                    || message.contains("errno 1062"))) {
                return true;
            }
        }
        return false;
    }

    private Long findMessageId(Long sessionId, Integer seq) {
        if (sessionId == null || seq == null) {
            return null;
        }
        ChatMessage message = chatMessageMapper.selectOne(new LambdaQueryWrapper<ChatMessage>()
                .eq(ChatMessage::getSessionId, sessionId)
                .eq(ChatMessage::getAgentkItemSeq, seq)
                .last("LIMIT 1"));
        return message == null ? null : message.getId();
    }

    private void applyRunStarted(ChatSession session, ChatRun run, JsonNode payload, String roundId) {
        String model = text(payload, "model");
        String dialect = text(payload, "dialect");
        String profileVersion = text(payload, "profileVersion");
        String promptFingerprint = text(payload, "promptSha256");
        String workerId = text(payload, "workerId");
        String toolsJson = payload.has("tools") ? safeJson(payload.get("tools")) : null;

        run.setModel(model);
        run.setDialect(dialect);
        run.setProfileVersion(profileVersion);
        run.setPromptFingerprint(promptFingerprint);
        run.setWorkerId(workerId);
        run.setToolsJson(toolsJson);
        if (StringUtils.hasText(roundId)) {
            run.setLastRoundId(roundId.trim());
        }
        boolean mismatch = StringUtils.hasText(session.getEffectiveModel())
                && StringUtils.hasText(model)
                && !Objects.equals(session.getEffectiveModel(), model);
        run.setConfigMismatch(mismatch);
        chatRunMapper.updateById(run);

        LOGGER.info("round.started persisted platformExecutionId={} agentkExecutionId={} model={} dialect={} "
                        + "profileVersion={} promptFingerprint={} workerId={} configMismatch={} sessionEffectiveModel={}",
                run.getId(), run.getAgentkRunId(), model, dialect, profileVersion, promptFingerprint, workerId,
                mismatch, session.getEffectiveModel());
        if (mismatch) {
            LOGGER.warn("round.started model mismatch sessionId={} expected={} actual={}",
                    session.getId(), session.getEffectiveModel(), model);
        }
    }

    private Optional<ChatRun> findRunByAgentkId(String agentkRunId) {
        if (!StringUtils.hasText(agentkRunId)) {
            return Optional.empty();
        }
        return Optional.ofNullable(chatRunMapper.selectOne(new LambdaQueryWrapper<ChatRun>()
                .eq(ChatRun::getAgentkRunId, agentkRunId)
                .last("LIMIT 1")));
    }

    private String resolveAssetSource(SyncedAsset asset, String agentkSessionId, String agentkRunId) {
        if (asset != null && StringUtils.hasText(asset.getSource())) {
            return ChatAssetSources.normalize(asset.getSource());
        }
        if (isOfflineBatchRun(agentkSessionId, agentkRunId)) {
            return ChatAssetSources.OFFLINE;
        }
        return ChatAssetSources.ONLINE;
    }

    private boolean isOfflineBatchRun(String agentkSessionId, String agentkRunId) {
        if (!StringUtils.hasText(agentkSessionId) && !StringUtils.hasText(agentkRunId)) {
            return false;
        }
        return offlineJobCellMapper.countByAgentkRef(
                StringUtils.hasText(agentkSessionId) ? agentkSessionId.trim() : "",
                StringUtils.hasText(agentkRunId) ? agentkRunId.trim() : "") > 0;
    }

    /** 作品集查询：仅当平台 run_id 明确关联 failed 才排除（pipeline 在写入侧已过滤失败）。 */
    private void applyExcludeFailedRunAssets(LambdaQueryWrapper<ChatAsset> wrapper) {
        wrapper.apply("NOT EXISTS (SELECT 1 FROM aop_execution r WHERE r.`status` = 'failed' "
                + "AND execution_id IS NOT NULL AND execution_id > 0 AND r.id = execution_id)");
    }

    private boolean isAssetFromFailedRun(ChatAsset asset) {
        if (asset == null) {
            return false;
        }
        if (asset.getRunId() != null && asset.getRunId() > 0) {
            ChatRun run = chatRunMapper.selectById(asset.getRunId());
            if (run != null && "failed".equals(empty(run.getStatus()))) {
                return true;
            }
        }
        return false;
    }

    private List<ChatJob> listOpenChatJobs(Long sessionId) {
        return chatJobMapper.selectList(new LambdaQueryWrapper<ChatJob>()
                .eq(ChatJob::getSessionId, sessionId)
                .eq(ChatJob::getStatus, "submitted"));
    }

    private AgentkCallLog newCallLog(String bizType, Long bizId, String userId, String tenantId, String api) {
        AgentkCallLog log = new AgentkCallLog();
        log.setBizType(bizType);
        log.setBizId(bizId == null ? 0L : bizId);
        log.setUserId(empty(userId));
        log.setTenantId(empty(tenantId));
        log.setApi(api);
        log.setStatus("pending");
        log.setCreatedAt(LocalDateTime.now());
        return log;
    }

    private String safeJson(Object value) {
        try {
            return objectMapper.writeValueAsString(value);
        } catch (Exception e) {
            return "{}";
        }
    }

    private String jsonOrNull(JsonNode node) {
        if (node == null || node.isNull() || node.isMissingNode()) {
            return null;
        }
        try {
            return objectMapper.writeValueAsString(node);
        } catch (Exception e) {
            return node.toString();
        }
    }

    private static LocalDateTime parseDateTime(String raw) {
        // 带 Z/+08:00 的上游时间转到东八区墙钟，禁止 OffsetDateTime.toLocalDateTime() 直接丢掉偏移。
        return PlatformDateTimes.parse(raw);
    }

    private static boolean isTerminalPipelineStatus(String status) {
        if (!StringUtils.hasText(status)) {
            return false;
        }
        String normalized = status.trim().toLowerCase();
        return "completed".equals(normalized)
                || "failed".equals(normalized)
                || "cancelled".equals(normalized)
                || "canceled".equals(normalized)
                || normalized.contains("fail")
                || normalized.contains("cancel");
    }

    private static String text(JsonNode node, String field) {
        if (node == null || !node.has(field) || node.get(field).isNull()) {
            return "";
        }
        String v = node.get(field).asText();
        return v == null ? "" : v;
    }

    private static String empty(String s) {
        return s == null ? "" : s;
    }

    /**
     * 合并 prompt：空不覆盖；更长文本优先，避免催进度短句冲掉创作指令。
     */
    private static String preferCreationPrompt(String existing, String incoming) {
        String oldText = existing == null ? "" : existing.trim();
        String newText = incoming == null ? "" : incoming.trim();
        if (!StringUtils.hasText(newText)) {
            return oldText;
        }
        if (!StringUtils.hasText(oldText)) {
            return newText;
        }
        if (newText.length() >= oldText.length()) {
            return newText;
        }
        return oldText;
    }

    private static boolean isPublishableAssetType(String assetType) {
        if (!StringUtils.hasText(assetType)) {
            return false;
        }
        String normalized = assetType.trim().toLowerCase();
        return "image".equals(normalized) || "video".equals(normalized);
    }
}
