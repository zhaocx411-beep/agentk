package com.kuaishou.agent.operation.backend.platform.dto;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.annotation.JsonDeserialize;
import com.kuaishou.agent.operation.backend.platform.support.ProfileJsonDeserializer;

import lombok.Data;

/**
 * 保存 Workflow 版本草稿。
 * {@code definitionJson} 现为 JSON object（字段名历史遗留）。
 */
@Data
public class SaveWorkflowVersionRequest {
    /**
     * Workflow 定义 JSON object（必填）。推荐直接传 object，也兼容 JSON 字符串。
     * <pre>
     * {
     *   "name": "storyboard_film",
     *   "nodes": { "start": { "type": "start" } }
     * }
     * </pre>
     * 或 { "workflows": { "storyboard_film": { "nodes": {...} } } }
     */
    @JsonDeserialize(using = ProfileJsonDeserializer.class)
    private JsonNode definitionJson;
}
