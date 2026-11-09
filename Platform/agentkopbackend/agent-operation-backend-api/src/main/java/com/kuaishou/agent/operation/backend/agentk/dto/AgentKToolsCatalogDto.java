package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.ArrayList;
import java.util.List;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * {@code GET /v1/tools/catalog} 响应：实例级工具目录（含 inputSchema）。
 * <p>
 * 仅 agentk-demo 提供上游路由；agentk-api 核心产物无此端点。
 */
@Data
public class AgentKToolsCatalogDto {

    /** 模型目录版本，与 {@code catalogVersion} 对齐。 */
    private String catalogVersion;
    /** 当前实例可构造的全部工具（builtin + 目录派生）。 */
    private List<AgentKToolCatalogItemDto> tools = new ArrayList<>();

    @Data
    public static class AgentKToolCatalogItemDto {
        private String name;
        private String description;
        private JsonNode inputSchema;
        /** inline / activity / workflow / user */
        private String bindingKind;
        /** 异步工具（Activity/Workflow 异步档）时为 true；缺省表示同步或未声明。 */
        private Boolean async;
        private JsonNode producesArtifact;
    }
}
