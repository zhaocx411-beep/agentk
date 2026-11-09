package com.kuaishou.klingai.agentk.client.internal

import com.connectrpc.protocols.NetworkProtocol
import com.kuaishou.klingai.agentk.api.v1.AgentKServiceClient
import okhttp3.OkHttpClient
import java.util.concurrent.atomic.AtomicBoolean

/** Owns one protocol client connected directly to a fixed AgentK endpoint. */
public class DirectAgentKServiceProvider(
    endpoint: String,
    networkProtocol: NetworkProtocol,
    private val okHttpClient: OkHttpClient,
) : AgentKServiceProvider {
    private val closed = AtomicBoolean(false)
    private val rpc = ProtocolClients.create(endpoint, networkProtocol, okHttpClient)

    override fun acquire(): AgentKServiceClient {
        check(!closed.get()) { "agentk client: client is closed" }
        return rpc
    }

    override fun close() {
        if (closed.compareAndSet(false, true)) {
            ProtocolClients.close(okHttpClient)
        }
    }
}
