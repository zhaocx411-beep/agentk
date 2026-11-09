package com.kuaishou.agent.operation.backend.agentk.dto;

import lombok.Data;

/**
 * 建 session profile.pipelines 用的 ref/name，来自 {@code aop_agentk_capability}。
 */
@Data
public class AgentKPipelineBindingDto {
    /** 写入 profile.pipelines[].ref，如 pipeline:contrib.storyboard_film_python。 */
    private String pipelineRef;
    /** 写入 profile.pipelines[].name，如 storyboard_film_example。 */
    private String pipelineName;
}
