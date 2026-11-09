package com.kuaishou.agent.operation.backend.agentk.grpc;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse.AgentKEventDto;
import com.kuaishou.klingai.agentk.api.v1.AgentExecutionResult;
import com.kuaishou.klingai.agentk.api.v1.Event;
import com.kuaishou.klingai.agentk.api.v1.ExecutionFinished;
import com.kuaishou.klingai.agentk.api.v1.ExecutionStatus;

class AgentKEventWireConverterTest {

    private final ObjectMapper objectMapper = new ObjectMapper();

    @Test
    void uuidEventIdMustNotBecomeASequence() {
        Event event = Event.newBuilder()
                .setEventId("evt_01a017ab-307f-7e58-9b2e-8e2be789c969")
                .setExecutionId("exe_1")
                .setExecutionFinished(ExecutionFinished.newBuilder()
                        .setStatus(ExecutionStatus.EXECUTION_STATUS_COMPLETED)
                        .setAgent(AgentExecutionResult.newBuilder()))
                .build();

        AgentKEventDto dto = AgentKEventWireConverter.toEventDto(event, objectMapper);

        assertNull(dto.getSeq());
        assertEquals("execution.finished", dto.getKind());
        assertEquals("evt_01a017ab-307f-7e58-9b2e-8e2be789c969", dto.getEventId());
    }
}
