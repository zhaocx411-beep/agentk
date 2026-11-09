package com.kuaishou.agent.operation.backend.controller;

import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import com.kuaishou.agent.operation.backend.agentk.AgentKIdentityResolver;
import com.kuaishou.agent.operation.backend.agentk.AgentKOutboundIdentity;
import com.kuaishou.agent.operation.backend.agentk.AgentKRegressionService;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKRegressionAgentTaskRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKRegressionAgentTaskResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKRegressionWorkflowTaskRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKRegressionWorkflowTaskResponse;
import com.kuaishou.agent.operation.backend.model.Message;

/**
 * agent-k 在线回归编排：同步阻塞直到 run/workflow 终态（或超时），供调度平台 smoke / CI 调用。
 * 对外一律返回平台 {@link Message}，不透传 agent-k 原始 JSON。
 */
@RestController
@RequestMapping("/agent/operation/backend/agentk/regression")
public class AgentKRegressionController {

    private final AgentKRegressionService regressionService;
    private final AgentKIdentityResolver identityResolver;

    public AgentKRegressionController(AgentKRegressionService regressionService,
            AgentKIdentityResolver identityResolver) {
        this.regressionService = regressionService;
        this.identityResolver = identityResolver;
    }

    @PostMapping("/agent-tasks")
    public Message<AgentKRegressionAgentTaskResponse> runAgentTask(
            @RequestBody AgentKRegressionAgentTaskRequest agentTaskRequest) {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        return regressionService.runAgentTask(agentTaskRequest, identity);
    }

    @PostMapping("/pipeline-tasks")
    public Message<AgentKRegressionWorkflowTaskResponse> runPipelineTask(
            @RequestBody AgentKRegressionWorkflowTaskRequest request) {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        return regressionService.runWorkflowTask(request, identity);
    }

    /*
     * 旧路径：workflow → pipeline 后改名为 /pipeline-tasks。先注释保留。
     *
     * @PostMapping("/workflow-tasks")
     * public Message<AgentKRegressionWorkflowTaskResponse> runWorkflowTask(
     *         @RequestBody AgentKRegressionWorkflowTaskRequest request) {
     *     AgentKOutboundIdentity identity = identityResolver.resolve();
     *     return regressionService.runWorkflowTask(request, identity);
     * }
     */
}
