package com.kuaishou.agent.operation.backend.controller;

import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import com.kuaishou.agent.operation.backend.agent100.Agent100ConfigService;
import com.kuaishou.agent.operation.backend.agent100.Agent100ExecutionService;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100CatalogView;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100ConfigView;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100ExecutionStartRequest;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100ExecutionView;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100HistoryView;

/**
 * Agent100 BFF。路径按 {@code agentId} 参数化，不在 URL 中写死某一业务 Agent。
 * <p>
 * 列表来自代码注册表；执行复用现有 Session / Pipeline 代理与 {@code aop_pipeline_exec} 落库。
 */
@RestController
@RequestMapping("/agent/operation/backend/platform/agent100")
public class Agent100Controller {

    private final Agent100ExecutionService executionService;
    private final Agent100ConfigService configService;

    public Agent100Controller(Agent100ExecutionService executionService,
            Agent100ConfigService configService) {
        this.executionService = executionService;
        this.configService = configService;
    }

    /** Agent100 模块目录。 */
    @GetMapping("")
    public Message<Agent100CatalogView> catalog() {
        return executionService.catalog();
    }

    /** 配置、模型白名单、Pipeline schema、Capability 就绪状态。 */
    @GetMapping("/{agentId}/config")
    public Message<Agent100ConfigView> config(@PathVariable("agentId") String agentId) {
        return configService.config(agentId);
    }

    /** 启动一条 Pipeline（无 sessionId 时按该 agent 注册的全部 pipelines 建 Session）。 */
    @PostMapping("/{agentId}/executions")
    public Message<Agent100ExecutionView> start(@PathVariable("agentId") String agentId,
            @RequestBody Agent100ExecutionStartRequest request) {
        return executionService.start(agentId, request);
    }

    /**
     * 最近执行历史（{@code aop_pipeline_exec}）。
     * 普通用户只看自己；超管不传 {@code userId} 看全部，传则看指定用户。
     */
    @GetMapping("/{agentId}/executions")
    public Message<Agent100HistoryView> listExecutions(
            @PathVariable("agentId") String agentId,
            @RequestParam(value = "userId", required = false) String userId) {
        return executionService.listHistory(agentId, userId);
    }

    /** 查询一次执行状态 / 事件 / 输出；上游失败时回落本地缓存。 */
    @GetMapping("/{agentId}/executions/{sessionId}/{executionId}")
    public Message<Agent100ExecutionView> getExecution(
            @PathVariable("agentId") String agentId,
            @PathVariable("sessionId") String sessionId,
            @PathVariable("executionId") String executionId) {
        return executionService.getExecution(agentId, sessionId, executionId);
    }
}
