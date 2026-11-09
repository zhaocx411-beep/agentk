package com.kuaishou.klingai.agentk.client.grpc

import com.connectrpc.protocols.NetworkProtocol
import com.kuaishou.klingai.agentk.api.v1.AgentKServiceClient
import com.kuaishou.klingai.agentk.client.internal.AgentKServiceProvider
import com.kuaishou.klingai.agentk.client.internal.ProtocolClients
import okhttp3.OkHttpClient
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.ThreadLocalRandom
import java.util.concurrent.atomic.AtomicBoolean

internal class KessAgentKServiceProvider(
    private val endpointSource: EndpointSource = KessEndpointSource(),
    private val okHttpClient: OkHttpClient = ProtocolClients.newGrpcClient(),
    private val random: () -> Double = { ThreadLocalRandom.current().nextDouble() },
) : AgentKServiceProvider {
    private val closed = AtomicBoolean(false)
    private val clients = ConcurrentHashMap<EndpointAddress, AgentKServiceClient>()

    override fun acquire(): AgentKServiceClient {
        check(!closed.get()) { "agentk client: client is closed" }
        val endpoint = WeightedEndpointPicker.pickBaseline(endpointSource.endpoints(), random)
        return clients.computeIfAbsent(EndpointAddress(endpoint.host, endpoint.port)) {
            ProtocolClients.create(endpoint.baseUrl(), NetworkProtocol.GRPC, okHttpClient)
        }
    }

    override fun close() {
        if (!closed.compareAndSet(false, true)) {
            return
        }
        clients.clear()
        ProtocolClients.close(okHttpClient)
    }
}
