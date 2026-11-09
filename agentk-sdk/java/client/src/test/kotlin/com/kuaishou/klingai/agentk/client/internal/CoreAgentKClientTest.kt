package com.kuaishou.klingai.agentk.client.internal

import com.connectrpc.Code
import com.connectrpc.ConnectException
import com.connectrpc.Headers
import com.connectrpc.ResponseMessage
import com.connectrpc.ServerOnlyStreamInterface
import com.google.protobuf.Struct
import com.google.protobuf.Value
import com.kuaishou.klingai.agentk.api.v1.AgentMessageExecutionInput
import com.kuaishou.klingai.agentk.api.v1.CancelExecutionRequest
import com.kuaishou.klingai.agentk.api.v1.CancelExecutionResponse
import com.kuaishou.klingai.agentk.api.v1.Capability
import com.kuaishou.klingai.agentk.api.v1.CreateSessionRequest
import com.kuaishou.klingai.agentk.api.v1.CreateSessionResponse
import com.kuaishou.klingai.agentk.api.v1.Delta
import com.kuaishou.klingai.agentk.api.v1.Event
import com.kuaishou.klingai.agentk.api.v1.Execution
import com.kuaishou.klingai.agentk.api.v1.GetExecutionRequest
import com.kuaishou.klingai.agentk.api.v1.GetExecutionResponse
import com.kuaishou.klingai.agentk.api.v1.InvokeCapabilityRequest
import com.kuaishou.klingai.agentk.api.v1.InvokeCapabilityResponse
import com.kuaishou.klingai.agentk.api.v1.ListCapabilitiesRequest
import com.kuaishou.klingai.agentk.api.v1.ListCapabilitiesResponse
import com.kuaishou.klingai.agentk.api.v1.ListEventsRequest
import com.kuaishou.klingai.agentk.api.v1.ListEventsResponse
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsRequest
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsResponse
import com.kuaishou.klingai.agentk.api.v1.ListPipelinesRequest
import com.kuaishou.klingai.agentk.api.v1.ListPipelinesResponse
import com.kuaishou.klingai.agentk.api.v1.ListSessionsRequest
import com.kuaishou.klingai.agentk.api.v1.ListSessionsResponse
import com.kuaishou.klingai.agentk.api.v1.Pipeline
import com.kuaishou.klingai.agentk.api.v1.Profile
import com.kuaishou.klingai.agentk.api.v1.Session
import com.kuaishou.klingai.agentk.api.v1.StartExecutionRequest
import com.kuaishou.klingai.agentk.api.v1.StartExecutionResponse
import com.kuaishou.klingai.agentk.api.v1.StreamEnd
import com.kuaishou.klingai.agentk.api.v1.WatchEventsRequest
import com.kuaishou.klingai.agentk.api.v1.WatchEventsResponse
import com.kuaishou.klingai.agentk.api.v1.WhoAmIRequest
import com.kuaishou.klingai.agentk.api.v1.WhoAmIResponse
import com.kuaishou.klingai.agentk.client.EventObserver
import java.util.concurrent.CountDownLatch
import java.util.concurrent.ExecutionException
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.channels.Channel
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

internal const val TEST_KWS_BINARY = "ChlhZ2VudGsuc2RrLmZpeHR1cmUv5Y6f5qC3"
internal const val TEST_CREDENTIAL = "test-credential"
internal val testIdentityHeaders = mapOf(
    "kws-bin" to listOf(TEST_KWS_BINARY),
    "kwsCredential" to listOf(TEST_CREDENTIAL),
)

class CoreAgentKClientTest {
    @Test
    fun `unary methods preserve requests responses and identity`() {
        val protocol = FakeProtocolClient()
        val client = CoreAgentKClient(FixedTestProvider(protocol), testIdentityHeaders)
        try {
            checkUnary(
                protocol, "CreateSession",
                CreateSessionRequest.newBuilder().setTitle("new session")
                    .setProfile(Profile.newBuilder().setDefaultModel("test-model")).build(),
                CreateSessionResponse.newBuilder()
                    .setSession(Session.newBuilder().setId("created").setTitle("new session")).build(),
                client::createSession,
            )
            checkUnary(
                protocol, "ListSessions",
                ListSessionsRequest.newBuilder().setPageSize(7).setPageToken("sessions-before").build(),
                ListSessionsResponse.newBuilder().addSessions(Session.newBuilder().setId("listed"))
                    .setNextPageToken("sessions-after").build(),
                client::listSessions,
            )
            checkUnary(
                protocol, "StartExecution",
                StartExecutionRequest.newBuilder().setSessionId("start-session")
                    .setMessage(AgentMessageExecutionInput.newBuilder().setText("hello")).build(),
                StartExecutionResponse.newBuilder().setExecution(Execution.newBuilder().setId("started")).build(),
                client::startExecution,
            )
            checkUnary(
                protocol, "CancelExecution",
                CancelExecutionRequest.newBuilder().setSessionId("cancel-session").build(),
                CancelExecutionResponse.getDefaultInstance(),
                client::cancelExecution,
            )
            checkUnary(
                protocol, "ListEvents",
                ListEventsRequest.newBuilder().setSessionId("events-session").setPageSize(8)
                    .setPageToken("events-before").build(),
                ListEventsResponse.newBuilder().addEvents(Event.newBuilder().setEventId("event"))
                    .setNextPageToken("events-after").build(),
                client::listEvents,
            )
            checkUnary(
                protocol, "ListPipelines",
                ListPipelinesRequest.newBuilder().setSessionId("pipelines-session").setPageSize(9)
                    .setPageToken("pipelines-before").build(),
                ListPipelinesResponse.newBuilder().addPipelines(Pipeline.newBuilder().setName("render"))
                    .setNextPageToken("pipelines-after").build(),
                client::listPipelines,
            )
            checkUnary(
                protocol, "ListExecutions",
                ListExecutionsRequest.newBuilder().setSessionId("executions-session").setPageSize(10)
                    .setPageToken("executions-before").build(),
                ListExecutionsResponse.newBuilder().addExecutions(Execution.newBuilder().setId("listed-execution"))
                    .setNextPageToken("executions-after").build(),
                client::listExecutions,
            )
            checkUnary(
                protocol, "GetExecution",
                GetExecutionRequest.newBuilder().setSessionId("get-session").setExecutionId("get-execution").build(),
                GetExecutionResponse.newBuilder().setExecution(Execution.newBuilder().setId("get-execution")).build(),
                client::getExecution,
            )
            checkUnary(
                protocol, "ListCapabilities",
                ListCapabilitiesRequest.newBuilder().setPageSize(11).setPageToken("capabilities-before")
                    .setIncludeVariants(true).build(),
                ListCapabilitiesResponse.newBuilder()
                    .addCapabilities(Capability.newBuilder().setId("contrib.test.render"))
                    .setNextPageToken("capabilities-after").build(),
                client::listCapabilities,
            )
            checkUnary(
                protocol, "InvokeCapability",
                InvokeCapabilityRequest.newBuilder().setCapabilityId("contrib.test.render").setInput(
                    Struct.newBuilder().putFields("prompt", Value.newBuilder().setStringValue("draw").build())
                ).build(),
                InvokeCapabilityResponse.newBuilder().setOutput(Value.newBuilder().setStringValue("rendered")).build(),
                client::invokeCapability,
            )
            checkUnary(
                protocol, "WhoAmI",
                WhoAmIRequest.getDefaultInstance(),
                WhoAmIResponse.newBuilder().setId("caller").setAppId("app").setDisplayName("Caller").build(),
                client::whoAmI,
            )
        } finally {
            client.close()
        }
    }

    private fun <Q : Any, R : Any> checkUnary(
        protocol: FakeProtocolClient,
        method: String,
        request: Q,
        response: R,
        call: (Q) -> java.util.concurrent.CompletionStage<R>,
    ) {
        protocol.unaryResult = { ResponseMessage.Success(response, emptyMap(), emptyMap()) }
        assertEquals(response, call(request).await())
        assertEquals("agentk.api.v1.AgentKService/$method", protocol.paths.last())
        assertEquals(request, protocol.requests.last())
        val headers = protocol.headers.last()
        assertEquals(listOf(TEST_KWS_BINARY), headers["kws-bin"])
        assertEquals(listOf(TEST_CREDENTIAL), headers["kwsCredential"])
        assertEquals(null, headers["authorization"])
    }

    @Test
    fun `passes through connect errors`() {
        val failure = ConnectException(Code.UNAVAILABLE, "offline")
        val protocol = FakeProtocolClient().apply {
            unaryResult = { ResponseMessage.Failure<Any>(failure, emptyMap(), emptyMap()) }
        }
        val client = CoreAgentKClient(FixedTestProvider(protocol), testIdentityHeaders)

        val error = try {
            client.whoAmI(WhoAmIRequest.getDefaultInstance()).await()
            error("expected failure")
        } catch (exception: ExecutionException) {
            exception.cause
        }

        assertSame(failure, error)
        client.close()
    }

    @Test
    fun `future cancellation cancels the underlying coroutine`() {
        val started = CompletableDeferred<Unit>()
        val cancelled = CompletableDeferred<Unit>()
        val protocol = FakeProtocolClient().apply {
            unaryResult = {
                try {
                    started.complete(Unit)
                    awaitCancellation()
                } finally {
                    cancelled.complete(Unit)
                }
            }
        }
        val client = CoreAgentKClient(FixedTestProvider(protocol), testIdentityHeaders)
        val future = client.invokeCapability(
            InvokeCapabilityRequest.getDefaultInstance(),
        ).toCompletableFuture()

        kotlinx.coroutines.runBlocking {
            kotlinx.coroutines.withTimeout(2_000) { started.await() }
        }
        assertTrue(future.cancel(true))
        kotlinx.coroutines.runBlocking {
            kotlinx.coroutines.withTimeout(2_000) { cancelled.await() }
        }
        assertTrue(future.isCancelled)
        client.close()
    }

    @Test
    fun `close is idempotent and rejects later calls`() {
        val protocol = FakeProtocolClient()
        val provider = FixedTestProvider(protocol)
        val client = CoreAgentKClient(provider, testIdentityHeaders)

        client.close()
        client.close()
        val error = try {
            client.whoAmI(WhoAmIRequest.getDefaultInstance()).await()
            error("expected failure")
        } catch (exception: ExecutionException) {
            exception.cause
        }

        assertTrue(provider.closed)
        assertTrue(error is IllegalStateException)
    }

    @Test
    fun `watch events delivers frames and completes`() {
        val expected = listOf(
            WatchEventsResponse.newBuilder().setDelta(Delta.newBuilder().setExecutionId("execution").setText("hello"))
                .build(),
            WatchEventsResponse.newBuilder().setStreamEnd(StreamEnd.newBuilder().setExecutionId("execution")).build(),
        )
        val request = WatchEventsRequest.newBuilder().setSessionId("session").setExecutionId("execution").build()
        val stream = FakeServerStream(expected)
        val protocol = FakeProtocolClient().apply { serverStreamResult = stream }
        val client = CoreAgentKClient(FixedTestProvider(protocol), testIdentityHeaders)
        val frames = mutableListOf<WatchEventsResponse>()
        val complete = CountDownLatch(1)
        var error: Throwable? = null

        client.watchEvents(
            request,
            object : EventObserver {
                override fun onNext(event: WatchEventsResponse) {
                    frames += event
                }

                override fun onError(errorValue: Throwable) {
                    error = errorValue
                    complete.countDown()
                }

                override fun onComplete() {
                    complete.countDown()
                }
            },
        )

        assertTrue(complete.await(2, TimeUnit.SECONDS))
        assertEquals(expected, frames)
        assertEquals(null, error)
        assertEquals(request, stream.request)
        assertEquals(listOf(TEST_KWS_BINARY), protocol.headers.single()["kws-bin"])
        assertEquals(listOf(TEST_CREDENTIAL), protocol.headers.single()["kwsCredential"])
        assertEquals(null, protocol.headers.single()["authorization"])
        client.close()
    }

    @Test
    fun `watch cancellation has no terminal callback`() {
        val stream = FakeServerStream(emptyList(), keepOpen = true)
        val protocol = FakeProtocolClient().apply { serverStreamResult = stream }
        val client = CoreAgentKClient(FixedTestProvider(protocol), testIdentityHeaders)
        val terminal = CountDownLatch(1)
        val subscription = client.watchEvents(
            WatchEventsRequest.getDefaultInstance(),
            object : EventObserver {
                override fun onNext(event: WatchEventsResponse) = Unit
                override fun onError(error: Throwable) = terminal.countDown()
                override fun onComplete() = terminal.countDown()
            },
        )

        assertTrue(stream.sent.await(2, TimeUnit.SECONDS))
        subscription.cancel()

        assertTrue(subscription.isCancelled)
        assertFalse(terminal.await(200, TimeUnit.MILLISECONDS))
        client.close()
    }

    @Test
    fun `watch passes through server errors`() {
        val failure = ConnectException(Code.INTERNAL_ERROR, "stream failed")
        val stream = FakeServerStream(emptyList(), failure = failure)
        val protocol = FakeProtocolClient().apply { serverStreamResult = stream }
        val client = CoreAgentKClient(FixedTestProvider(protocol), testIdentityHeaders)
        val terminal = CountDownLatch(1)
        var observed: Throwable? = null

        client.watchEvents(
            WatchEventsRequest.getDefaultInstance(),
            object : EventObserver {
                override fun onNext(event: WatchEventsResponse) = Unit
                override fun onError(error: Throwable) {
                    observed = error
                    terminal.countDown()
                }

                override fun onComplete() = terminal.countDown()
            },
        )

        assertTrue(terminal.await(2, TimeUnit.SECONDS))
        assertSame(failure, observed)
        client.close()
    }

    @Test
    fun `observer failures are reported to onError`() {
        val failure = IllegalArgumentException("observer failed")
        val stream = FakeServerStream(listOf(WatchEventsResponse.getDefaultInstance()))
        val protocol = FakeProtocolClient().apply { serverStreamResult = stream }
        val client = CoreAgentKClient(FixedTestProvider(protocol), testIdentityHeaders)
        val terminal = CountDownLatch(1)
        var observed: Throwable? = null

        client.watchEvents(
            WatchEventsRequest.getDefaultInstance(),
            object : EventObserver {
                override fun onNext(event: WatchEventsResponse) {
                    throw failure
                }

                override fun onError(error: Throwable) {
                    observed = error
                    terminal.countDown()
                }

                override fun onComplete() = terminal.countDown()
            },
        )

        assertTrue(terminal.await(2, TimeUnit.SECONDS))
        assertSame(failure, observed)
        client.close()
    }

    @Test
    fun `client close cancels watch without terminal callback`() {
        val stream = FakeServerStream(emptyList(), keepOpen = true)
        val protocol = FakeProtocolClient().apply { serverStreamResult = stream }
        val provider = FixedTestProvider(protocol)
        val client = CoreAgentKClient(provider, testIdentityHeaders)
        val terminal = CountDownLatch(1)

        client.watchEvents(
            WatchEventsRequest.getDefaultInstance(),
            object : EventObserver {
                override fun onNext(event: WatchEventsResponse) = Unit
                override fun onError(error: Throwable) = terminal.countDown()
                override fun onComplete() = terminal.countDown()
            },
        )

        assertTrue(stream.sent.await(2, TimeUnit.SECONDS))
        client.close()

        assertTrue(provider.closed)
        assertFalse(terminal.await(200, TimeUnit.MILLISECONDS))
    }

    private fun <T> java.util.concurrent.CompletionStage<T>.await(): T =
        toCompletableFuture().get(2, TimeUnit.SECONDS)
}

internal class FakeServerStream(
    responses: List<WatchEventsResponse>,
    private val keepOpen: Boolean = false,
    failure: Throwable? = null,
) : ServerOnlyStreamInterface<WatchEventsRequest, WatchEventsResponse> {
    private val channel = Channel<WatchEventsResponse>(Channel.UNLIMITED)
    private val closed = AtomicBoolean(false)
    private val headers = CompletableDeferred<Headers>(emptyMap())
    private val trailers = CompletableDeferred<Headers>(emptyMap())
    val sent = CountDownLatch(1)
    var request: WatchEventsRequest? = null

    init {
        responses.forEach { channel.trySend(it) }
        if (!keepOpen) {
            channel.close(failure)
            closed.set(true)
        }
    }

    override fun responseChannel() = channel
    override fun responseHeaders() = headers
    override fun responseTrailers() = trailers

    override suspend fun sendAndClose(input: WatchEventsRequest): Result<Unit> {
        request = input
        sent.countDown()
        return Result.success(Unit)
    }

    override suspend fun receiveClose() {
        channel.cancel()
        closed.set(true)
    }

    override fun isReceiveClosed(): Boolean = closed.get()
    override fun isClosed(): Boolean = closed.get()
}
