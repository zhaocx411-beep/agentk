package com.kuaishou.agent.operation.backend.agentk.dto;

import com.fasterxml.jackson.annotation.JsonAlias;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * {@code POST /v1/sessions/{id}/pipeline-runs} 请求体。
 * agent-k 现行字段为 {@code args}；兼容旧客户端传 {@code inputs}。
 */
@Data
public class AgentKStartWorkflowRequest {

    private String pipeline;

    @JsonProperty("args")
    @JsonAlias("inputs")
    private JsonNode args;

    /** @deprecated 使用 {@link #getArgs()}；保留给回归编排旧调用。 */
    @Deprecated
    public JsonNode getInputs() {
        return args;
    }

    /** @deprecated 使用 {@link #setArgs(JsonNode)}。 */
    @Deprecated
    public void setInputs(JsonNode inputs) {
        this.args = inputs;
    }
}
