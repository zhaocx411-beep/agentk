package com.kuaishou.agent.operation.backend.agentk;

import java.util.Locale;
import java.util.regex.Pattern;

import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Component;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKAppendResultDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKErrorDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKWorkflowExecDto;

/**
 * {@link AgentkOutboundGateway} 的 gRPC 实现：离线跑数与在线 Controller 共用
 * {@link AgentKClient}，不再自建 HTTP 出站。
 * <p>
 * 这里只做「路径 → 响应 DTO 类型」的映射与 JSON 化，RPC 分发在
 * {@code AgentKGrpcExchange}；调用方拿到的仍是 agent-k wire 形态的 JSON。
 */
@Component
public class AgentkGrpcOutboundGateway implements AgentkOutboundGateway {

    /** 离线跑数无登录态，用固定标识写出站访问日志。 */
    private static final AgentKOutboundIdentity OFFLINE_IDENTITY =
            new AgentKOutboundIdentity("batch-eval", "batch-eval", "离线跑数");

    private static final Pattern SESSION_MESSAGES = Pattern.compile("^/sessions/[^/]+/messages$");
    private static final Pattern SESSION_EVENTS = Pattern.compile("^/sessions/[^/]+/events$");
    private static final Pattern SESSION_PIPELINE_RUNS = Pattern.compile("^/sessions/[^/]+/pipeline-runs$");
    private static final Pattern SESSION_PIPELINE_RUN = Pattern.compile("^/sessions/[^/]+/pipeline-runs/[^/]+$");

    private final AgentKClient agentKClient;
    private final ObjectMapper objectMapper;

    public AgentkGrpcOutboundGateway(AgentKClient agentKClient, ObjectMapper objectMapper) {
        this.agentKClient = agentKClient;
        this.objectMapper = objectMapper;
    }

    @Override
    public JsonNode exchange(String method, String path, String query, JsonNode body) {
        Class<?> successType = successType(method, path);
        ResponseEntity<?> response;
        try {
            response = agentKClient.exchangeJson(method, path, query, body, successType, OFFLINE_IDENTITY);
        } catch (RuntimeException e) {
            throw new AgentkOutboundException(0, "调用 agent-k 失败: " + e.getMessage(), e);
        }
        Object payload = response.getBody();
        if (!response.getStatusCode().is2xxSuccessful()) {
            throw new AgentkOutboundException(response.getStatusCodeValue(),
                    "agent-k " + response.getStatusCodeValue() + ": " + errorText(payload));
        }
        return payload == null ? objectMapper.createObjectNode() : objectMapper.valueToTree(payload);
    }

    private static Class<?> successType(String method, String path) {
        String upperMethod = method == null ? "GET" : method.toUpperCase(Locale.ROOT);
        String normalized = path == null ? "" : path;
        if ("GET".equals(upperMethod) && "/capabilities".equals(normalized)) {
            return AgentKCapabilitiesDto.class;
        }
        if ("POST".equals(upperMethod) && "/sessions".equals(normalized)) {
            return AgentKSessionDto.class;
        }
        if ("POST".equals(upperMethod) && SESSION_MESSAGES.matcher(normalized).matches()) {
            return AgentKAppendResultDto.class;
        }
        if ("GET".equals(upperMethod) && SESSION_EVENTS.matcher(normalized).matches()) {
            return AgentKListEventsResponse.class;
        }
        if ("POST".equals(upperMethod) && SESSION_PIPELINE_RUNS.matcher(normalized).matches()) {
            return AgentKWorkflowExecDto.class;
        }
        if ("GET".equals(upperMethod) && SESSION_PIPELINE_RUN.matcher(normalized).matches()) {
            return AgentKWorkflowExecDto.class;
        }
        throw new AgentkOutboundException(404, "agent-k 出站不支持的路径: " + upperMethod + " " + normalized);
    }

    private static String errorText(Object payload) {
        if (payload instanceof AgentKErrorDto) {
            return ((AgentKErrorDto) payload).getError();
        }
        return payload == null ? "" : String.valueOf(payload);
    }
}
