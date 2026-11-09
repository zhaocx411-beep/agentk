package com.kuaishou.agent.operation.backend.agentk;

import static org.junit.jupiter.api.Assertions.assertEquals;

import java.util.Collections;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse.AgentKEventDto;

class ChatRunFollowUpServiceTest {

    private final ObjectMapper objectMapper = new ObjectMapper();

    @Test
    void roundCompletedIsNotExecutionTerminal() {
        assertEquals("running", ChatRunFollowUpService.inferTurnStatus(
                Collections.singletonList(event("round.completed", null))));
    }

    @Test
    void overflowSeqIsNotTruncatedToInt() {
        assertEquals(Integer.valueOf(Integer.MAX_VALUE), ChatRunFollowUpService.toIntSeqOrNull((long) Integer.MAX_VALUE));
        assertEquals(null, ChatRunFollowUpService.toIntSeqOrNull(Integer.MAX_VALUE + 1L));
        assertEquals(null, ChatRunFollowUpService.toIntSeqOrNull(null));
    }

    private AgentKEventDto event(String kind, String status) {
        AgentKEventDto event = new AgentKEventDto();
        event.setKind(kind);
        if (status != null) {
            event.setData(objectMapper.createObjectNode().put("status", status));
        }
        return event;
    }
}
