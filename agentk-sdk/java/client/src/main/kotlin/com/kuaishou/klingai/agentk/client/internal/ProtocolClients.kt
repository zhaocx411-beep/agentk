package com.kuaishou.klingai.agentk.client.internal

import com.connectrpc.ProtocolClientConfig
import com.connectrpc.extensions.GoogleJavaProtobufStrategy
import com.connectrpc.impl.ProtocolClient
import com.connectrpc.okhttp.ConnectOkHttpClient
import com.connectrpc.protocols.NetworkProtocol
import com.connectrpc.simpleTimeouts
import com.kuaishou.klingai.agentk.api.v1.AgentKServiceClient
import kotlinx.coroutines.Dispatchers
import okhttp3.OkHttpClient
import okhttp3.Protocol
import kotlin.time.Duration.Companion.seconds

/** Internal Connect/OkHttp construction shared with the optional gRPC module. */
public object ProtocolClients {
    @JvmStatic
    public fun newHttpClient(): OkHttpClient = configuredBuilder().build()

    @JvmStatic
    public fun newGrpcClient(): OkHttpClient = configuredBuilder()
        .protocols(listOf(Protocol.H2_PRIOR_KNOWLEDGE))
        .build()

    @JvmStatic
    public fun create(
        baseUrl: String,
        networkProtocol: NetworkProtocol,
        okHttpClient: OkHttpClient,
    ): AgentKServiceClient {
        val protocolClient = ProtocolClient(
            ConnectOkHttpClient(okHttpClient),
            ProtocolClientConfig(
                host = baseUrl,
                serializationStrategy = GoogleJavaProtobufStrategy(),
                networkProtocol = networkProtocol,
                ioCoroutineContext = Dispatchers.IO,
                timeoutOracle = simpleTimeouts(10.seconds, null),
            ),
        )
        return AgentKServiceClient(protocolClient)
    }

    @JvmStatic
    public fun close(okHttpClient: OkHttpClient) {
        okHttpClient.dispatcher.cancelAll()
        okHttpClient.dispatcher.executorService.shutdown()
        okHttpClient.connectionPool.evictAll()
        okHttpClient.cache?.close()
    }

    private fun configuredBuilder(): OkHttpClient.Builder =
        ConnectOkHttpClient.configureClient(OkHttpClient.Builder())
            .retryOnConnectionFailure(false)
}
