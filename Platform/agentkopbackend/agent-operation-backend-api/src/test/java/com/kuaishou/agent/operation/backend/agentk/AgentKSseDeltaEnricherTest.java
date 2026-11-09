package com.kuaishou.agent.operation.backend.agentk;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

class AgentKSseDeltaEnricherTest {

    private final ObjectMapper mapper = new ObjectMapper();

    @Test
    void isDeltaEventOnlyForDeltas() {
        assertTrue(AgentKSseDeltaEnricher.isDeltaEvent("reasoning.delta"));
        assertTrue(AgentKSseDeltaEnricher.isDeltaEvent("text.delta"));
        assertFalse(AgentKSseDeltaEnricher.isDeltaEvent("run.started"));
        assertFalse(AgentKSseDeltaEnricher.isDeltaEvent(null));
    }

    @Test
    void enrichDataAddsIdAndTsForDelta() throws Exception {
        String out = AgentKSseDeltaEnricher.enrichData("{\"text\":\"1\"}", 7L, 1710000000123L, mapper);
        JsonNode node = mapper.readTree(out);
        assertEquals(7L, node.get("id").asLong());
        assertEquals(1710000000123L, node.get("ts").asLong());
        assertEquals("1", node.get("text").asText());
    }

    @Test
    void enrichDataAddsIdWithoutTsWhenTsNull() throws Exception {
        String out = AgentKSseDeltaEnricher.enrichData("{\"model\":\"x\"}", 1L, null, mapper);
        JsonNode node = mapper.readTree(out);
        assertEquals(1L, node.get("id").asLong());
        assertNull(node.get("ts"));
    }

    @Test
    void enrichDataKeepsInvalidJson() {
        assertEquals("{bad", AgentKSseDeltaEnricher.enrichData("{bad", 1L, 1L, mapper));
    }
}
