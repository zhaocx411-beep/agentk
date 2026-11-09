package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;
import java.util.Map;

import com.fasterxml.jackson.annotation.JsonInclude;

import lombok.Data;

/**
 * 平台侧 Session Profile 强类型定义，对齐 agent-k {@code internal/profile/profile.go}。
 * <p>
 * 创建 session 的请求仍使用 JsonNode 承接历史 JSON/YAML，但默认 Profile、版本入库和接口文档统一按此结构生成。
 */
@Data
@JsonInclude(JsonInclude.Include.NON_NULL)
public class AgentKSessionProfileDto {

    /** 主 agent 模型名；为空时 agent-k 使用模型目录默认值。 */
    private String defaultModel;
    /** 主 agent 的 system prompt；agent-k 服务端不持有默认 system。 */
    private String system;
    /** 主 agent 启用的工具，按顺序提供给模型。 */
    private List<ToolEntry> tools;
    /** 按工具名配置当前 session 的默认入参；模型显式参数优先。 */
    private Map<String, Map<String, Object>> toolDefaults;
    /** 低代码 HTTP 工具声明。 */
    private List<HttpToolDecl> httpTools;
    /** 向当前 session 暴露的 Pipeline Catalog ref。 */
    private List<PipelineEntry> pipelines;

    @Data
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ToolEntry {
        /** agent-k 现行字段：裸 Capability ID，如 {@code agentk.read_file}。 */
        private String capability;
        /** @deprecated 入站兼容旧 {@code tool:*}；出站优先写 {@link #capability}。 */
        private String ref;
        private String name;
        private String instructions;
    }

    @Data
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class HttpToolDecl {
        private String name;
        private String description;
        private Map<String, Object> inputSchema;
        private HttpDecl http;
        private AsyncDecl async;
    }

    @Data
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class HttpDecl {
        private String url;
        private String method;
        private Integer timeoutSeconds;
    }

    @Data
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class AsyncDecl {
        private String taskIdField;
        private String wakePolicy;
        private PollDecl poll;
        private String statusField;
        private List<String> doneValues;
        private List<String> failedValues;
        private String resultField;
        private String errorField;
    }

    @Data
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class PollDecl {
        private String url;
        private String method;
    }

    @Data
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class PipelineEntry {
        /** agent-k 现行字段：裸 Capability ID。 */
        private String capability;
        /** @deprecated 入站兼容旧 {@code pipeline:*}；出站改写为 {@link #capability}。 */
        private String ref;
        private String name;
        private String displayName;
    }
}
