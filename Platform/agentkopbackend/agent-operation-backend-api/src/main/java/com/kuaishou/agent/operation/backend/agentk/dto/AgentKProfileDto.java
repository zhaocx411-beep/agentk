package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;
import java.util.Map;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * {@code GET /v1/profile} 响应，与 agent-k demo {@code demoui.getProfile} 对齐。
 * <p>
 * 仅 agentk-demo 提供该上游路由；agentk-api 核心产物无此端点。
 */
@Data
public class AgentKProfileDto {

    /** 模型目录版本（demoui：{@code catalogVersion}）。 */
    private String catalogVersion;
    /**
     * 旧字段：历史 profile YAML 版本标识。当前 demoui 不再下发，保留兼容反序列化。
     */
    private String version;
    /**
     * 旧字段：实例 profile YAML 原文。当前 demoui 不再下发，保留兼容反序列化。
     */
    private String yaml;
    /** 可选 chat 模型名列表（供会话 profile.defaultModel 选择）。 */
    private List<String> models;
    /** 目录级缺省 chat 模型；多 chat 且目录未指定时可为空。 */
    private String defaultModel;
    /** 目录能构造出的工具名列表（builtin + 能力工具）。 */
    private List<String> tools;
    /**
     * 能力工具可选模型列表（仅 generate_video / generate_image / edit_image 等有候选）。
     * key=工具名，value=该工具在模型目录中的候选模型名。
     */
    private Map<String, List<String>> toolModels;
    /**
     * 各能力工具不钉选时的默认模型（目录该 kind 首条）。
     * key=工具名，value=生效模型名。
     */
    private Map<String, String> toolDefaults;
    /**
     * demo 建会话基线文档（JSON object）；核心 API 不下发。
     * 页面以此为起点应用选择器结果。
     */
    private JsonNode baseProfile;
}
