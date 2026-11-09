package com.kuaishou.klingai.agentk.client.http

import com.connectrpc.protocols.NetworkProtocol
import com.kuaishou.klingai.agentk.client.AgentKClient
import com.kuaishou.klingai.agentk.client.ClientOptions
import com.kuaishou.klingai.agentk.client.internal.CoreAgentKClient
import com.kuaishou.klingai.agentk.client.internal.DirectAgentKServiceProvider
import com.kuaishou.klingai.agentk.client.internal.KessIdentity
import com.kuaishou.klingai.agentk.client.internal.ProtocolClients

/** Creates AgentK clients that use the environment's HTTP gateway. */
public class AgentKHttpClient private constructor() {
    public companion object {
        private const val STAGING_GATEWAY = "https://agentk-api.staging.kuaishou.com"
        private const val PRODUCTION_GATEWAY = "http://agentk-api.internal"

        @JvmStatic
        public fun create(options: ClientOptions): AgentKClient =
            create(options, KessIdentity.headers())

        internal fun create(
            options: ClientOptions,
            identityHeaders: Map<String, List<String>>,
        ): AgentKClient {
            val endpoint = options.endpoint.ifEmpty { gatewayUrl(System::getenv) }
            val okHttpClient = ProtocolClients.newHttpClient()
            return CoreAgentKClient(
                DirectAgentKServiceProvider(endpoint, NetworkProtocol.CONNECT, okHttpClient),
                identityHeaders,
            )
        }

        internal fun gatewayUrl(getenv: (String) -> String?): String =
            when (getenv("KWS_SERVICE_STAGE")?.trim()?.uppercase()) {
                "STAGING", "CANDIDATE" -> STAGING_GATEWAY
                else -> PRODUCTION_GATEWAY
            }
    }

}
