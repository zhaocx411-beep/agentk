package com.kuaishou.agent.operation.backend.platform.service;

import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCreateSessionRequest;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;
import com.kuaishou.agent.operation.backend.platform.support.ChatEntrySources;
import com.kuaishou.agent.operation.backend.platform.support.ChatSessionKinds;
import com.kuaishou.agent.operation.backend.platform.support.ChatSessionTitles;

/**
 * 官方快速建会话：请求体与 {@code POST /agentk/sessions} 的 {@link AgentKCreateSessionRequest} 一致，
 * 未传字段从 DB 默认示范资产（is_default=1）补齐后再走 {@code createSession}。
 * <p>
 * Workflow 入口只 pin {@code aop_workflow} 已发布版本；未传 agentLoopVersionId/profile 时，
 * 建会话走 Pipeline-only 默认 profile（tools=[]），再 merge workflow pipelines。
 */
@Service
public class OfficialQuickSessionService {

    private final OfficialDemoService officialDemoService;
    private final AgentVersionService agentVersionService;

    public OfficialQuickSessionService(OfficialDemoService officialDemoService,
            AgentVersionService agentVersionService) {
        this.officialDemoService = officialDemoService;
        this.agentVersionService = agentVersionService;
    }

    /** {@code POST /official/sessions/quick} */
    public AgentKCreateSessionRequest buildDefaultQuickRequest(AgentKCreateSessionRequest request) {
        AgentKCreateSessionRequest src = emptyToNull(request);
        AgentVersion agentVersion = resolveAgentLoopVersion(src, null);
        return merge(src, agentVersion, ChatSessionKinds.AGENT_LOOP);
    }

    /** {@code POST /official/sessions/quick/agent-loops/{agentLoopId}} */
    public AgentKCreateSessionRequest buildAgentLoopQuickRequest(long agentLoopId,
            AgentKCreateSessionRequest request) {
        AgentKCreateSessionRequest src = emptyToNull(request);
        AgentVersion agentVersion = resolveAgentLoopVersion(src, agentLoopId);
        return merge(src, agentVersion, ChatSessionKinds.AGENT_LOOP);
    }

    /**
     * {@code POST /official/sessions/quick/workflows/{workflowKey}}
     * <p>
     * 只 pin {@code aop_workflow} 已发布版本，不自动挂工具底座 AgentLoop。
     * 未显式传 {@code agentLoopVersionId} / {@code profile} 时，createSession 使用
     * Pipeline-only 默认 profile（tools=[]），再 merge workflow pipelines。
     */
    public AgentKCreateSessionRequest buildWorkflowQuickRequest(String workflowKey,
            AgentKCreateSessionRequest request) {
        AgentKCreateSessionRequest src = emptyToNull(request);
        AgentVersion agentVersion = null;
        if (src.getAgentLoopVersionId() != null && src.getAgentLoopVersionId() > 0) {
            agentVersion = agentVersionService.requirePublished(src.getAgentLoopVersionId());
        }
        AgentKCreateSessionRequest merged = merge(src, agentVersion, ChatSessionKinds.WORKFLOW);
        if (merged.getWorkflowVersionId() == null || merged.getWorkflowVersionId() <= 0) {
            WorkflowVersion workflowVersion = officialDemoService
                    .findDefaultPublishedWorkflowVersion(null, workflowKey)
                    .orElseThrow(() -> new IllegalArgumentException(
                            "未找到官方默认 Workflow（workflowKey=" + workflowKey
                                    + "）。请将 is_default=1 的 Pipeline 发布到 aop_workflow，或显式传 workflowVersionId"));
            merged.setWorkflowVersionId(workflowVersion.getId());
        }
        return merged;
    }

    private AgentVersion resolveAgentLoopVersion(AgentKCreateSessionRequest src, Long pathAgentLoopId) {
        if (src != null && src.getAgentLoopVersionId() != null && src.getAgentLoopVersionId() > 0) {
            return agentVersionService.requirePublished(src.getAgentLoopVersionId());
        }
        if (pathAgentLoopId != null && pathAgentLoopId > 0) {
            return officialDemoService.findDefaultPublishedAgentVersion(pathAgentLoopId, null)
                    .orElseThrow(() -> new IllegalArgumentException(
                            "官方默认 AgentLoop 未找到或无已发布版本: " + pathAgentLoopId));
        }
        return officialDemoService.findDefaultPublishedAgentVersion(null, null)
                .orElseThrow(() -> new IllegalArgumentException(
                        "官方默认 AgentLoop 未配置或无已发布版本"));
    }

    private AgentKCreateSessionRequest merge(AgentKCreateSessionRequest src, AgentVersion agentVersion,
            String pathDefaultSessionKind) {
        AgentKCreateSessionRequest merged = new AgentKCreateSessionRequest();
        merged.setTitle(resolveTitle(src));
        if (src != null && src.getProfile() != null && !src.getProfile().isNull()
                && !src.getProfile().isMissingNode()) {
            merged.setProfile(src.getProfile());
        }

        if (agentVersion != null && agentVersion.getId() != null) {
            merged.setAgentLoopVersionId(agentVersion.getId());
        }
        if (src != null && src.getWorkflowVersionId() != null && src.getWorkflowVersionId() > 0) {
            merged.setWorkflowVersionId(src.getWorkflowVersionId());
        }

        merged.setSessionKind(resolveSessionKind(src, merged, pathDefaultSessionKind));
        merged.setEntrySource(ChatEntrySources.resolve(
                src != null ? src.getEntrySource() : null, null));
        return merged;
    }

    private static String resolveSessionKind(AgentKCreateSessionRequest src,
            AgentKCreateSessionRequest merged, String pathDefaultSessionKind) {
        if (src != null && StringUtils.hasText(src.getSessionKind())) {
            String normalized = ChatSessionKinds.normalize(src.getSessionKind());
            if (normalized == null) {
                throw new IllegalArgumentException(
                        "sessionKind 仅支持 agent_loop / workflow|pipeline / custom，当前值: "
                                + src.getSessionKind().trim());
            }
            return normalized;
        }
        if (StringUtils.hasText(pathDefaultSessionKind)) {
            String normalized = ChatSessionKinds.normalize(pathDefaultSessionKind);
            if (normalized != null) {
                return normalized;
            }
        }
        return ChatSessionKinds.resolve(null, merged.getAgentLoopVersionId(), merged.getWorkflowVersionId());
    }

    private static String resolveTitle(AgentKCreateSessionRequest request) {
        if (request != null && StringUtils.hasText(request.getTitle())) {
            return request.getTitle().trim();
        }
        return ChatSessionTitles.DEFAULT_PLACEHOLDER;
    }

    private static AgentKCreateSessionRequest emptyToNull(AgentKCreateSessionRequest request) {
        return request != null ? request : new AgentKCreateSessionRequest();
    }
}
