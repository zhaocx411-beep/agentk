package com.kuaishou.agent.operation.backend.agentk;

import java.io.IOException;
import java.nio.charset.StandardCharsets;

import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Component;

import com.kuaishou.agent.operation.backend.agentk.grpc.AgentKGrpcExchange;

/**
 * agent-k 出站客户端：统一经 gRPC SDK（KESS → {@code klingai-agentk-api}）。
 * 对平台 Controller 仍暴露 {@link #exchangeJson} 路径形态，内部映射到 protobuf RPC。
 */
@Component
public class AgentKClient {

    private final AgentKGrpcExchange grpcExchange;
    private final AgentKProxyAccessLogger accessLogger;

    public AgentKClient(AgentKGrpcExchange grpcExchange, AgentKProxyAccessLogger accessLogger) {
        this.grpcExchange = grpcExchange;
        this.accessLogger = accessLogger;
    }

    public <T> ResponseEntity<?> exchangeJson(String method, String upstreamPath, String query, Object requestBody,
            Class<T> successType, AgentKOutboundIdentity identity) {
        accessLogger.logJsonRequest(method, upstreamPath, query, identity, serializeForLog(requestBody));
        long started = System.currentTimeMillis();
        ResponseEntity<?> upstream = grpcExchange.exchangeJson(method, upstreamPath, query, requestBody, successType,
                identity);
        accessLogger.logJsonResponse(method, upstreamPath, upstream.getStatusCodeValue(),
                upstream.getBody(), System.currentTimeMillis() - started);
        return upstream;
    }

    public ResponseEntity<String> probeHealth() {
        long started = System.currentTimeMillis();
        ResponseEntity<String> upstream = grpcExchange.probeHealth();
        accessLogger.logJsonResponse("GET", "/healthz", upstream.getStatusCodeValue(),
                upstream.getBody(), System.currentTimeMillis() - started);
        return upstream;
    }

    /**
     * 历史 JSON 透传入口；gRPC 模式下无对应 HTTP 上游，返回 501。
     */
    public void proxyJson(HttpServletRequest request, HttpServletResponse response, String method,
            String upstreamPath, byte[] body, AgentKOutboundIdentity identity) throws IOException {
        writeBadGateway(response, "agent-k JSON proxy is not supported; use gRPC SDK routes");
    }

    static void writeBadGateway(HttpServletResponse response, String message) throws IOException {
        response.setStatus(HttpServletResponse.SC_BAD_GATEWAY);
        response.setCharacterEncoding(StandardCharsets.UTF_8.name());
        response.setContentType("application/json;charset=UTF-8");
        String body = "{\"error\":\"" + escapeJson(message) + "\"}";
        response.getOutputStream().write(body.getBytes(StandardCharsets.UTF_8));
        response.flushBuffer();
    }

    private byte[] serializeForLog(Object requestBody) {
        if (requestBody == null) {
            return new byte[0];
        }
        try {
            if (requestBody instanceof byte[]) {
                return (byte[]) requestBody;
            }
            return requestBody.toString().getBytes(StandardCharsets.UTF_8);
        } catch (Exception e) {
            return new byte[0];
        }
    }

    private static String escapeJson(String value) {
        if (value == null) {
            return "";
        }
        return value.replace("\\", "\\\\").replace("\"", "\\\"");
    }
}
