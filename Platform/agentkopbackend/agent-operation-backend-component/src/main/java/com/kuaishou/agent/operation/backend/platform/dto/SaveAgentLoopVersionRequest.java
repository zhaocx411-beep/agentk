package com.kuaishou.agent.operation.backend.platform.dto;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.annotation.JsonDeserialize;
import com.kuaishou.agent.operation.backend.platform.support.SaveAgentLoopVersionRequestDeserializer;

import lombok.Data;

/**
 * 保存 AgentLoop 的 profile 版本草稿。
 * 字段名 profileYaml 为历史遗留。
 * <p>
 * 两种请求体都合法：
 * <pre>
 * {"profileYaml": {"defaultModel":"...","system":"...","tools":[],"pipelines":[]}}
 * </pre>
 * 或直接把 Profile 对象当根：
 * <pre>
 * {"defaultModel":"...","system":"...","tools":[],"pipelines":[]}
 * </pre>
 */
@Data
@JsonDeserialize(using = SaveAgentLoopVersionRequestDeserializer.class)
public class SaveAgentLoopVersionRequest {

    private JsonNode profileYaml;
}
