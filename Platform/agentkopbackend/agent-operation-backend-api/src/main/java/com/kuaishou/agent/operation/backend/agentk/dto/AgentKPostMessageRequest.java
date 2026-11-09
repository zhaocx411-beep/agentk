package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import com.fasterxml.jackson.annotation.JsonAlias;

import lombok.Data;

/**
 * {@code POST /agentk/sessions/{id}/messages} 请求体。
 * <p>
 * 出站 agent-k：{@code text} + {@code attachments}。{@code tts}、{@code userInput}
 * 以及顶层商品/模特图字段是平台字段，拼进 text 后再发给上游；落库仍用用户原文 {@code text}。
 */
@Data
public class AgentKPostMessageRequest {

    private String text;

    /** 本轮媒体，对齐 agent-k {@code AgentMessageExecutionInput.attachments}。 */
    private List<AgentKMediaAttachment> attachments;

    /** 可选。合成语音时传引擎 + 音色，转写/翻译不要传。 */
    private AgentKTtsHint tts;

    /**
     * 虚拟试穿结构化输入（推荐）。也接受 JSON 字段名 {@code input}。
     */
    @JsonAlias("input")
    private VirtualTryOnUserInput userInput;

    /** 兼容扁平字段：商品图 URL。{@code userInput} 优先。 */
    private List<String> productImageUrls;

    /** 兼容扁平字段：模特图 URL。{@code userInput} 优先。 */
    private String modelImageUrl;

    /** 兼容扁平字段：image 或 video。{@code userInput} 优先。 */
    private String outputType;
}
