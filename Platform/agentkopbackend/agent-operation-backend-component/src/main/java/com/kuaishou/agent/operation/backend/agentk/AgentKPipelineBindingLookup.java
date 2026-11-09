package com.kuaishou.agent.operation.backend.agentk;

import java.util.Collection;
import java.util.Map;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKPipelineBindingDto;

/**
 * 从能力缓存解析 pipeline ref/name。实现在 api 模块（依赖 agent-k 客户端）。
 */
public interface AgentKPipelineBindingLookup {

    Map<String, AgentKPipelineBindingDto> pipelineBindings(Collection<String> agentkIds);
}
