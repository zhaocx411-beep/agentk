package com.kuaishou.agent.operation.backend.controller;

import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import com.kuaishou.agent.operation.backend.agentk.AgentKCapabilityCacheService;
import com.kuaishou.agent.operation.backend.agentk.AgentKCreateSessionService;
import com.kuaishou.agent.operation.backend.agentk.AgentKIdentityResolver;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityListViewDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCreateSessionRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionDto;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.dto.OfficialDefaultCatalogView;
import com.kuaishou.agent.operation.backend.platform.dto.OfficialToolPlazaView;
import com.kuaishou.agent.operation.backend.platform.service.OfficialDemoService;
import com.kuaishou.agent.operation.backend.platform.service.OfficialQuickSessionService;

/**
 * 官方示范资产专用接口层（与个人 Agent/Workflow 列表隔离）。
 * <p>
 * 数据来自种子 {@code is_default=1} 资产；不改动 {@code GET /agent-loops} / {@code GET /workflows}。
 */
@RestController
@RequestMapping("/agent/operation/backend/platform/official")
public class OfficialDemoController {

    private final OfficialDemoService officialDemoService;
    private final OfficialQuickSessionService officialQuickSessionService;
    private final AgentKCreateSessionService createSessionService;
    private final AgentKCapabilityCacheService capabilityCacheService;
    private final AgentKIdentityResolver identityResolver;

    public OfficialDemoController(OfficialDemoService officialDemoService,
            OfficialQuickSessionService officialQuickSessionService,
            AgentKCreateSessionService createSessionService,
            AgentKCapabilityCacheService capabilityCacheService,
            AgentKIdentityResolver identityResolver) {
        this.officialDemoService = officialDemoService;
        this.officialQuickSessionService = officialQuickSessionService;
        this.createSessionService = createSessionService;
        this.capabilityCacheService = capabilityCacheService;
        this.identityResolver = identityResolver;
    }

    /**
     * 官方示范目录：
     * {@code agentLoops[]} = {@code aop_agent is_default=1} 已发布；
     * {@code workflowAgents[]} = {@code aop_workflow is_default=1} 已发布。
     */
    @GetMapping("/default")
    public Message<OfficialDefaultCatalogView> defaultCatalog() {
        return Message.ok(officialDemoService.defaultCatalog());
    }

    /**
     * 工具广场：ListCapabilities 全量（含 activity / workflow），条目带 {@code kind}；
     * 上游失败回读 {@code aop_agentk_capability}。
     */
    @GetMapping("/tools")
    public Message<OfficialToolPlazaView> toolPlaza() {
        try {
            AgentKCapabilityListViewDto caps = capabilityCacheService.listPlazaCapabilities(
                    identityResolver.resolve());
            return Message.ok(officialDemoService.toolPlaza(
                    caps == null ? null : caps.getCapabilities(), null));
        } catch (Exception e) {
            return Message.error(ResultCode.RELY_SERVICE_BUSY,
                    e.getMessage() == null ? "agent-k capabilities fetch failed" : e.getMessage());
        }
    }

    /**
     * 官方快速对话（默认示例 AgentLoop）；body 与 {@code POST /agentk/sessions} 完全一致。
     * 未传的 {@code agentLoopVersionId} / {@code workflowVersionId} / {@code sessionKind} / {@code profile}
     * 由 DB 默认资产补齐（profile 在 createSession 内从已发布版本编译）。
     */
    @PostMapping("/sessions/quick")
    public Message<AgentKSessionDto> quickSession(
            @RequestBody(required = false) AgentKCreateSessionRequest request) {
        return createSessionService.createSession(officialQuickSessionService.buildDefaultQuickRequest(request));
    }

    /**
     * 指定示例 AgentLoop 的快速对话；body 与 {@code POST /agentk/sessions} 一致。
     * path {@code agentLoopId} 对应 DB 默认 Agent；{@code agentLoopVersionId} 未传时用其已发布版本。
     */
    @PostMapping("/sessions/quick/agent-loops/{agentLoopId}")
    public Message<AgentKSessionDto> quickAgentSessionById(
            @PathVariable("agentLoopId") long agentLoopId,
            @RequestBody(required = false) AgentKCreateSessionRequest request) {
        return createSessionService.createSession(
                officialQuickSessionService.buildAgentLoopQuickRequest(agentLoopId, request));
    }

    /**
     * 指定示例工作流的快速对话；body 与 {@code POST /agentk/sessions} 一致。
     * path {@code workflowKey} 匹配官方默认 {@code aop_workflow} 的
     * {@code agentk_id} / {@code definition_json.capability} / {@code definition_json.ref} / 展示名；
     * 服务端 pin 该工作流已发布版本；未传 AgentLoop/profile 时用 Pipeline-only 默认 profile。
     */
    @PostMapping("/sessions/quick/workflows/{workflowKey:.+}")
    public Message<AgentKSessionDto> quickWorkflowSession(
            @PathVariable("workflowKey") String workflowKey,
            @RequestBody(required = false) AgentKCreateSessionRequest request) {
        return createSessionService.createSession(
                officialQuickSessionService.buildWorkflowQuickRequest(workflowKey, request));
    }
}
