package com.kuaishou.agent.operation.backend.agentk.tools;

import java.util.LinkedHashMap;
import java.util.Map;

import lombok.Data;

/**
 * agent-k 可选 pipeline 投影（UI 勾选 + 编译 Profile {@code pipelines[]} 用）。
 * <p>
 * 与工具面不同，pipeline 必须带 {@code ref=pipeline:&lt;capabilityId&gt;}：agent-k 的
 * Profile 契约要求 {@code pipelines[]} 每条同时给 ref 与会话内 name。
 */
@Data
public class AgentkPipelineInfo {

    /** 会话内 canonical name，如 generate_brand_ad。 */
    private String name;
    /** Profile 引用串，如 {@code pipeline:agentk.generate_brand_ad}。 */
    private String ref;
    private String description;
    /** capability id，如 agentk.generate_brand_ad。 */
    private String capabilityId;
    /** agentk = 来自 GET /v1/capabilities；mock = 中后台兜底。 */
    private String source;
    /** 入参 JSON Schema，前端渲染表单、后端做必填校验。 */
    private Map<String, Object> inputSchema = new LinkedHashMap<>();
}
