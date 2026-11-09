package com.kuaishou.agent.operation.backend.controller;

import java.io.IOException;

import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.ResponseEntity;
import org.springframework.util.StringUtils;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import com.kuaishou.agent.operation.backend.agentk.ChatAssetSyncService;
import com.kuaishou.agent.operation.backend.agentk.AgentKCapabilityCacheService;
import com.kuaishou.agent.operation.backend.agentk.AgentKClient;
import com.kuaishou.agent.operation.backend.agentk.AgentKCreateSessionService;
import com.kuaishou.agent.operation.backend.agentk.AgentKIdentityResolver;
import com.kuaishou.agent.operation.backend.agentk.AgentKMessageMapper;
import com.kuaishou.agent.operation.backend.agentk.AgentKOutboundIdentity;
import com.kuaishou.agent.operation.backend.agentk.AgentKPaging;
import com.kuaishou.agent.operation.backend.agentk.AgentKSseProxy;
import com.kuaishou.agent.operation.backend.agentk.ChatRunFollowUpService;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKAnswerRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKAppendResultDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCancelRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityRefreshResultDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCreateSessionRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListSessionsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListWorkflowRunsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListWorkflowsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMeDto;
import com.kuaishou.agent.operation.backend.agentk.grpc.AgentKMessageExecutionInputs;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKPostMessageRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKTtsCatalogView;
import com.kuaishou.agent.operation.backend.agentk.tts.AgentKTtsCatalog;
import com.kuaishou.agent.operation.backend.agentk.tryon.VirtualTryOnUserInputComposer;
import com.kuaishou.agent.operation.backend.agentk.tts.AgentKTtsMessageComposer;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKStartWorkflowRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKWorkflowExecDto;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.PostMessageBegin;

/**
 * agent-k 代理入口。JSON 统一包装为 {@link Message}；SSE 为原生 event-stream 透传。
 * <p>
 * 合并两路逻辑：
 * <ul>
 *   <li>出站路径/字段对齐新 agent-k API（sessions events/stream、pipelines、AppendResult）</li>
 *   <li>createSession / postMessage / events / stream 保留平台会话与运行落库</li>
 * </ul>
 * 上游已删除的旧接口整段注释保留，勿直接删。
 */
@RestController
@RequestMapping("/agent/operation/backend/agentk")
public class AgentKProxyController {

    private static final Logger LOGGER = LoggerFactory.getLogger(AgentKProxyController.class);

    private final AgentKClient agentKClient;
    private final AgentKSseProxy agentKSseProxy;
    private final AgentKIdentityResolver identityResolver;
    private final AgentKMessageMapper messageMapper;
    private final ChatPersistService chatPersistService;
    private final ChatRunFollowUpService chatRunFollowUpService;
    private final AgentKCapabilityCacheService capabilityCacheService;
    private final ChatAssetSyncService chatAssetSyncService;
    private final AgentKCreateSessionService createSessionService;

    public AgentKProxyController(AgentKClient agentKClient, AgentKSseProxy agentKSseProxy,
            AgentKIdentityResolver identityResolver, AgentKMessageMapper messageMapper,
            ChatPersistService chatPersistService,
            ChatRunFollowUpService chatRunFollowUpService,
            AgentKCapabilityCacheService capabilityCacheService,
            ChatAssetSyncService chatAssetSyncService,
            AgentKCreateSessionService createSessionService) {
        this.agentKClient = agentKClient;
        this.agentKSseProxy = agentKSseProxy;
        this.identityResolver = identityResolver;
        this.messageMapper = messageMapper;
        this.chatPersistService = chatPersistService;
        this.chatRunFollowUpService = chatRunFollowUpService;
        this.capabilityCacheService = capabilityCacheService;
        this.chatAssetSyncService = chatAssetSyncService;
        this.createSessionService = createSessionService;
    }

    @PostMapping("/sessions")
    public Message<AgentKSessionDto> createSession(
            @RequestBody(required = false) AgentKCreateSessionRequest createSessionRequest) {
        return createSessionService.createSession(createSessionRequest);
    }

    /**
     * 会话历史列表（keyset）：{@code limit} 缺省 20、上限 100；{@code before} 为上一页末尾 sessionId。
     */
    @GetMapping("/sessions")
    public Message<AgentKListSessionsResponse> listSessions(
            @RequestParam(value = "limit", required = false) Integer limit,
            @RequestParam(value = "before", required = false) String before) {
        String query = AgentKPaging.sessionsQuery(limit, before);
        if (query == null) {
            return Message.error(ResultCode.BAD_REQUEST, "limit must be a positive integer");
        }
        AgentKOutboundIdentity identity = identityResolver.resolve();
        ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/sessions", query, null,
                AgentKListSessionsResponse.class, identity);
        return messageMapper.toMessage(upstream, AgentKListSessionsResponse.class);
    }

    /**
     * TTS 引擎与音色下拉。前端只展示 {@code displayName}，发消息时回传 {@code engine} + {@code voiceId}。
     */
    @GetMapping("/tts/voices")
    public Message<AgentKTtsCatalogView> listTtsVoices() {
        return Message.ok(AgentKTtsCatalog.view());
    }

    @PostMapping("/sessions/{sessionId}/messages")
    public Message<AgentKAppendResultDto> postMessage(@PathVariable("sessionId") String sessionId,
            @RequestBody AgentKPostMessageRequest postMessageRequest) {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        String userText = postMessageRequest != null ? postMessageRequest.getText() : null;
        AgentKPostMessageRequest outbound = new AgentKPostMessageRequest();
        try {
            outbound.setText(AgentKTtsMessageComposer.compose(
                    VirtualTryOnUserInputComposer.apply(postMessageRequest)));
            if (postMessageRequest != null) {
                outbound.setAttachments(postMessageRequest.getAttachments());
            }
            AgentKMessageExecutionInputs.from(outbound);
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, e.getMessage());
        }
        PostMessageBegin begin = null;
        try {
            ChatSession session = chatPersistService.findByAgentkSessionId(sessionId).orElse(null);
            if (session != null) {
                // 平台已登记的 session：必须本人才能继续发消息（权限隔离）
                chatPersistService.requireOwnedByAgentkSessionId(sessionId, identity.getUserId());
                begin = chatPersistService.beginPostMessage(session, userText);
            }
        } catch (com.kuaishou.agent.operation.backend.platform.support.PlatformAccessException e) {
            return Message.error(ResultCode.FORBIDDEN, e.getMessage());
        } catch (Exception e) {
            LOGGER.warn("platform persist beginPostMessage failed sessionId={}", sessionId, e);
        }

        // 新 API 返回 AppendResult，不再返回 Run
        ResponseEntity<?> upstream = agentKClient.exchangeJson("POST", "/sessions/" + sessionId + "/messages", null,
                outbound, AgentKAppendResultDto.class, identity);
        Message<AgentKAppendResultDto> postMessageResponse =
                messageMapper.toMessage(upstream, AgentKAppendResultDto.class);

        if (begin != null) {
            try {
                boolean ok = isOk(postMessageResponse) && postMessageResponse.getData() != null;
                String executionId = ok ? postMessageResponse.getData().getExecutionId() : null;
                String executionStatus = ok && postMessageResponse.getData() != null
                        ? postMessageResponse.getData().getStatus()
                        : null;
                chatPersistService.completePostMessage(begin,
                        executionId,
                        ok ? executionStatus : null,
                        ok,
                        upstream.getStatusCodeValue(),
                        ok ? null : postMessageResponse.getMessage());
                if (ok && StringUtils.hasText(executionId)) {
                    chatRunFollowUpService.scheduleAfterPostMessage(sessionId, executionId, identity);
                }
            } catch (Exception e) {
                LOGGER.warn("platform persist completePostMessage failed sessionId={}", sessionId, e);
            }
        }
        return postMessageResponse;
    }

    @PostMapping("/sessions/{sessionId}/answers")
    public Message<AgentKAppendResultDto> answerQuestions(@PathVariable("sessionId") String sessionId,
            @RequestBody AgentKAnswerRequest request) {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        ResponseEntity<?> upstream = agentKClient.exchangeJson("POST", "/sessions/" + sessionId + "/answers", null,
                request, AgentKAppendResultDto.class, identity);
        return messageMapper.toMessage(upstream, AgentKAppendResultDto.class);
    }

    @PostMapping("/sessions/{sessionId}/cancel")
    public Message<Void> cancelSession(@PathVariable("sessionId") String sessionId,
            @RequestBody(required = false) AgentKCancelRequest request) {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        ResponseEntity<?> upstream = agentKClient.exchangeJson("POST", "/sessions/" + sessionId + "/cancel", null,
                request != null ? request : new AgentKCancelRequest(), Void.class, identity);
        return messageMapper.toMessage(upstream, Void.class);
    }

    /*
     * agent-k 已删除 GET /sessions/{id}/items（改用会话事件流）。整段注释保留，勿直接删。
     *
     * @GetMapping("/sessions/{sessionId}/items")
     * public Message<AgentKListItemsResponse> listItems(@PathVariable("sessionId") String sessionId,
     *         @RequestParam(value = "after", required = false) Long after,
     *         @RequestParam(value = "limit", required = false) Integer limit) {
     *     String query = AgentKPaging.itemsQuery(after, limit);
     *     if (query == null) {
     *         return Message.error(ResultCode.BAD_REQUEST,
     *                 "after must be a non-negative integer; limit must be a positive integer");
     *     }
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/sessions/" + sessionId + "/items",
     *             query, null, AgentKListItemsResponse.class, identity);
     *     return messageMapper.toMessage(upstream, AgentKListItemsResponse.class);
     * }
     */

    /*
     * agent-k 已将 Run 事件账本改为会话级 GET /sessions/{id}/events。旧接口整段注释保留，勿直接删。
     *
     * @GetMapping("/runs/{runId}/events")
     * public Message<AgentKListEventsResponse> listEvents(@PathVariable("runId") String runId,
     *         @RequestParam(value = "after", required = false) Long after) {
     *     String query = AgentKPaging.eventsQuery(after);
     *     if (query == null) {
     *         return Message.error(ResultCode.BAD_REQUEST, "after must be a non-negative integer");
     *     }
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/runs/" + runId + "/events",
     *             query, null, AgentKListEventsResponse.class, identity);
     *     Message<AgentKListEventsResponse> message = messageMapper.toMessage(upstream, AgentKListEventsResponse.class);
     *     if (isOk(message) && message.getData() != null) {
     *         tryUpdateCursorFromEvents(runId, message.getData());
     *         AgentKRunDto run = message.getData().getRun();
     *         if (run != null && StringUtils.hasText(run.getSessionId())) {
     *             chatRunFollowUpService.syncAfterStream(run.getSessionId(), runId, identity);
     *         }
     *     }
     *     return message;
     * }
     */

    /**
     * 会话事件账本：{@code after} 缺省 0；{@code limit} 缺省 500、上限 2000。
     * 成功后回写平台 stream cursor，并触发闭环 sync。
     */
    @GetMapping("/sessions/{sessionId}/events")
    public Message<AgentKListEventsResponse> listEvents(@PathVariable("sessionId") String sessionId,
            @RequestParam(value = "after", required = false) Long after,
            @RequestParam(value = "limit", required = false) Integer limit) {
        String query = AgentKPaging.eventsQuery(after, limit);
        if (query == null) {
            return Message.error(ResultCode.BAD_REQUEST,
                    "after must be a non-negative integer; limit must be a positive integer");
        }
        AgentKOutboundIdentity identity = identityResolver.resolve();
        ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/sessions/" + sessionId + "/events",
                query, null, AgentKListEventsResponse.class, identity);
        Message<AgentKListEventsResponse> message = messageMapper.toMessage(upstream, AgentKListEventsResponse.class);
        if (isOk(message) && message.getData() != null) {
            tryUpdateCursorFromEvents(sessionId, message.getData());
            chatRunFollowUpService.syncAfterStream(sessionId, null, identity);
        }
        return message;
    }

    /*
     * agent-k 已将 SSE 改为会话级 GET /sessions/{id}/stream。旧接口整段注释保留，勿直接删。
     *
     * @GetMapping("/runs/{runId}/stream")
     * public void streamRun(@PathVariable("runId") String runId,
     *         @RequestParam(value = "after", required = false) String after,
     *         HttpServletRequest httpServletRequest, HttpServletResponse httpServletResponse) throws IOException {
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     String query = httpServletRequest.getQueryString();
     *     if (!StringUtils.hasText(query) && StringUtils.hasText(after)) {
     *         query = "after=" + after;
     *     }
     *     try {
     *         agentKSseProxy.proxyStream(httpServletRequest, httpServletResponse, "/runs/" + runId + "/stream",
     *                 query, identity);
     *     } finally {
     *         // sessionId 由 /events 响应里的 run.sessionId 回填
     *         chatRunFollowUpService.syncAfterStream(null, runId, identity);
     *     }
     * }
     */

    /**
     * SSE 订流（会话级），原生 event-stream 透传；结束后触发平台闭环 sync。
     */
    @GetMapping("/sessions/{sessionId}/stream")
    public void streamSession(@PathVariable("sessionId") String sessionId,
            @RequestParam(value = "after", required = false) String after,
            HttpServletRequest httpServletRequest, HttpServletResponse httpServletResponse) throws IOException {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        String query = httpServletRequest.getQueryString();
        if (!StringUtils.hasText(query) && StringUtils.hasText(after)) {
            query = "after=" + after;
        }
        try {
            agentKSseProxy.proxyStream(httpServletRequest, httpServletResponse,
                    "/sessions/" + sessionId + "/stream", query, identity);
        } finally {
            chatRunFollowUpService.syncAfterStream(sessionId, null, identity);
        }
    }

    /*
     * agent-k 已删除 GET /runs/{runId}/trace。整段注释保留，勿直接删。
     *
     * @GetMapping("/runs/{runId}/trace")
     * public Message<AgentKTraceDto> exportTrace(@PathVariable("runId") String runId) {
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/runs/" + runId + "/trace", null, null,
     *             AgentKTraceDto.class, identity);
     *     return messageMapper.toMessage(upstream, AgentKTraceDto.class);
     * }
     */

    @GetMapping("/me")
    public Message<AgentKMeDto> whoAmI() {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/me", null, null, AgentKMeDto.class, identity);
        return messageMapper.toMessage(upstream, AgentKMeDto.class);
    }

    /**
     * 能力发现（原始契约）：实时拉 agent-k gRPC {@code ListCapabilities}。
     * <p>
     * 供核对 toolRef / pipelineRef、Profile 组装；前端表单渲染请用
     * {@code GET /platform/agentk/capabilities}（含 fields / uiSchema）。
     */
    @GetMapping("/capabilities")
    public Message<AgentKCapabilitiesDto> listCapabilities() {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        try {
            return Message.ok(capabilityCacheService.listRawCapabilities(identity));
        } catch (IllegalStateException e) {
            return Message.error(ResultCode.RELY_SERVICE_BUSY, e.getMessage());
        }
    }

    /**
     * 强制从 agent-k 拉 capabilities 并写 {@code aop_agentk_capability}。
     * 已从登录拦截放行，无票据也可调；无登录态时出站身份回退 admin。
     */
    @PostMapping("/capabilities/refresh")
    public Message<AgentKCapabilityRefreshResultDto> refreshCapabilities() {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        return Message.ok(capabilityCacheService.refreshSync(identity, true));
    }

    /*
     * agent-k gRPC 无对应 RPC，整段注释保留，勿直接删。
     *
     * demo 能力面回显：透传 agentk-demo 的 GET /v1/profile（模型目录 / 工具素材 / baseProfile）。
     * 仅 agentk-demo 提供该上游路由；连 agentk-api 时会 404。平台建会话主路径仍推荐
     * agentLoopVersionId，本接口留给 demo / 本地联调选择器。
     *
     * @GetMapping("/profile")
     * public Message<AgentKProfileDto> getProfile() {
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/profile", null, null,
     *             AgentKProfileDto.class, identity);
     *     return messageMapper.toMessage(upstream, AgentKProfileDto.class);
     * }
     *
     * 实例级工具目录：透传 agentk-demo GET /v1/tools/catalog（name / description / inputSchema）。
     * 比 getProfile() 的 tools[] 更完整，供平台工具广场与参数表单使用。
     * 仅 agentk-demo 提供上游；连 agentk-api 时会 404。
     *
     * @GetMapping("/tools/catalog")
     * public Message<AgentKToolsCatalogDto> getToolsCatalog() {
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/tools/catalog", null, null,
     *             AgentKToolsCatalogDto.class, identity);
     *     return messageMapper.toMessage(upstream, AgentKToolsCatalogDto.class);
     * }
     *
     * @GetMapping("/health")
     * public Message<AgentKHealthDto> probeHealth() {
     *     long started = System.currentTimeMillis();
     *     ResponseEntity<String> upstream = agentKClient.probeHealth();
     *     AgentKHealthDto dto = new AgentKHealthDto();
     *     dto.setLatencyMs(System.currentTimeMillis() - started);
     *     dto.setAgentkReachable(upstream.getStatusCode().is2xxSuccessful());
     *     dto.setStatus(StringUtils.hasText(upstream.getBody()) ? upstream.getBody().trim() : "");
     *     if (upstream.getStatusCode().is2xxSuccessful()) {
     *         return Message.ok(dto);
     *     }
     *     return Message.error(ResultCode.RELY_SERVICE_BUSY, "agent-k health check failed", dto);
     * }
     */

    /*
     * agent-k 已将 workflows 重命名为 pipelines。旧接口整段注释保留，勿直接删。
     *
     * @GetMapping("/sessions/{sessionId}/workflows")
     * public Message<AgentKListWorkflowsResponse> listWorkflows(@PathVariable("sessionId") String sessionId) {
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/sessions/" + sessionId + "/workflows", null,
     *             null, AgentKListWorkflowsResponse.class, identity);
     *     return messageMapper.toMessage(upstream, AgentKListWorkflowsResponse.class);
     * }
     *
     * @GetMapping("/sessions/{sessionId}/workflow-runs")
     * public Message<AgentKListWorkflowRunsResponse> listWorkflowRuns(@PathVariable("sessionId") String sessionId) {
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/sessions/" + sessionId + "/workflow-runs",
     *             null, null, AgentKListWorkflowRunsResponse.class, identity);
     *     return messageMapper.toMessage(upstream, AgentKListWorkflowRunsResponse.class);
     * }
     *
     * @PostMapping("/sessions/{sessionId}/workflow-runs")
     * public Message<AgentKWorkflowExecDto> startWorkflow(@PathVariable("sessionId") String sessionId,
     *         @RequestBody AgentKStartWorkflowRequest startWorkflowRequest) {
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     ResponseEntity<?> upstream = agentKClient.exchangeJson("POST", "/sessions/" + sessionId + "/workflow-runs",
     *             null, startWorkflowRequest, AgentKWorkflowExecDto.class, identity);
     *     return messageMapper.toMessage(upstream, AgentKWorkflowExecDto.class);
     * }
     *
     * @GetMapping("/workflow-runs/{execId}")
     * public Message<AgentKWorkflowExecDto> getWorkflowRun(@PathVariable("execId") String execId) {
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/workflow-runs/" + execId, null, null,
     *             AgentKWorkflowExecDto.class, identity);
     *     return messageMapper.toMessage(upstream, AgentKWorkflowExecDto.class);
     * }
     */

    @GetMapping("/sessions/{sessionId}/pipelines")
    public Message<AgentKListWorkflowsResponse> listPipelines(@PathVariable("sessionId") String sessionId) {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/sessions/" + sessionId + "/pipelines", null,
                null, AgentKListWorkflowsResponse.class, identity);
        return messageMapper.toMessage(upstream, AgentKListWorkflowsResponse.class);
    }

    /**
     * 会话下 pipeline 执行列表（全量，上游暂无分页）。
     */
    @GetMapping("/sessions/{sessionId}/pipeline-runs")
    public Message<AgentKListWorkflowRunsResponse> listPipelineRuns(@PathVariable("sessionId") String sessionId) {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/sessions/" + sessionId + "/pipeline-runs",
                null, null, AgentKListWorkflowRunsResponse.class, identity);
        Message<AgentKListWorkflowRunsResponse> response =
                messageMapper.toMessage(upstream, AgentKListWorkflowRunsResponse.class);
        if (response.getData() != null) {
            enrichPipelineRuns(response.getData());
            persistPipelineRunsAssets(sessionId, response.getData());
        }
        return response;
    }

    @PostMapping("/sessions/{sessionId}/pipeline-runs")
    public Message<AgentKWorkflowExecDto> startPipeline(@PathVariable("sessionId") String sessionId,
            @RequestBody AgentKStartWorkflowRequest request) {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        if (request == null) {
            request = new AgentKStartWorkflowRequest();
        }
        // 前端常传 workflowKey/capability id；agent-k 只认 profile.pipelines[].name
        String rawPipeline = request.getPipeline();
        String resolvedPipeline = capabilityCacheService.resolveStartPipelineName(rawPipeline);
        if (StringUtils.hasText(resolvedPipeline) && !resolvedPipeline.equals(rawPipeline)) {
            LOGGER.info("startPipeline normalize sessionId={} pipeline {} -> {}",
                    sessionId, rawPipeline, resolvedPipeline);
            request.setPipeline(resolvedPipeline);
        }
        ResponseEntity<?> upstream = agentKClient.exchangeJson("POST", "/sessions/" + sessionId + "/pipeline-runs",
                null, request, AgentKWorkflowExecDto.class, identity);
        Message<AgentKWorkflowExecDto> response = messageMapper.toMessage(upstream, AgentKWorkflowExecDto.class);
        AgentKWorkflowExecDto data = response.getData();
        if (data != null) {
            fillPipelineRunFromRequest(data, request);
            enrichPipelineRun(data);
            persistPipelineExecAndAssets(sessionId, data);
        }
        return response;
    }

    /**
     * 查询单次 pipeline 执行。agent-k 现为会话下路径：
     * {@code GET /v1/sessions/{sessionId}/pipeline-runs/{execId}}。
     */
    @GetMapping("/sessions/{sessionId}/pipeline-runs/{execId}")
    public Message<AgentKWorkflowExecDto> getPipelineRun(@PathVariable("sessionId") String sessionId,
            @PathVariable("execId") String execId) {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        ResponseEntity<?> upstream = agentKClient.exchangeJson("GET",
                "/sessions/" + sessionId + "/pipeline-runs/" + execId, null, null,
                AgentKWorkflowExecDto.class, identity);
        Message<AgentKWorkflowExecDto> response = messageMapper.toMessage(upstream, AgentKWorkflowExecDto.class);
        AgentKWorkflowExecDto data = response.getData();
        if (data != null) {
            enrichPipelineRun(data);
            persistPipelineExecAndAssets(sessionId, data);
        }
        return response;
    }

    /*
     * agent-k 已将 GET /pipeline-runs/{execId} 改为会话下路径。旧接口整段注释保留，勿直接删。
     *
     * @GetMapping("/pipeline-runs/{execId}")
     * public Message<AgentKWorkflowExecDto> getPipelineRun(@PathVariable("execId") String execId) {
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/pipeline-runs/" + execId, null, null,
     *             AgentKWorkflowExecDto.class, identity);
     *     return messageMapper.toMessage(upstream, AgentKWorkflowExecDto.class);
     * }
     */

    /*
     * agent-k 已删除 GET /sessions/{id}/jobs（改看 job.* 事件）。整段注释保留，勿直接删。
     *
     * @GetMapping("/sessions/{sessionId}/jobs")
     * public Message<AgentKListJobsResponse> listJobs(@PathVariable("sessionId") String sessionId,
     *         @RequestParam(value = "status", required = false) String status) {
     *     String query = AgentKPaging.jobsQuery(status);
     *     if (query == null) {
     *         return Message.error(ResultCode.BAD_REQUEST, "status must be pending or all");
     *     }
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/sessions/" + sessionId + "/jobs",
     *             query, null, AgentKListJobsResponse.class, identity);
     *     return messageMapper.toMessage(upstream, AgentKListJobsResponse.class);
     * }
     */

    private void enrichPipelineRuns(AgentKListWorkflowRunsResponse data) {
        if (data == null || data.getPipelineRuns() == null) {
            return;
        }
        for (AgentKWorkflowExecDto run : data.getPipelineRuns()) {
            chatAssetSyncService.enrichPipelineRun(run);
        }
    }

    private void enrichPipelineRun(AgentKWorkflowExecDto run) {
        chatAssetSyncService.enrichPipelineRun(run);
    }

    private void persistPipelineRunsAssets(String sessionId, AgentKListWorkflowRunsResponse data) {
        if (data == null || data.getPipelineRuns() == null) {
            return;
        }
        chatAssetSyncService.syncPipelineExecAndAssetsBatch(sessionId, data.getPipelineRuns());
    }

    private void persistPipelineExecAndAssets(String sessionId, AgentKWorkflowExecDto run) {
        chatAssetSyncService.syncPipelineExecAndAssets(sessionId, run);
    }

    /** POST 响应若缺 args/name，用请求体补齐，保证平台能记下完整入参。 */
    private static void fillPipelineRunFromRequest(AgentKWorkflowExecDto run, AgentKStartWorkflowRequest request) {
        if (run == null || request == null) {
            return;
        }
        if (run.getArgs() == null || run.getArgs().isNull() || run.getArgs().isMissingNode()) {
            run.setArgs(request.getArgs());
        }
        if (!StringUtils.hasText(run.getName()) && StringUtils.hasText(request.getPipeline())) {
            run.setName(request.getPipeline().trim());
        }
    }

    private void tryUpdateCursorFromEvents(String sessionId, AgentKListEventsResponse data) {
        int maxSeq = 0;
        if (data.getEvents() != null) {
            for (AgentKListEventsResponse.AgentKEventDto event : data.getEvents()) {
                Integer eventSeq = ChatRunFollowUpService.toIntSeqOrNull(event.getSeq());
                if (eventSeq != null && eventSeq > maxSeq) {
                    maxSeq = eventSeq;
                }
            }
        }
        String status = ChatRunFollowUpService.inferTurnStatus(data.getEvents());
        chatPersistService.updateStreamCursor(sessionId, null, maxSeq, status);
    }

    /*
     * 旧 Run 事件游标回写（配合 /runs/{runId}/events）。整段注释保留，勿直接删。
     *
     * private void tryUpdateCursorFromEvents(String runId, AgentKListEventsResponse data) {
     *     if (data.getRun() == null || !StringUtils.hasText(data.getRun().getSessionId())) {
     *         return;
     *     }
     *     int maxSeq = 0;
     *     if (data.getEvents() != null) {
     *         for (AgentKListEventsResponse.AgentKEventDto event : data.getEvents()) {
     *             if (event.getSeq() != null && event.getSeq() > maxSeq) {
     *                 maxSeq = event.getSeq().intValue();
     *             }
     *         }
     *     }
     *     String status = data.getRun().getStatus();
     *     chatPersistService.updateStreamCursor(data.getRun().getSessionId(), runId, maxSeq, status);
     * }
     */

    private static boolean isOk(Message<?> message) {
        return message != null && message.getCode() == ResultCode.SUCCESS.getCode();
    }
}
