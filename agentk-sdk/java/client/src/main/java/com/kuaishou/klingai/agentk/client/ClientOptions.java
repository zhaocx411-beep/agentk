package com.kuaishou.klingai.agentk.client;

import java.net.URI;
import java.net.URISyntaxException;

/**
 * Endpoint options shared by AgentK client transports.
 */
public final class ClientOptions {
    private final String endpoint;

    /**
     * Creates client options that use the transport's default AgentK endpoint.
     */
    public ClientOptions() {
        this.endpoint = "";
    }

    /**
     * Creates client options with a fixed AgentK endpoint.
     *
     * @param endpoint an absolute AgentK URL
     * @return client options using {@code endpoint}
     * @throws IllegalArgumentException if {@code endpoint} is null, empty, or not absolute
     */
    public static ClientOptions forEndpoint(String endpoint) {
        if (endpoint == null || endpoint.isEmpty()) {
            throw invalidEndpoint(endpoint);
        }
        try {
            URI uri = new URI(endpoint);
            if (!uri.isAbsolute()
                    || uri.getRawAuthority() == null
                    || uri.getRawAuthority().isEmpty()) {
                throw invalidEndpoint(endpoint);
            }
        } catch (URISyntaxException error) {
            throw invalidEndpoint(endpoint);
        }
        return new ClientOptions(endpoint);
    }

    /**
     * Returns the fixed AgentK endpoint, or an empty string when the transport default is used.
     *
     * @return the configured endpoint
     */
    public String getEndpoint() {
        return endpoint;
    }

    private ClientOptions(String endpoint) {
        this.endpoint = endpoint;
    }

    private static IllegalArgumentException invalidEndpoint(String endpoint) {
        return new IllegalArgumentException("agentk client: invalid endpoint \"" + endpoint + "\"");
    }
}
