package com.kuaishou.klingai.agentk.client.grpc

import com.connectrpc.protocols.NetworkProtocol
import com.kuaishou.klingai.agentk.client.AgentKClient
import com.kuaishou.klingai.agentk.client.ClientOptions
import com.kuaishou.klingai.agentk.client.internal.CoreAgentKClient
import com.kuaishou.klingai.agentk.client.internal.DirectAgentKServiceProvider
import com.kuaishou.klingai.agentk.client.internal.AgentKServiceProvider
import com.kuaishou.klingai.agentk.client.internal.ProtocolClients

/** Creates AgentK clients that use KESS service discovery and gRPC over h2c. */
public class AgentKGrpcClient private constructor() {
    public companion object {
        /**
         * Creates an authenticated AgentK client.
         *
         * The returned client owns its transport resources and must be closed
         * when it is no longer needed.
         *
         * @param options endpoint options for the client
         * @return a client that connects directly when an endpoint is configured, or discovers
         * AgentK endpoints through KESS otherwise
         */
        @JvmStatic
        public fun create(options: ClientOptions): AgentKClient = create(options, null)

        internal fun create(
            options: ClientOptions,
            identityHeaders: Map<String, List<String>>?,
        ): AgentKClient {
            fun client(provider: AgentKServiceProvider): AgentKClient =
                if (identityHeaders == null) CoreAgentKClient(provider)
                else CoreAgentKClient(provider, identityHeaders)

            if (options.endpoint.isEmpty()) {
                return client(KessAgentKServiceProvider())
            }
            val okHttpClient = ProtocolClients.newGrpcClient()
            return client(
                DirectAgentKServiceProvider(
                    options.endpoint,
                    NetworkProtocol.GRPC,
                    okHttpClient,
                ),
            )
        }
    }
}
