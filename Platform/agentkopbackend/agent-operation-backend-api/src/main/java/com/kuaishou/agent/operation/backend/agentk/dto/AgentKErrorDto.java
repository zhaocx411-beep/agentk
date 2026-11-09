package com.kuaishou.agent.operation.backend.agentk.dto;

import lombok.Data;

@Data
public class AgentKErrorDto {

    /** 给调用方看的完整错误；优先保留 gRPC 原文（含 code 前缀）。 */
    private String error;

    /** 上游 gRPC / Connect 状态名，如 INVALID_ARGUMENT、UNAVAILABLE。 */
    private String grpcCode;

    /** 上游 gRPC description，不含包装后的 HTTP/业务码。 */
    private String grpcMessage;

    /** 平台为兼容旧客户端而映射的 HTTP 状态，不是上游真实码。 */
    private Integer wrappedHttp;
}
