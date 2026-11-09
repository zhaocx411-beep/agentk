package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;
import java.util.Map;

import lombok.Data;

/**
 * 会话级窄选择，与 agent-k {@code types.Spec} 对齐。
 */
@Data
public class AgentKSpecDto {

    private String model;
    private List<String> tools;
    /**
     * 能力工具钉选模型；优先级高于 profile 默认。
     * 只传已启用且需要覆盖的工具；「跟随配置」则不要写入对应 key。
     */
    private Map<String, String> toolModels;
}
