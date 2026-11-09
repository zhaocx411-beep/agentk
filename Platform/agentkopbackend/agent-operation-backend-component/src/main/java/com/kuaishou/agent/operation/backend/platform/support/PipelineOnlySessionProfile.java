package com.kuaishou.agent.operation.backend.platform.support;

import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionProfileDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionProfileDto.PipelineEntry;

/**
 * Pipeline 草稿模板（tools=[] + 可选 pipelines）。
 * <p>
 * 供 {@code GET .../session-profiles/templates/pipeline-only}，以及建会话仅 pin
 * {@code workflowVersionId}（官方 Workflow 快速对话）时作为默认 profile。
 */
public final class PipelineOnlySessionProfile {

    public static final String DEFAULT_MODEL = OfficialDefaultSessionProfile.DEFAULT_MODEL;
    public static final String PROFILE_VERSION_LABEL = "pipeline-only-default-v1";
    public static final String TEMPLATE_KEY = "pipeline_only_default";

    private PipelineOnlySessionProfile() {
    }

    public static AgentKSessionProfileDto build() {
        return buildWithPipelines(Collections.emptyList());
    }

    public static AgentKSessionProfileDto buildWithPipelines(List<PipelineEntry> pipelines) {
        AgentKSessionProfileDto profile = new AgentKSessionProfileDto();
        profile.setDefaultModel(DEFAULT_MODEL);
        profile.setSystem(defaultSystemPrompt());
        profile.setTools(new ArrayList<>());
        profile.setToolDefaults(new LinkedHashMap<>());
        profile.setPipelines(pipelines == null ? new ArrayList<>() : new ArrayList<>(pipelines));
        return profile;
    }

    public static JsonNode toJsonNode(ObjectMapper objectMapper) {
        return objectMapper.valueToTree(build());
    }

    public static JsonNode toJsonNode(ObjectMapper objectMapper, List<PipelineEntry> pipelines) {
        return objectMapper.valueToTree(buildWithPipelines(pipelines));
    }

    private static String defaultSystemPrompt() {
        return String.join("\n",
                "你是 pipeline 执行会话助手。本会话以声明的 pipelines 为主能力面，tools 为空。",
                "用户通过平台直接触发 pipeline 时，按暴露的 pipeline 契约接收参数并执行；",
                "不要假设存在未在 profile.pipelines 中声明的流水线。",
                "不启用 Agent 工具循环。执行由调用方通过 StartExecution 显式启动。");
    }
}
