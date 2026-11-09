package com.kuaishou.agent.operation.backend.controller;

import java.util.List;

import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import com.kuaishou.agent.operation.backend.agentk.AgentKIdentityResolver;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.dto.AdminChatSessionQuery;
import com.kuaishou.agent.operation.backend.platform.dto.ChatCaseReviewView;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;
import com.kuaishou.agent.operation.backend.platform.entity.ChatRun;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.entity.RunEvent;
import com.kuaishou.agent.operation.backend.platform.service.AdminChatReviewService;

/**
 * 管理/资产 owner 回看：按 AgentLoop / Workflow 查看全部用户会话 Case。
 * <p>
 * 访问条件（满足其一）：
 * <ul>
 *   <li>{@code permission-stub=true}（仅本地）</li>
 *   <li>表 {@code aop_platform_admin} 中 status=active 的管理员（当前主方案）</li>
 *   <li>当前用户是查询目标 AgentLoop / Workflow 的 owner</li>
 * </ul>
 */
@RestController
@RequestMapping("/agent/operation/backend/platform/admin/chat")
public class AdminChatReviewController {

    private final AdminChatReviewService adminChatReviewService;
    private final AgentKIdentityResolver identityResolver;

    public AdminChatReviewController(AdminChatReviewService adminChatReviewService,
            AgentKIdentityResolver identityResolver) {
        this.adminChatReviewService = adminChatReviewService;
        this.identityResolver = identityResolver;
    }

    /**
     * 分页列出会话（跨用户，平台库，不调 agent-k）。
     * <p>
     * Body 筛选可空：无版本维度时为管理员全局回看（仅 global admin）；
     * 带 agentLoop / workflow 维度时，admin 或对应资产 owner 可查。
     * 过滤优先级：agentLoopVersionId → workflowVersionId → agentLoopId → workflowId。
     * 另支持 {@code sessionId}：仅 agent-k {@code ses_xxx}。
     * 默认排除 {@code entry_source=offline_batch}；每条回填 {@code lastExecutionId}/{@code lastAgentkRunId}。
     */
    @PostMapping("/sessions/query")
    public Message<PageResult<ChatSession>> listSessions(@RequestBody(required = false) AdminChatSessionQuery body) {
        try {
            return Message.ok(adminChatReviewService.listSessions(identityResolver.resolve().getUserId(), body));
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.BAD_REQUEST, e.getMessage());
        }
    }

    /** 按 agent-k session id（如 ses_xxx）查 runs。 */
    @GetMapping("/sessions/by-agentk/{agentkSessionId}/runs")
    public Message<List<ChatRun>> sessionRunsByAgentk(
            @PathVariable("agentkSessionId") String agentkSessionId,
            @RequestParam(value = "limit", required = false) Integer limit) {
        return Message.ok(adminChatReviewService.sessionRunsByAgentk(
                identityResolver.resolve().getUserId(), agentkSessionId, limit));
    }

    /**
     * 按 agent-k session id 查 Case 聚合。
     * {@code sessionKind=agent_loop|workflow}：不传则用会话自身 {@code sessionKind}。
     * 始终带 {@code events}（会话全部过程事件，seq 升序）；
     * agent_loop 另读 runs/messages；pipelineExecs 与 assets（产出）始终返回。
     */
    @GetMapping("/sessions/by-agentk/{agentkSessionId}/case")
    public Message<ChatCaseReviewView> sessionCaseByAgentk(
            @PathVariable("agentkSessionId") String agentkSessionId,
            @RequestParam(value = "sessionKind", required = false) String sessionKind,
            @RequestParam(value = "messageLimit", required = false) Integer messageLimit,
            @RequestParam(value = "runLimit", required = false) Integer runLimit,
            @RequestParam(value = "callLogLimit", required = false) Integer callLogLimit,
            @RequestParam(value = "eventLimit", required = false) Integer eventLimit) {
        return Message.ok(adminChatReviewService.sessionCaseByAgentk(
                identityResolver.resolve().getUserId(), agentkSessionId, sessionKind,
                messageLimit, runLimit, callLogLimit, eventLimit));
    }

    /**
     * 按 agent-k session id（如 ses_xxx）分页查 {@code aop_run_event}，
     * 直接过滤 {@code agentk_session_id}，seq 倒序。
     */
    @GetMapping("/sessions/by-agentk/{agentkSessionId}/events")
    public Message<PageResult<RunEvent>> sessionEventsByAgentk(
            @PathVariable("agentkSessionId") String agentkSessionId,
            @RequestParam(value = "page", required = false) Integer page,
            @RequestParam(value = "pageSize", required = false) Integer pageSize) {
        return Message.ok(adminChatReviewService.sessionEventsByAgentk(
                identityResolver.resolve().getUserId(), agentkSessionId, page, pageSize));
    }

    /*
     * 与 by-agentk 查询重复（仅入口为平台数字 id）。对外统一走 ses_xxx，整段注释保留。
     *
     * @GetMapping("/sessions/{sessionId:\\d+}/case")
     * public Message<ChatCaseReviewView> sessionCase(@PathVariable("sessionId") long sessionId,
     *         @RequestParam(value = "messageLimit", required = false) Integer messageLimit,
     *         @RequestParam(value = "runLimit", required = false) Integer runLimit,
     *         @RequestParam(value = "callLogLimit", required = false) Integer callLogLimit) {
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     ChatSession session = chatPersistService.requireSession(sessionId);
     *     assertCanReviewSession(identity.getUserId(), session);
     *     ChatCaseReviewView view = new ChatCaseReviewView();
     *     view.setSession(toSessionDetail(session));
     *     view.setRuns(chatPersistService.listRuns(sessionId, runLimit == null ? 100 : runLimit));
     *     view.setMessages(chatPersistService.listMessages(sessionId, messageLimit == null ? 500 : messageLimit));
     *     view.setCallLogs(chatPersistService.listCallLogsBySession(sessionId,
     *             callLogLimit == null ? 100 : callLogLimit));
     *     return Message.ok(view);
     * }
     *
     * @GetMapping("/sessions/{sessionId:\\d+}/runs")
     * public Message<List<ChatRun>> sessionRuns(@PathVariable("sessionId") long sessionId,
     *         @RequestParam(value = "limit", required = false) Integer limit) {
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     ChatSession session = chatPersistService.requireSession(sessionId);
     *     assertCanReviewSession(identity.getUserId(), session);
     *     return Message.ok(chatPersistService.listRuns(sessionId, limit == null ? 100 : limit));
     * }
     */

    @GetMapping("/runs/{runId:\\d+}/events")
    public Message<List<RunEvent>> runEvents(@PathVariable("runId") long runId,
            @RequestParam(value = "limit", required = false) Integer limit) {
        return Message.ok(adminChatReviewService.runEvents(
                identityResolver.resolve().getUserId(), runId, limit));
    }

    /** 按 agent-k run id（如 run_xxx）查事件。 */
    @GetMapping("/runs/by-agentk/{agentkRunId}/events")
    public Message<List<RunEvent>> runEventsByAgentk(@PathVariable("agentkRunId") String agentkRunId,
            @RequestParam(value = "limit", required = false) Integer limit) {
        return Message.ok(adminChatReviewService.runEventsByAgentk(
                identityResolver.resolve().getUserId(), agentkRunId, limit));
    }
}
