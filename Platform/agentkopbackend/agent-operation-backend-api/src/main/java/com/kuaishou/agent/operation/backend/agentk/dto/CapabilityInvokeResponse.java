package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * 瞬态 capability 调用：POST 立即返回 {@code running}；GET /invoke 轮询 status；
 * GET /invoke/output 取最终 output。不创建 AgentK Session。
 */
@Data
public class CapabilityInvokeResponse {

    /**
     * 平台侧幂等键，形如 {@code inv_*}。写入 {@code aop_session.agentk_session_id}
     * 与 {@code aop_pipeline_exec.agentk_exec_id}，不是 AgentK 的 {@code ses_*}。
     */
    private String invocationId;
    /** 平台 {@code aop_session.id}。 */
    private Long platformSessionId;
    private String capabilityId;
    private String status;
    private JsonNode args;
    private JsonNode output;
    private List<AgentKMediaArtifactDto> mediaArtifacts;
    private List<String> imageUrls;
    private List<String> videoUrls;
    /** 与 Lab pipeline-run 相同形状，便于前端复用投影。 */
    private AgentKWorkflowExecDto execution;
}
