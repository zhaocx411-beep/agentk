package com.kuaishou.agent.operation.backend.agentk;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.Test;

class AgentKSseProxyTest {

    @Test
    void resolveAfterSeqPrefersQueryThenLastEventId() {
        assertEquals(0L, AgentKSseProxy.resolveAfterSeq(null, null));
        assertEquals(12L, AgentKSseProxy.resolveAfterSeq("after=12", "3"));
        assertEquals(3L, AgentKSseProxy.resolveAfterSeq(null, "3"));
        assertEquals(0L, AgentKSseProxy.resolveAfterSeq("after=-1", null));
        assertEquals(0L, AgentKSseProxy.resolveAfterSeq("after=abc", "x"));
    }

    @Test
    void shouldDropCommittedSeqSkipsReplayAndDuplicates() {
        assertTrue(AgentKSseProxy.shouldDropCommittedSeq(5L, 5L));
        assertTrue(AgentKSseProxy.shouldDropCommittedSeq(4L, 5L));
        assertFalse(AgentKSseProxy.shouldDropCommittedSeq(6L, 5L));
        assertFalse(AgentKSseProxy.shouldDropCommittedSeq(0L, 5L));
    }
}
