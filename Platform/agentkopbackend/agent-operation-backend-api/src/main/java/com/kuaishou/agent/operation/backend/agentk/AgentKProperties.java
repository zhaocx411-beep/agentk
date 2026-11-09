package com.kuaishou.agent.operation.backend.agentk;

import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * agent-k 透传层配置。
 */
@ConfigurationProperties(prefix = "agent.operation.backend.agentk")
public class AgentKProperties {

    /**
     * 已废弃：agent-k HTTP 已下线，出站统一 gRPC（KESS {@code klingai-agentk-api}）。
     * 保留字段仅为兼容旧 yml / batch-eval 组件读取。
     */
    private String baseUrl = "http://127.0.0.1:8080";

    private int connectTimeoutMs = 3000;

    /**
     * JSON 接口读超时；SSE 不使用此值。
     */
    private int jsonReadTimeoutMs = 120_000;

    /**
     * gRPC unary RPC 等待超时（毫秒）；SSE {@code WatchEvents} 不受此限制。
     * 不含 {@code InvokeCapability}（见 {@link #grpcInvokeTimeoutMs}）。
     */
    private int grpcReadTimeoutMs = 120_000;

    /**
     * {@code InvokeCapability} 后台 gRPC 等待超时。HTTP 已立即返回 {@code inv_*}，
     * 此超时只约束工作线程：覆盖解析队列（最多约 2 分钟）+ 能力自身上限（文生文 10 分钟 / 生图约 6 分钟）。
     */
    private int grpcInvokeTimeoutMs = 900_000;

    public String getBaseUrl() {
        return baseUrl;
    }

    public void setBaseUrl(String baseUrl) {
        this.baseUrl = baseUrl;
    }

    public int getConnectTimeoutMs() {
        return connectTimeoutMs;
    }

    public void setConnectTimeoutMs(int connectTimeoutMs) {
        this.connectTimeoutMs = connectTimeoutMs;
    }

    public int getJsonReadTimeoutMs() {
        return jsonReadTimeoutMs;
    }

    public void setJsonReadTimeoutMs(int jsonReadTimeoutMs) {
        this.jsonReadTimeoutMs = jsonReadTimeoutMs;
    }

    public int getGrpcReadTimeoutMs() {
        return grpcReadTimeoutMs;
    }

    public void setGrpcReadTimeoutMs(int grpcReadTimeoutMs) {
        this.grpcReadTimeoutMs = grpcReadTimeoutMs;
    }

    public int getGrpcInvokeTimeoutMs() {
        return grpcInvokeTimeoutMs > 0 ? grpcInvokeTimeoutMs : grpcReadTimeoutMs;
    }

    public void setGrpcInvokeTimeoutMs(int grpcInvokeTimeoutMs) {
        this.grpcInvokeTimeoutMs = grpcInvokeTimeoutMs;
    }
}
