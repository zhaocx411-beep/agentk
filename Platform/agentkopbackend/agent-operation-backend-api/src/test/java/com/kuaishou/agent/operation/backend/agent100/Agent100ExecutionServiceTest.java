package com.kuaishou.agent.operation.backend.agent100;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse.AgentKEventDto;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100ExecutionView.Agent100EventView;

class Agent100ExecutionServiceTest {

    private final ObjectMapper objectMapper = new ObjectMapper();

    @Test
    void mapsInvocationProgressWireKind() {
        AgentKEventDto event = new AgentKEventDto();
        event.setEventId("evt_1");
        event.setKind("invocation.progress");
        ObjectNode data = objectMapper.createObjectNode();
        data.put("stepId", "analyze_story");
        data.put("message", "正在分析剧情");
        data.set("data", objectMapper.createObjectNode().put("percent", 54));
        event.setData(data);

        Agent100EventView view = Agent100ExecutionService.toEventView(event);

        assertThat(view.getId()).isEqualTo("evt_1");
        assertThat(view.getType()).isEqualTo("progress");
        assertThat(view.getStep()).isEqualTo("analyze_story");
        assertThat(view.getMessage()).isEqualTo("正在分析剧情");
        assertThat(view.getData().path("percent").asInt()).isEqualTo(54);
    }

    @Test
    void mapsExecutionFinishedWireKind() {
        AgentKEventDto event = new AgentKEventDto();
        event.setEventId("evt_2");
        event.setKind("execution.finished");
        ObjectNode data = objectMapper.createObjectNode();
        data.put("status", "completed");
        data.put("error", "");
        event.setData(data);

        Agent100EventView view = Agent100ExecutionService.toEventView(event);

        assertThat(view.getType()).isEqualTo("finished");
        assertThat(view.getStatus()).isEqualTo("completed");
        assertThat(view.getError()).isEqualTo("");
    }
}
