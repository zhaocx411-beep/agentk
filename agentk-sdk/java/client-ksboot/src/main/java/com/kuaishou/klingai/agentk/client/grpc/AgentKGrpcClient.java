package com.kuaishou.klingai.agentk.client.grpc;

import com.kuaishou.klingai.agentk.client.AgentKClient;
import com.kuaishou.klingai.agentk.client.ClientOptions;

/**
 * Creates AgentK clients that use KESS service discovery and gRPC over h2c.
 */
public final class AgentKGrpcClient {
    private AgentKGrpcClient() {
    }

    /**
     * Creates an authenticated AgentK client that owns its transport resources.
     */
    public static AgentKClient create(ClientOptions options) {
        return create(options, KessIdentity.fromEnvironment());
    }

    static AgentKClient create(ClientOptions options, KessIdentity identity) {
        if (options == null) {
            throw new IllegalArgumentException("agentk client: options are required");
        }
        ChannelProvider channels = options.getEndpoint().isEmpty()
                ? new KessChannelProvider()
                : new FixedChannelProvider(options.getEndpoint());
        return new KsBootAgentKClient(channels, identity);
    }
}
