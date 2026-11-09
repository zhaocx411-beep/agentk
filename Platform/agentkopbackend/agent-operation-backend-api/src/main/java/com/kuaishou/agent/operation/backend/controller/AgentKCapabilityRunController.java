package com.kuaishou.agent.operation.backend.controller;

import java.util.List;

import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import com.kuaishou.agent.operation.backend.agentk.CapabilityInvokeService;
import com.kuaishou.agent.operation.backend.agentk.CapabilityRunService;
import com.kuaishou.agent.operation.backend.agentk.dto.CapabilityInvokeResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.CapabilityRunCancelRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.CapabilityRunHistoryItem;
import com.kuaishou.agent.operation.backend.agentk.dto.CapabilityRunRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.CapabilityRunResponse;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;

/**
 * Capability 单次执行（Capability Lab），与平台资产 / 官方对话入口隔离。
 * <p>
 * 标识一律用 agent-k 字符串：{@code capabilityId}（如 {@code agentk.generate_image}）、
 * {@code sessionId}（{@code ses_*}）、{@code executionId}；不用平台数字主键。
 */
@RestController
@RequestMapping("/agent/operation/backend/capabilities")
public class AgentKCapabilityRunController {

    private final CapabilityRunService capabilityRunService;
    private final CapabilityInvokeService capabilityInvokeService;

    public AgentKCapabilityRunController(CapabilityRunService capabilityRunService,
            CapabilityInvokeService capabilityInvokeService) {
        this.capabilityRunService = capabilityRunService;
        this.capabilityInvokeService = capabilityInvokeService;
    }

    /**
     * 启动一次 capability 执行。body.capabilityId 为 agent-k 能力 ID。
     */
    @PostMapping("/runs")
    public Message<CapabilityRunResponse> start(@RequestBody CapabilityRunRequest request) {
        return capabilityRunService.run(request);
    }

    /**
     * 瞬态调用：立即返回 {@code invocationId}（{@code inv_*}），不创建上游 Session。
     * 后台等待 AgentK {@code InvokeCapability}，结束后把 status / output 写入
     * {@code aop_pipeline_exec}。前端 {@code GET /invoke} 轮询状态，{@code GET /invoke/output} 取最终结果。
     */
    @PostMapping("/invoke")
    public Message<CapabilityInvokeResponse> invoke(@RequestBody CapabilityRunRequest request) {
        return capabilityInvokeService.invoke(request);
    }

    /**
     * 轮询 invoke 状态：读 {@code aop_pipeline_exec} 的 status（running/completed/failed），不打 AgentK。
     */
    @GetMapping("/invoke")
    public Message<CapabilityInvokeResponse> getInvoke(
            @RequestParam("invocationId") String invocationId) {
        return capabilityInvokeService.get(invocationId);
    }

    /**
     * 查询最终 output：仅 status=completed 时返回库中的 output / 媒体 URL；
     * 仍在跑返回未就绪；失败返回上游 error。
     */
    @GetMapping("/invoke/output")
    public Message<CapabilityInvokeResponse> getInvokeOutput(
            @RequestParam("invocationId") String invocationId) {
        return capabilityInvokeService.getOutput(invocationId);
    }

    /**
     * Lab 历史：谁、哪个工具、入参 args、出参 output。
     * 普通用户只看自己；超管可传 {@code userId}。{@code capabilityId} 不传则全部工具。
     */
    @GetMapping("/runs/history")
    public Message<PageResult<CapabilityRunHistoryItem>> history(
            @RequestParam(value = "capabilityId", required = false) String capabilityId,
            @RequestParam(value = "userId", required = false) String userId,
            @RequestParam(value = "status", required = false) String status,
            @RequestParam(value = "page", required = false) Integer page,
            @RequestParam(value = "pageSize", required = false) Integer pageSize) {
        return capabilityRunService.listHistory(capabilityId, userId, status, page, pageSize);
    }

    /**
     * 当前可见范围内出现过的工具 ID（如 {@code agentk.edit_image}），用于按工具分组。
     */
    @GetMapping("/runs/history/tools")
    public Message<List<String>> historyTools(
            @RequestParam(value = "userId", required = false) String userId) {
        return capabilityRunService.listHistoryTools(userId);
    }

    /**
     * 回溯一次执行：入参 args、输出 output、status。
     * {@code inv_*} 走平台库（invoke 落库结果），{@code ses_*} 仍打 AgentK。
     * 普通用户只能看自己的；超管可回看任何人（与 history 一致）。
     */
    @GetMapping("/runs")
    public Message<CapabilityRunResponse> get(
            @RequestParam("sessionId") String sessionId,
            @RequestParam("executionId") String executionId) {
        if (isInvokeId(sessionId) || isInvokeId(executionId)) {
            String invocationId = isInvokeId(executionId) ? executionId : sessionId;
            Message<CapabilityInvokeResponse> invoke = capabilityInvokeService.getOutput(invocationId.trim());
            if (invoke.getCode() != ResultCode.SUCCESS.getCode() || invoke.getData() == null) {
                return new Message<>(invoke.getCode(), invoke.getMessage(), null);
            }
            CapabilityInvokeResponse data = invoke.getData();
            CapabilityRunResponse response = new CapabilityRunResponse();
            response.setSessionId(data.getInvocationId());
            response.setExecutionId(data.getInvocationId());
            response.setCapabilityId(data.getCapabilityId());
            response.setPipeline(data.getExecution() == null ? "capability_run" : data.getExecution().getName());
            response.setStatus(data.getStatus());
            response.setExecution(data.getExecution());
            return Message.ok(response);
        }
        return capabilityRunService.get(sessionId, executionId);
    }

    private static boolean isInvokeId(String id) {
        return id != null && id.trim().startsWith("inv_");
    }

    /**
     * 取消该 Lab session 上正在跑的执行。
     * body.sessionId 为启动/历史返回的 agent-k id（{@code ses_xxx}），不是路径参数，也不是平台主键。
     */
    @PostMapping("/runs/cancel")
    public Message<Void> cancel(@RequestBody CapabilityRunCancelRequest request) {
        return capabilityRunService.cancel(request == null ? null : request.getSessionId());
    }
}
