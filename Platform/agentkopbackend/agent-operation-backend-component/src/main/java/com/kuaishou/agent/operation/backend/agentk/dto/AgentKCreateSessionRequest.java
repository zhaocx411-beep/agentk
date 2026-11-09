package com.kuaishou.agent.operation.backend.agentk.dto;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * 建会话请求：与 agent-k {@code POST /v1/sessions} 字段对齐（title + profile JSON），
 * 另加平台 pin 字段（不传给 agent-k）。
 */
@Data
public class AgentKCreateSessionRequest {

    /** 与 agent-k 一致：会话标题，可空（空则落库「新对话」，可由首问派生或首条消息后再替换）。 */
    private String title;
    /**
     * 平台扩展：建会话时的首问原文。仅用于派生 {@code title}（与首条消息同一套截断规则），
     * 不传给 agent-k，也不会代替 {@code POST .../messages}。
     * 已显式传非空 {@code title} 时忽略本字段。
     */
    private String text;
    /**
     * 与 agent-k 一致：Profile JSON object。
     * <p>
     * 平台侧强类型定义见 {@link AgentKSessionProfileDto}；这里保留 {@link JsonNode}
     * 是为了兼容现有版本发布、官方快速入口与历史 JSON/YAML 组装逻辑。
     */
    private JsonNode profile;

    /**
     * 平台扩展：已发布 AgentLoop 版本 id；未传 profile 时用该版本编译产物填 profile。
     * 不传给 agent-k。
     */
    private Long agentLoopVersionId;
    /**
     * 平台扩展：已发布 Workflow 版本 id；合并进 profile.pipelines。
     * 不传给 agent-k。
     */
    private Long workflowVersionId;
    /**
     * 平台扩展：会话入口类型，落库 {@code session_kind}。
     * 取值：{@code agent_loop} / {@code workflow}（可用别名 {@code pipeline}）/ {@code custom}。
     * 不传则按 pin 推断；两边都 pin 时默认 agent_loop，Pipeline 页务必显式传。
     * 不传给 agent-k。
     */
    private String sessionKind;
    /**
     * 会话入口，落库 {@code entry_source}：只区分 {@code online}（在线，含官方快速入口）
     * 和 {@code offline_batch}（离线跑数）。空或不传按 {@code online}。不传给 agent-k。
     */
    private String entrySource;
}
