package com.kuaishou.agent.operation.backend.agentk;

import com.fasterxml.jackson.databind.JsonNode;

/**
 * agent-k 出站通道。实现在 api 模块（依赖 agent-k gRPC SDK），离线跑数经此复用在线出站客户端。
 * <p>
 * 路径沿用 agent-k 的 HTTP 资源形态（如 {@code /sessions/{id}/messages}），但<strong>不带
 * {@code /v1} 前缀</strong>——api 侧按该形态分发到对应 protobuf RPC。
 */
public interface AgentkOutboundGateway {

    /**
     * @param method HTTP 语义方法名，GET / POST
     * @param path 资源路径，如 {@code /sessions/ses_1/events}
     * @param query 查询串，如 {@code after=12}；无参传 null
     * @param body 请求体，GET 传 null
     * @return 响应体 JSON；上游无响应体时返回空对象
     * @throws AgentkOutboundException 上游返回非 2xx，或调用本身失败
     */
    JsonNode exchange(String method, String path, String query, JsonNode body);
}
