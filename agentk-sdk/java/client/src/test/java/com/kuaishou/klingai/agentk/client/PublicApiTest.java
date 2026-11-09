package com.kuaishou.klingai.agentk.client;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertThrows;

import java.lang.reflect.Method;

import org.junit.Test;

public class PublicApiTest {
    @Test
    public void publicApiUsesOnlyJavaAndProtocolTypes() {
        for (Method method : AgentKClient.class.getMethods()) {
            assertFalse(method.toGenericString().contains("kotlinx.coroutines"));
            assertFalse(method.toGenericString().contains("com.connectrpc"));
            assertFalse(method.toGenericString().contains("com.kuaishou.kess"));
        }
    }

    @Test
    public void clientOptionsAcceptsAbsoluteEndpoint() {
        ClientOptions defaults = new ClientOptions();
        ClientOptions direct = ClientOptions.forEndpoint("http://127.0.0.1:8080");

        assertEquals("", defaults.getEndpoint());
        assertEquals("http://127.0.0.1:8080", direct.getEndpoint());
        assertThrows(IllegalArgumentException.class, () -> ClientOptions.forEndpoint(null));
        assertThrows(IllegalArgumentException.class, () -> ClientOptions.forEndpoint(""));
        assertThrows(IllegalArgumentException.class, () -> ClientOptions.forEndpoint("localhost:8080"));
    }
}
