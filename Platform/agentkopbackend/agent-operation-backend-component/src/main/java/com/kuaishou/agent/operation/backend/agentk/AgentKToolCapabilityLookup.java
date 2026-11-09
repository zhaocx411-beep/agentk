package com.kuaishou.agent.operation.backend.agentk;

import java.util.List;

/**
 * 从 {@code aop_agentk_capability} 缓存按工具名解析裸 Capability ID。
 * 实现在 api 模块（{@code AgentKCapabilityCacheService}）。
 */
public interface AgentKToolCapabilityLookup {

    /**
     * 按 profile tools[].name 查找裸 ID（如 {@code agentk.generate_speech_qwen}）。
     * 未命中返回 null（本地工具如 echo / web_fetch 保持 name-only）。
     */
    String findCapabilityIdByToolName(String toolName);

    /**
     * 缓存中是否存在该裸 Capability ID（用于剥离错误的 {@code agentk.web_fetch} 等伪 ID）。
     */
    boolean hasCapabilityId(String capabilityId);

    /**
     * 已启用的 activity 能力列表，供官方默认 Profile / 工具广场使用。
     */
    List<ActivityTool> listActivityTools();

    /** 广场 / 默认 Profile 共用的轻量条目。 */
    final class ActivityTool {
        private final String name;
        private final String capabilityId;
        private final String description;
        private final String instructions;

        public ActivityTool(String name, String capabilityId, String description, String instructions) {
            this.name = name;
            this.capabilityId = capabilityId;
            this.description = description;
            this.instructions = instructions;
        }

        public String getName() {
            return name;
        }

        public String getCapabilityId() {
            return capabilityId;
        }

        public String getDescription() {
            return description;
        }

        public String getInstructions() {
            return instructions;
        }
    }
}
