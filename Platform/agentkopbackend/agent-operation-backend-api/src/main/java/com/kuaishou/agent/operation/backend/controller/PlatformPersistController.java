package com.kuaishou.agent.operation.backend.controller;

import java.util.List;
import java.util.Map;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.util.StringUtils;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.PutMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestMethod;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.agentk.AgentKCapabilityCacheService;
import com.kuaishou.agent.operation.backend.agentk.AgentKIdentityResolver;
import com.kuaishou.agent.operation.backend.agentk.AgentKOutboundIdentity;
import com.kuaishou.agent.operation.backend.agentk.ChatAssetSyncService;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityListViewDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityRefreshResultDto;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.dto.ChatAssetDetailView;
import com.kuaishou.agent.operation.backend.platform.dto.ChatAssetPortfolioItemView;
import com.kuaishou.agent.operation.backend.platform.dto.ChatAssetPortfolioQuery;
import com.kuaishou.agent.operation.backend.platform.dto.ChatSessionDetailView;
import com.kuaishou.agent.operation.backend.platform.dto.CreateAgentLoopRequest;
import com.kuaishou.agent.operation.backend.platform.dto.CreateWorkflowRequest;
import com.kuaishou.agent.operation.backend.platform.dto.MineChatSessionQuery;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;
import com.kuaishou.agent.operation.backend.platform.dto.SaveAgentLoopVersionRequest;
import com.kuaishou.agent.operation.backend.platform.dto.SessionProfileTemplateView;
import com.kuaishou.agent.operation.backend.platform.dto.StreamCursorUpdateRequest;
import com.kuaishou.agent.operation.backend.platform.dto.UpdateSessionTitleRequest;
import com.kuaishou.agent.operation.backend.platform.entity.Agent;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.entity.AgentkCallLog;
import com.kuaishou.agent.operation.backend.platform.entity.ChatAsset;
import com.kuaishou.agent.operation.backend.platform.entity.ChatMessage;
import com.kuaishou.agent.operation.backend.platform.entity.ChatRun;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.entity.PipelineExec;
import com.kuaishou.agent.operation.backend.platform.entity.RunEvent;
import com.kuaishou.agent.operation.backend.platform.entity.Workflow;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;
import com.kuaishou.agent.operation.backend.platform.service.ChatAssetPortfolioService;
import com.kuaishou.agent.operation.backend.platform.service.PlatformAgentWorkflowService;
import com.kuaishou.agent.operation.backend.platform.service.PlatformChatService;

/**
 * 平台资产 API（Chat 归属 / AgentLoop·Workflow 版本 / Dataset Snapshot）。
 * <p>
 * AgentLoop：管理 agent-k Agent Loop 使用的 profile 版本（表仍为 aop_agent / aop_agent_version）。
 * 不含离线评测 EvalRun。
 */
@RestController
@RequestMapping("/agent/operation/backend/platform")
public class PlatformPersistController {

    private static final Logger LOGGER = LoggerFactory.getLogger(PlatformPersistController.class);

    private final PlatformChatService platformChatService;
    private final PlatformAgentWorkflowService platformAgentWorkflowService;
    private final AgentKIdentityResolver identityResolver;
    private final AgentKCapabilityCacheService capabilityCacheService;
    private final ChatAssetSyncService chatAssetSyncService;
    private final ChatAssetPortfolioService chatAssetPortfolioService;

    public PlatformPersistController(PlatformChatService platformChatService,
            PlatformAgentWorkflowService platformAgentWorkflowService,
            AgentKIdentityResolver identityResolver,
            AgentKCapabilityCacheService capabilityCacheService,
            ChatAssetSyncService chatAssetSyncService,
            ChatAssetPortfolioService chatAssetPortfolioService) {
        this.platformChatService = platformChatService;
        this.platformAgentWorkflowService = platformAgentWorkflowService;
        this.identityResolver = identityResolver;
        this.capabilityCacheService = capabilityCacheService;
        this.chatAssetSyncService = chatAssetSyncService;
        this.chatAssetPortfolioService = chatAssetPortfolioService;
    }

    /**
     * 我的会话列表（兼容旧 Query；建议改用 {@link #mySessionsQuery}）。
     */
    @GetMapping("/chat/sessions")
    public Message<List<ChatSession>> mySessions(@RequestParam(value = "limit", required = false) Integer limit,
            @RequestParam(value = "agentLoopVersionId", required = false) Long agentLoopVersionId,
            @RequestParam(value = "workflowVersionId", required = false) Long workflowVersionId) {
        return Message.ok(platformChatService.listMine(identityResolver.resolve().getUserId(),
                limit, agentLoopVersionId, workflowVersionId));
    }

    /**
     * 个人历史分页：默认按当前用户隔离。超级管理员（role=SUPER_ADMIN）可不传 userId 看全部，
     * 或传 {@code userId} 看指定用户；非超管传入的 userId 会被忽略。
     * <p>
     * 选 {@code agentLoopVersionId} 时返回该版本下会话；私有版本须属于自己的 AgentLoop，
     * public 版本允许所有人查询自己的历史（超管可跨用户）。
     * 也可只传 {@code sessionKind=agent_loop|workflow|custom} 按入口类型筛。
     * 默认排除 {@code entry_source=offline_batch}；每条回填 {@code lastExecutionId}/{@code lastAgentkRunId}。
     */
    @PostMapping("/chat/sessions/query")
    public Message<PageResult<ChatSession>> mySessionsQuery(
            @RequestBody(required = false) MineChatSessionQuery body) {
        return Message.ok(platformChatService.queryMine(identityResolver.resolve().getUserId(), body));
    }

    /** 平台会话详情（含 AgentLoop/Workflow 版本绑定）。 */
    @GetMapping("/chat/sessions/{sessionId:\\d+}")
    public Message<ChatSessionDetailView> sessionDetail(@PathVariable("sessionId") long sessionId) {
        return Message.ok(platformChatService.sessionDetail(identityResolver.resolve().getUserId(), sessionId));
    }

    @GetMapping("/chat/sessions/by-public/{publicId}")
    public Message<ChatSessionDetailView> sessionDetailByPublicId(@PathVariable("publicId") String publicId) {
        return Message.ok(platformChatService.sessionDetailByPublicId(
                identityResolver.resolve().getUserId(), publicId));
    }

    @GetMapping("/chat/sessions/by-agentk/{agentkSessionId}")
    public Message<ChatSessionDetailView> sessionDetailByAgentk(
            @PathVariable("agentkSessionId") String agentkSessionId) {
        return Message.ok(platformChatService.sessionDetailByAgentk(
                identityResolver.resolve().getUserId(), agentkSessionId));
    }

    /**
     * 手动修改会话标题（仅平台库；改名后不再被首条消息自动覆盖）。按 agent-k ses_xxx。
     * 主路径用 PUT（与 cursor 一致，网关/CORS 已放行）；兼容 PATCH。
     */
    @RequestMapping(value = "/chat/sessions/by-agentk/{agentkSessionId}/title",
            method = {RequestMethod.PUT, RequestMethod.PATCH})
    public Message<ChatSessionDetailView> updateSessionTitle(
            @PathVariable("agentkSessionId") String agentkSessionId,
            @RequestBody UpdateSessionTitleRequest request) {
        try {
            return Message.ok(platformChatService.updateSessionTitle(identityResolver.resolve().getUserId(),
                    agentkSessionId, request == null ? null : request.getTitle()));
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.BAD_REQUEST, e.getMessage());
        }
    }

    @GetMapping("/chat/sessions/{sessionId:\\d+}/messages")
    public Message<List<ChatMessage>> sessionMessages(@PathVariable("sessionId") long sessionId,
            @RequestParam(value = "limit", required = false) Integer limit) {
        return Message.ok(platformChatService.sessionMessages(
                identityResolver.resolve().getUserId(), sessionId, limit));
    }

    @GetMapping("/chat/assets")
    public Message<List<ChatAsset>> myAssets(@RequestParam(value = "assetType", required = false) String assetType,
            @RequestParam(value = "limit", required = false) Integer limit) {
        return Message.ok(platformChatService.myAssets(identityResolver.resolve().getUserId(), assetType, limit));
    }

    /**
     * 团队/公开作品集分页：按 run / pipeline / api 执行（roundKey）聚合，
     * 每条含中间产物 {@code intermediateAssets} 与结果产物 {@code resultAssets}。
     * <p>
     * 免登录（拦截器排除 {@code /chat/portfolio/**}）；可选 body.userId 筛选。
     */
    @PostMapping("/chat/portfolio/query")
    public Message<PageResult<ChatAssetPortfolioItemView>> portfolioQuery(
            @RequestBody(required = false) ChatAssetPortfolioQuery body) {
        return Message.ok(chatAssetPortfolioService.query(body));
    }

    /**
     * 团队/公开作品详情：媒体 + 同轮产物 + 会话实际 profile 配置（非版本号）。
     */
    @GetMapping("/chat/portfolio/assets/{assetId}")
    public Message<ChatAssetDetailView> portfolioAssetDetail(@PathVariable("assetId") long assetId) {
        return Message.ok(chatAssetPortfolioService.detail(assetId));
    }

    /**
     * 作品集筛选项：有作品资产的名称去重。免登录（{@code /chat/portfolio/**}）。
     * 含 public 智能体、public Workflow，以及 {@code source_kind=api} 的 taskType；不含 private。
     */
    @GetMapping("/chat/portfolio/agent-names")
    public Message<List<String>> portfolioAgentNames() {
        return Message.ok(chatAssetPortfolioService.listAgentNames());
    }

    /**
     * 团队/公开作品集筛选项：session_kind=workflow 且有作品的 Workflow 去重名称。
     * 免登录（{@code /chat/portfolio/**}）。
     */
    @GetMapping("/chat/portfolio/workflow-names")
    public Message<List<String>> portfolioWorkflowNames() {
        return Message.ok(chatAssetPortfolioService.listWorkflowNames());
    }

    /**
     * 我的作品集分页：聚合结构与团队作品集一致，强制只返回当前登录用户的产物。
     * <p>
     * 需登录；body.userId 会被忽略。路径不在 portfolio 免登录白名单内。
     */
    @PostMapping("/chat/my-portfolio/query")
    public Message<PageResult<ChatAssetPortfolioItemView>> myPortfolioQuery(
            @RequestBody(required = false) ChatAssetPortfolioQuery body) {
        return Message.ok(chatAssetPortfolioService.queryMine(identityResolver.resolve().getUserId(), body));
    }

    /**
     * 我的作品详情：仅本人资产可读。
     */
    @GetMapping("/chat/my-portfolio/assets/{assetId}")
    public Message<ChatAssetDetailView> myPortfolioAssetDetail(@PathVariable("assetId") long assetId) {
        return Message.ok(chatAssetPortfolioService.detailMine(assetId, identityResolver.resolve().getUserId()));
    }

    /**
     * 我的作品集筛选项：当前用户 {@code aop_asset.agent_name} 去重。需登录。
     */
    @GetMapping("/chat/my-portfolio/agent-names")
    public Message<List<String>> myPortfolioAgentNames() {
        return Message.ok(chatAssetPortfolioService.listAgentNamesMine(identityResolver.resolve().getUserId()));
    }

    /**
     * 我的作品集筛选项：当前用户 session_kind=workflow 且有作品的 Workflow 去重名称。需登录。
     */
    @GetMapping("/chat/my-portfolio/workflow-names")
    public Message<List<String>> myPortfolioWorkflowNames() {
        return Message.ok(chatAssetPortfolioService.listWorkflowNamesMine(identityResolver.resolve().getUserId()));
    }

    /**
     * Pipeline 草稿模板（tools=[]，可预填 pipelines）。只读不落库。
     * 官方 Workflow 快速对话仅 pin {@code workflowVersionId} 时，建会话会自动套用同一套 Pipeline-only 默认 profile。
     */
    @GetMapping("/session-profiles/templates/pipeline-only")
    public Message<SessionProfileTemplateView> pipelineOnlyProfileTemplate(
            @RequestParam(value = "workflowVersionId", required = false) Long workflowVersionId,
            @RequestParam(value = "agentkId", required = false) String agentkId) {
        return Message.ok(platformAgentWorkflowService.pipelineOnlyProfileTemplate(
                identityResolver.resolve().getUserId(), workflowVersionId, agentkId));
    }

    /**
     * 官方 AgentLoop 默认 Profile 模板（含 tools）。只读不落库。
     */
    @GetMapping("/session-profiles/templates/agent-loop-default")
    public Message<SessionProfileTemplateView> agentLoopDefaultProfileTemplate() {
        return Message.ok(platformAgentWorkflowService.agentLoopDefaultProfileTemplate());
    }

    /**
     * 历史回填：从 agent-k pipeline-runs + 会话 events 投影。
     * <p>
     * pipeline：写 {@code aop_pipeline_exec}（含 args/output/status，失败也会记）+
     * 成功媒体进 {@code aop_chat_asset}；agent_loop：成功 image/video 进 asset。
     */
    @PostMapping("/chat/assets/backfill")
    public Message<Map<String, Object>> backfillAssets(
            @RequestParam(value = "source", defaultValue = "all") String source,
            @RequestParam(value = "scope", defaultValue = "all") String scope,
            @RequestParam(value = "limit", defaultValue = "50") Integer limit,
            @RequestParam(value = "offset", defaultValue = "0") Integer offset,
            @RequestParam(value = "agentkSessionId", required = false) String agentkSessionId) {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        int sessionLimit = limit == null || limit <= 0 ? 50 : Math.min(limit, 200);
        int sessionOffset = offset == null || offset < 0 ? 0 : offset;
        return Message.ok(chatAssetSyncService.backfill(source, scope, sessionLimit, sessionOffset,
                agentkSessionId, identity));
    }

    /**
     * 兼容旧路径：等同 {@code POST /chat/assets/backfill?source=pipeline}。
     */
    @PostMapping("/chat/assets/backfill-from-pipeline-runs")
    public Message<Map<String, Object>> backfillAssetsFromPipelineRuns(
            @RequestParam(value = "limit", defaultValue = "50") Integer limit,
            @RequestParam(value = "offset", defaultValue = "0") Integer offset,
            @RequestParam(value = "agentkSessionId", required = false) String agentkSessionId,
            @RequestParam(value = "scope", defaultValue = "all") String scope) {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        int sessionLimit = limit == null || limit <= 0 ? 50 : Math.min(limit, 200);
        int sessionOffset = offset == null || offset < 0 ? 0 : offset;
        Map<String, Object> result = chatAssetSyncService.backfill("pipeline", scope, sessionLimit,
                sessionOffset, StringUtils.hasText(agentkSessionId) ? agentkSessionId.trim() : null, identity);
        result.put("syncedAssetCount", result.get("pipelineSyncedAssetCount"));
        return Message.ok(result);
    }

    /**
     * 能力目录（BFF）：实时调用 agent-k gRPC {@code ListCapabilities}，含 fields / uiSchema。
     * <p>
     * {@code workflowKey} 传 {@code official/default} 返回的 {@code workflowAgents[].workflowKey}
     * （capability id / pipeline ref / 短 name 均可），只返回匹配的一条。
     */
    @GetMapping("/agentk/capabilities")
    public Message<AgentKCapabilityListViewDto> agentkCapabilities(
            @RequestParam(value = "kind", required = false) String kind,
            @RequestParam(value = "workflowKey", required = false) String workflowKey) {
        try {
            LOGGER.info("agentk capabilities query kind={} workflowKey={}", kind, workflowKey);
            Message<AgentKCapabilityListViewDto> result = Message.ok(
                    capabilityCacheService.listFrontendCapabilities(
                            kind, workflowKey, identityResolver.resolve()));
            AgentKCapabilityListViewDto data = result.getData();
            int size = data == null || data.getCapabilities() == null ? 0 : data.getCapabilities().size();
            LOGGER.info("agentk capabilities result kind={} workflowKey={} size={}",
                    kind, workflowKey, size);
            return result;
        } catch (IllegalStateException e) {
            LOGGER.warn("agentk capabilities failed kind={} workflowKey={} err={}",
                    kind, workflowKey, e.getMessage());
            return Message.error(ResultCode.RELY_SERVICE_BUSY, e.getMessage());
        }
    }

    /**
     * 与 {@code POST /agentk/capabilities/refresh} 相同；保留 platform 路径供平台前端调用。
     */
    @PostMapping("/agentk/capabilities/refresh")
    public Message<AgentKCapabilityRefreshResultDto> refreshAgentkCapabilities() {
        return Message.ok(capabilityCacheService.refreshSync(identityResolver.resolve(), true));
    }

    @GetMapping("/chat/sessions/{sessionId:\\d+}/runs")
    public Message<List<ChatRun>> sessionRuns(@PathVariable("sessionId") long sessionId,
            @RequestParam(value = "limit", required = false) Integer limit) {
        return Message.ok(platformChatService.sessionRuns(identityResolver.resolve().getUserId(), sessionId, limit));
    }

    /**
     * 会话下 pipeline 单独执行列表（平台投影，含完整 args/output/status）。
     * 与 agent_loop 的 {@code /runs} 对等。
     */
    @GetMapping("/chat/sessions/{sessionId:\\d+}/pipeline-execs")
    public Message<List<PipelineExec>> sessionPipelineExecs(@PathVariable("sessionId") long sessionId,
            @RequestParam(value = "limit", required = false) Integer limit) {
        return Message.ok(platformChatService.sessionPipelineExecs(
                identityResolver.resolve().getUserId(), sessionId, limit));
    }

    @GetMapping("/chat/pipeline-execs/{id:\\d+}")
    public Message<PipelineExec> pipelineExecDetail(@PathVariable("id") long id) {
        return Message.ok(platformChatService.pipelineExecDetail(identityResolver.resolve().getUserId(), id));
    }

    /**
     * 按 {@code pipeline_name} 分页查 {@code aop_pipeline_exec}（精确匹配）。
     * <p>
     * 例：{@code pipelineName=e-commerce-clothing-dupe}（KEffects taskType）。
     * 普通用户仅本人；超管可看该 pipeline 下全部。默认 page=1、pageSize=20（上限 100）。
     */
    @GetMapping("/chat/pipeline-execs")
    public Message<PageResult<PipelineExec>> pipelineExecsByPipelineName(
            @RequestParam("pipelineName") String pipelineName,
            @RequestParam(value = "page", required = false) Integer page,
            @RequestParam(value = "pageSize", required = false) Integer pageSize) {
        if (!StringUtils.hasText(pipelineName)) {
            return Message.error(ResultCode.PARAM_REQUIRED, "pipelineName is required");
        }
        try {
            return Message.ok(platformChatService.pipelineExecsByPipelineName(
                    identityResolver.resolve().getUserId(), pipelineName, page, pageSize));
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, e.getMessage());
        }
    }

    /**
     * 按 agent-k session id（{@code ses_xxx}）查 {@code aop_pipeline_exec}，
     * 过滤字段 {@code agentk_session_id}。
     */
    @GetMapping("/chat/pipeline-execs/by-agentk/{agentkSessionId}")
    public Message<List<PipelineExec>> pipelineExecsByAgentkSession(
            @PathVariable("agentkSessionId") String agentkSessionId,
            @RequestParam(value = "limit", required = false) Integer limit) {
        return Message.ok(platformChatService.pipelineExecsByAgentkSession(
                identityResolver.resolve().getUserId(), agentkSessionId, limit));
    }

    @GetMapping("/chat/sessions/{sessionId:\\d+}/call-logs")
    public Message<List<AgentkCallLog>> sessionCallLogs(@PathVariable("sessionId") long sessionId,
            @RequestParam(value = "limit", required = false) Integer limit) {
        return Message.ok(platformChatService.sessionCallLogs(
                identityResolver.resolve().getUserId(), sessionId, limit));
    }

    @GetMapping("/chat/runs/{runId:\\d+}")
    public Message<ChatRun> runDetail(@PathVariable("runId") long runId) {
        return Message.ok(platformChatService.runDetail(identityResolver.resolve().getUserId(), runId));
    }

    @GetMapping("/chat/runs/by-agentk/{agentkRunId}")
    public Message<ChatRun> runDetailByAgentk(@PathVariable("agentkRunId") String agentkRunId) {
        return Message.ok(platformChatService.runDetailByAgentk(identityResolver.resolve().getUserId(), agentkRunId));
    }

    @GetMapping("/chat/runs/{runId:\\d+}/events")
    public Message<List<RunEvent>> runEvents(@PathVariable("runId") long runId,
            @RequestParam(value = "limit", required = false) Integer limit) {
        return Message.ok(platformChatService.runEvents(identityResolver.resolve().getUserId(), runId, limit));
    }

    @GetMapping("/chat/runs/by-agentk/{agentkRunId}/events")
    public Message<List<RunEvent>> runEventsByAgentk(@PathVariable("agentkRunId") String agentkRunId,
            @RequestParam(value = "limit", required = false) Integer limit) {
        return Message.ok(platformChatService.runEventsByAgentk(
                identityResolver.resolve().getUserId(), agentkRunId, limit));
    }

    /** 我的 agent-k 调用审计（建会话/发消息等）。 */
    @GetMapping("/chat/call-logs")
    public Message<List<AgentkCallLog>> myCallLogs(@RequestParam(value = "limit", required = false) Integer limit) {
        return Message.ok(platformChatService.myCallLogs(identityResolver.resolve().getUserId(), limit));
    }

    /** 切回会话时由前端上报游标，便于下次续流（需本人 session）。 */
    @PutMapping("/chat/sessions/by-agentk/{agentkSessionId}/cursor")
    public Message<Void> updateCursor(@PathVariable("agentkSessionId") String agentkSessionId,
            @RequestBody StreamCursorUpdateRequest cursorUpdateRequest) {
        platformChatService.updateCursor(identityResolver.resolve().getUserId(), agentkSessionId, cursorUpdateRequest);
        return Message.ok(null);
    }

    /**
     * 创建平台 Agent（逻辑实体，用于挂载 profile 版本）。
     */
    @PostMapping("/agent-loops")
    public Message<Agent> createAgentLoop(@RequestBody CreateAgentLoopRequest createAgentLoopRequest) {
        return Message.ok(platformAgentWorkflowService.createAgentLoop(
                identityResolver.resolve().getUserId(),
                createAgentLoopRequest.getName(),
                createAgentLoopRequest.getDescription(),
                createAgentLoopRequest.getTenantId()));
    }

    @GetMapping("/agent-loops")
    public Message<PageResult<Agent>> listAgentLoops(
            @RequestParam(value = "pageNum", defaultValue = "1") Integer pageNum,
            @RequestParam(value = "pageSize", defaultValue = "20") Integer pageSize) {
        return Message.ok(platformAgentWorkflowService.listAgentLoops(
                identityResolver.resolve().getUserId(), pageNum, pageSize));
    }

    /**
     * 保存 Agent 的 profile 版本草稿（对应 agent-k session profile）。
     * {@code profileYaml} 现为 JSON object（列名历史遗留）。
     * 私有 Agent 仅 owner 可保存；public Agent 是公用底座，允许当前用户基于它保存草稿。
     */
    @PostMapping("/agent-loops/{agentLoopId}/versions")
    public Message<AgentVersion> saveAgentLoopVersion(@PathVariable("agentLoopId") long agentLoopId,
            @RequestBody SaveAgentLoopVersionRequest saveAgentLoopVersionRequest) {
        return Message.ok(platformAgentWorkflowService.saveAgentLoopVersion(
                identityResolver.resolve().getUserId(), agentLoopId, saveAgentLoopVersionRequest));
    }

    @PostMapping("/agent-loop-versions/{agentLoopVersionId}/publish")
    public Message<AgentVersion> publishAgentLoopVersion(
            @PathVariable("agentLoopVersionId") long agentLoopVersionId) {
        return Message.ok(platformAgentWorkflowService.publishAgentLoopVersion(
                identityResolver.resolve().getUserId(), agentLoopVersionId));
    }

    /**
     * 查询单个版本详情（含完整 profile 配置）；draft / published 均可。
     */
    @GetMapping("/agent-loop-versions/{agentLoopVersionId}")
    public Message<AgentVersion> getAgentLoopVersion(
            @PathVariable("agentLoopVersionId") long agentLoopVersionId) {
        return Message.ok(platformAgentWorkflowService.getAgentLoopVersion(
                identityResolver.resolve().getUserId(), agentLoopVersionId));
    }

    /**
     * 列出版本；可选 {@code status=draft|published} 过滤。
     * 列表同样带回 profileYaml（完整配置）。
     */
    @GetMapping("/agent-loops/{agentLoopId}/versions")
    public Message<List<AgentVersion>> listAgentLoopVersions(
            @PathVariable("agentLoopId") long agentLoopId,
            @RequestParam(value = "status", required = false) String status) {
        return Message.ok(platformAgentWorkflowService.listAgentLoopVersions(
                identityResolver.resolve().getUserId(), agentLoopId, status));
    }

    @PostMapping("/workflows")
    public Message<Workflow> createWorkflow(@RequestBody CreateWorkflowRequest createWorkflowRequest) {
        return Message.ok(platformAgentWorkflowService.createWorkflow(
                identityResolver.resolve().getUserId(),
                createWorkflowRequest.getName(),
                createWorkflowRequest.getDescription(),
                createWorkflowRequest.getTenantId()));
    }

    @GetMapping("/workflows")
    public Message<List<Workflow>> listWorkflows() {
        return Message.ok(platformAgentWorkflowService.listWorkflows(identityResolver.resolve().getUserId()));
    }

    /**
     * 保存 Workflow 版本草稿。body 推荐：
     * <pre>{ "definitionJson": { "name": "...", "nodes": {...} } }</pre>
     * 也兼容直接传定义对象本身（含 name/nodes）。
     * 私有 Workflow 仅 owner 可保存；public Workflow 是公用底座，允许当前用户基于它保存草稿。
     */
    @PostMapping("/workflows/{workflowId}/versions")
    public Message<WorkflowVersion> saveWorkflowVersion(@PathVariable("workflowId") long workflowId,
            @RequestBody JsonNode body) {
        return Message.ok(platformAgentWorkflowService.saveWorkflowVersion(
                identityResolver.resolve().getUserId(), workflowId, body));
    }

    @PostMapping("/workflow-versions/{workflowVersionId}/publish")
    public Message<WorkflowVersion> publishWorkflowVersion(
            @PathVariable("workflowVersionId") long workflowVersionId) {
        return Message.ok(platformAgentWorkflowService.publishWorkflowVersion(
                identityResolver.resolve().getUserId(), workflowVersionId));
    }

    @GetMapping("/workflows/{workflowId}/versions")
    public Message<List<WorkflowVersion>> listWorkflowVersions(@PathVariable("workflowId") long workflowId) {
        return Message.ok(platformAgentWorkflowService.listWorkflowVersions(
                identityResolver.resolve().getUserId(), workflowId));
    }

//    @PostMapping("/datasets")
//    public Message<Dataset> createDataset(@RequestBody CreateDatasetRequest createDatasetRequest) {
//        AgentKOutboundIdentity identity = identityResolver.resolve();
//        return Message.ok(datasetPersistService.createDataset(createDatasetRequest.getName(),
//                createDatasetRequest.getDescription(), createDatasetRequest.getKeyColumn(),
//                createDatasetRequest.getInputMappingJson(), identity.getUserId(),
//                empty(createDatasetRequest.getTenantId())));
//    }

//    @GetMapping("/datasets")
//    public Message<List<Dataset>> listDatasets() {
//        return Message.ok(datasetPersistService.listMine(identityResolver.resolve().getUserId()));
//    }
//
//    @PostMapping("/datasets/{datasetId}/snapshots:csv")
//    public Message<DatasetSnapshot> uploadDatasetCsvSnapshot(@PathVariable("datasetId") long datasetId,
//            @RequestBody String csvText) {
//        AgentKOutboundIdentity identity = identityResolver.resolve();
//        return Message.ok(datasetPersistService.uploadCsvSnapshot(datasetId, csvText, identity.getUserId()));
//    }
//
//    @GetMapping("/datasets/{datasetId}/snapshots")
//    public Message<List<DatasetSnapshot>> listDatasetSnapshots(@PathVariable("datasetId") long datasetId) {
//        return Message.ok(datasetPersistService.listSnapshots(datasetId));
//    }
}
