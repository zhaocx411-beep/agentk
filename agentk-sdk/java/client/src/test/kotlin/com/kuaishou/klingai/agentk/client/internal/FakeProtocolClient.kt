package com.kuaishou.klingai.agentk.client.internal

import com.connectrpc.BidirectionalStreamInterface
import com.connectrpc.ClientOnlyStreamInterface
import com.connectrpc.Headers
import com.connectrpc.MethodSpec
import com.connectrpc.ProtocolClientInterface
import com.connectrpc.ResponseMessage
import com.connectrpc.ServerOnlyStreamInterface
import com.connectrpc.UnaryBlockingCall
import com.connectrpc.http.Cancelable
import com.kuaishou.klingai.agentk.api.v1.AgentKServiceClient

internal class FakeProtocolClient : ProtocolClientInterface {
    val paths = mutableListOf<String>()
    val requests = mutableListOf<Any>()
    val headers = mutableListOf<Headers>()
    var unaryResult: suspend (MethodSpec<*, *>) -> ResponseMessage<*> = { method ->
        ResponseMessage.Success(defaultMessage(method.responseClass.java), emptyMap(), emptyMap())
    }
    var serverStreamResult: ServerOnlyStreamInterface<*, *>? = null

    fun serviceClient(): AgentKServiceClient = AgentKServiceClient(this)

    override fun <Input : Any, Output : Any> unary(
        request: Input,
        headers: Headers,
        methodSpec: MethodSpec<Input, Output>,
        onResult: (ResponseMessage<Output>) -> Unit,
    ): Cancelable = error("callback unary is not used by the SDK facade")

    @Suppress("UNCHECKED_CAST")
    override suspend fun <Input : Any, Output : Any> unary(
        request: Input,
        headers: Headers,
        methodSpec: MethodSpec<Input, Output>,
    ): ResponseMessage<Output> {
        paths += methodSpec.path
        requests += request
        this.headers += headers
        return unaryResult(methodSpec) as ResponseMessage<Output>
    }

    override fun <Input : Any, Output : Any> unaryBlocking(
        request: Input,
        headers: Headers,
        methodSpec: MethodSpec<Input, Output>,
    ): UnaryBlockingCall<Output> = error("blocking unary is not used by the SDK facade")

    override suspend fun <Input : Any, Output : Any> stream(
        headers: Headers,
        methodSpec: MethodSpec<Input, Output>,
    ): BidirectionalStreamInterface<Input, Output> = error("bidirectional streams are not used")

    @Suppress("UNCHECKED_CAST")
    override suspend fun <Input : Any, Output : Any> serverStream(
        headers: Headers,
        methodSpec: MethodSpec<Input, Output>,
    ): ServerOnlyStreamInterface<Input, Output> {
        paths += methodSpec.path
        this.headers += headers
        return checkNotNull(serverStreamResult) { "fake server stream was not configured" }
            as ServerOnlyStreamInterface<Input, Output>
    }

    override suspend fun <Input : Any, Output : Any> clientStream(
        headers: Headers,
        methodSpec: MethodSpec<Input, Output>,
    ): ClientOnlyStreamInterface<Input, Output> = error("client streams are not used")

    private companion object {
        @Suppress("UNCHECKED_CAST")
        fun <T : Any> defaultMessage(type: Class<T>): T =
            type.getMethod("getDefaultInstance").invoke(null) as T
    }
}

internal class FixedTestProvider(
    protocolClient: FakeProtocolClient,
) : AgentKServiceProvider {
    private val client = protocolClient.serviceClient()
    var closed = false

    override fun acquire(): AgentKServiceClient {
        check(!closed)
        return client
    }

    override fun close() {
        closed = true
    }
}
