package com.kuaishou.agent.operation.backend.agentk.dto;

import org.springframework.util.StringUtils;

import lombok.Data;

/**
 * 发消息时的 TTS 选择（平台字段，不传给 agent-k）。
 * <p>
 * {@code engine}：{@code qwen} / {@code minimax}；不传 {@code voiceId} 时用该引擎默认音色。
 */
@Data
public class AgentKTtsHint {

    private String engine;

    private String voiceId;

    public boolean isPresent() {
        return StringUtils.hasText(engine) || StringUtils.hasText(voiceId);
    }
}
