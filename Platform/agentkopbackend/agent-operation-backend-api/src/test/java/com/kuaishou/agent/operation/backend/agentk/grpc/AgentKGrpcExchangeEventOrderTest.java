package com.kuaishou.agent.operation.backend.agentk.grpc;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNull;

import java.util.Arrays;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse.AgentKEventDto;

class AgentKGrpcExchangeEventOrderTest {

    @Test
    void newestFirstEventsAreOrderedByAgentkCreatedAt() {
        AgentKListEventsResponse result = AgentKGrpcExchange.normalizeListedEvents(
                Arrays.asList(
                        event("evt_3", "2026-08-19T01:41:41Z"),
                        event("evt_1", "2026-08-19T01:38:10Z"),
                        event("evt_2", "2026-08-19T01:38:17Z")),
                0L, 10);

        assertEquals(Arrays.asList("evt_1", "evt_2", "evt_3"), Arrays.asList(
                result.getEvents().get(0).getEventId(),
                result.getEvents().get(1).getEventId(),
                result.getEvents().get(2).getEventId()));
        assertNull(result.getEvents().get(0).getSeq());
        assertFalse(result.getHasMore());
    }

    @Test
    void nativeSeqIsUsedForAfterCursorWhenPresent() {
        AgentKEventDto first = event("evt_1", "2026-08-19T01:38:10Z");
        first.setSeq(1L);
        AgentKEventDto second = event("evt_2", "2026-08-19T01:38:17Z");
        second.setSeq(2L);
        AgentKEventDto third = event("evt_3", "2026-08-19T01:41:41Z");
        third.setSeq(3L);

        AgentKListEventsResponse result = AgentKGrpcExchange.normalizeListedEvents(
                Arrays.asList(third, second, first), 1L, 10);

        assertEquals("evt_2", result.getEvents().get(0).getEventId());
        assertEquals(2L, result.getEvents().get(0).getSeq());
        assertEquals("evt_3", result.getEvents().get(1).getEventId());
    }

    private static AgentKEventDto event(String eventId, String createdAt) {
        AgentKEventDto event = new AgentKEventDto();
        event.setEventId(eventId);
        event.setCreatedAt(createdAt);
        return event;
    }
}
