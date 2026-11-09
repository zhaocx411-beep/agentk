package com.kuaishou.klingai.agentk.client.internal

import com.connectrpc.Code
import com.connectrpc.ConnectException
import com.connectrpc.ResponseMessage
import com.kuaishou.klingai.agentk.api.v1.WatchEventsRequest
import com.kuaishou.klingai.agentk.api.v1.WatchEventsResponse
import com.kuaishou.klingai.agentk.api.v1.WhoAmIRequest
import com.kuaishou.klingai.agentk.api.v1.WhoAmIResponse
import com.kuaishou.klingai.agentk.client.EventObserver
import io.opentelemetry.api.GlobalOpenTelemetry
import io.opentelemetry.api.trace.Span
import io.opentelemetry.api.trace.SpanContext
import io.opentelemetry.api.trace.SpanKind
import io.opentelemetry.api.trace.StatusCode
import io.opentelemetry.api.trace.TraceFlags
import io.opentelemetry.api.trace.TraceState
import io.opentelemetry.context.Context
import io.opentelemetry.sdk.OpenTelemetrySdk
import io.opentelemetry.sdk.trace.SdkTracerProvider
import io.opentelemetry.sdk.trace.data.SpanData
import io.opentelemetry.sdk.trace.export.SimpleSpanProcessor
import io.opentelemetry.sdk.testing.exporter.InMemorySpanExporter
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.awaitCancellation
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.ExecutionException
import java.util.concurrent.TimeUnit

class CoreAgentKClientTracingTest {
    @Test
    fun `unary preserves caller context across coroutine and injects child trace context`() {
        val telemetry = TestTelemetry()
        val entered = CountDownLatch(1)
        val release = CompletableDeferred<Unit>()
        var activeSpan = Span.getInvalid().spanContext
        val protocol = FakeProtocolClient().apply {
            unaryResult = {
                activeSpan = Span.current().spanContext
                entered.countDown()
                release.await()
                ResponseMessage.Success(WhoAmIResponse.getDefaultInstance(), emptyMap(), emptyMap())
            }
        }
        val client = CoreAgentKClient(FixedTestProvider(protocol), testIdentityHeaders)
        val upstream = Span.wrap(
            SpanContext.createFromRemoteParent(
                "11111111111111111111111111111111",
                "2222222222222222",
                TraceFlags.getSampled(),
                TraceState.builder().put("vendor", "state").build(),
            ),
        )
        val parent = telemetry.tracer.spanBuilder("caller")
            .setParent(Context.root().with(upstream))
            .startSpan()

        try {
            val future = parent.makeCurrent().use {
                client.whoAmI(WhoAmIRequest.getDefaultInstance())
            }
            assertTrue(entered.await(2, TimeUnit.SECONDS))
            release.complete(Unit)
            future.toCompletableFuture().get(2, TimeUnit.SECONDS)
            parent.end()

            val rpcSpan = telemetry.awaitSpan(RPC_WHO_AM_I)
            assertEquals(SpanKind.CLIENT, rpcSpan.kind)
            assertEquals(parent.spanContext.traceId, rpcSpan.spanContext.traceId)
            assertEquals(parent.spanContext.spanId, rpcSpan.parentSpanId)
            assertEquals(rpcSpan.spanContext, activeSpan)
            assertTrue(rpcSpan.events.isEmpty())

            val traceparent = protocol.headers.single()["traceparent"]?.single()
            assertTraceparent(traceparent, rpcSpan)
            assertEquals(listOf("vendor=state"), protocol.headers.single()["tracestate"])
            assertEquals(listOf(TEST_KWS_BINARY), protocol.headers.single()["kws-bin"])
            assertEquals(listOf(TEST_CREDENTIAL), protocol.headers.single()["kwsCredential"])
            assertEquals(null, protocol.headers.single()["authorization"])
        } finally {
            if (parent.isRecording) {
                parent.end()
            }
            client.close()
            telemetry.close()
        }
    }

    @Test
    fun `unary records failures as errors`() {
        val telemetry = TestTelemetry()
        val failure = ConnectException(Code.UNAVAILABLE, "offline")
        val protocol = FakeProtocolClient().apply {
            unaryResult = { ResponseMessage.Failure<Any>(failure, emptyMap(), emptyMap()) }
        }
        val client = CoreAgentKClient(FixedTestProvider(protocol), testIdentityHeaders)

        try {
            val observed = try {
                client.whoAmI(WhoAmIRequest.getDefaultInstance()).toCompletableFuture()
                    .get(2, TimeUnit.SECONDS)
                error("expected failure")
            } catch (error: ExecutionException) {
                error.cause
            }

            assertSame(failure, observed)
            val rpcSpan = telemetry.awaitSpan(RPC_WHO_AM_I)
            assertEquals(StatusCode.ERROR, rpcSpan.status.statusCode)
            assertTrue(rpcSpan.events.any { it.name == "exception" })
        } finally {
            client.close()
            telemetry.close()
        }
    }

    @Test
    fun `unary cancellation ends span without error`() {
        val telemetry = TestTelemetry()
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

        try {
            val future = client.whoAmI(WhoAmIRequest.getDefaultInstance()).toCompletableFuture()
            kotlinx.coroutines.runBlocking {
                kotlinx.coroutines.withTimeout(2_000) { started.await() }
            }
            assertTrue(future.cancel(true))
            kotlinx.coroutines.runBlocking {
                kotlinx.coroutines.withTimeout(2_000) { cancelled.await() }
            }

            val rpcSpan = telemetry.awaitSpan(RPC_WHO_AM_I)
            assertEquals(StatusCode.UNSET, rpcSpan.status.statusCode)
            assertTrue(rpcSpan.events.isEmpty())
        } finally {
            client.close()
            telemetry.close()
        }
    }

    @Test
    fun `watch events keeps one span current for the stream lifetime`() {
        val telemetry = TestTelemetry()
        val stream = FakeServerStream(
            listOf(WatchEventsResponse.getDefaultInstance(), WatchEventsResponse.getDefaultInstance()),
        )
        val protocol = FakeProtocolClient().apply { serverStreamResult = stream }
        val client = CoreAgentKClient(FixedTestProvider(protocol), testIdentityHeaders)
        val complete = CountDownLatch(1)
        val callbackSpans = mutableListOf<io.opentelemetry.api.trace.SpanContext>()

        try {
            client.watchEvents(
                WatchEventsRequest.getDefaultInstance(),
                object : EventObserver {
                    override fun onNext(event: WatchEventsResponse) {
                        callbackSpans += Span.current().spanContext
                    }

                    override fun onError(error: Throwable) {
                        complete.countDown()
                    }

                    override fun onComplete() {
                        complete.countDown()
                    }
                },
            )

            assertTrue(complete.await(2, TimeUnit.SECONDS))
            val rpcSpan = telemetry.awaitSpan(RPC_WATCH_EVENTS)
            assertEquals(2, callbackSpans.size)
            assertTrue(callbackSpans.all { it == rpcSpan.spanContext })
            assertEquals(SpanKind.CLIENT, rpcSpan.kind)
            assertTrue(rpcSpan.events.isEmpty())
            assertTraceparent(protocol.headers.single()["traceparent"]?.single(), rpcSpan)
        } finally {
            client.close()
            telemetry.close()
        }
    }

    @Test
    fun `watch errors mark span and cancellation does not`() {
        val telemetry = TestTelemetry()
        val failure = ConnectException(Code.INTERNAL_ERROR, "stream failed")
        val failedStream = FakeServerStream(emptyList(), failure = failure)
        val protocol = FakeProtocolClient().apply { serverStreamResult = failedStream }
        val client = CoreAgentKClient(FixedTestProvider(protocol), testIdentityHeaders)
        val terminal = CountDownLatch(1)

        try {
            client.watchEvents(
                WatchEventsRequest.getDefaultInstance(),
                object : EventObserver {
                    override fun onNext(event: WatchEventsResponse) = Unit
                    override fun onError(error: Throwable) = terminal.countDown()
                    override fun onComplete() = terminal.countDown()
                },
            )
            assertTrue(terminal.await(2, TimeUnit.SECONDS))

            val failedSpan = telemetry.awaitSpan(RPC_WATCH_EVENTS)
            assertEquals(StatusCode.ERROR, failedSpan.status.statusCode)
            assertTrue(failedSpan.events.any { it.name == "exception" })

            val openStream = FakeServerStream(emptyList(), keepOpen = true)
            protocol.serverStreamResult = openStream
            val cancelledTerminal = CountDownLatch(1)
            val subscription = client.watchEvents(
                WatchEventsRequest.getDefaultInstance(),
                object : EventObserver {
                    override fun onNext(event: WatchEventsResponse) = Unit
                    override fun onError(error: Throwable) = cancelledTerminal.countDown()
                    override fun onComplete() = cancelledTerminal.countDown()
                },
            )
            assertTrue(openStream.sent.await(2, TimeUnit.SECONDS))
            subscription.cancel()

            val spans = telemetry.awaitSpans(RPC_WATCH_EVENTS, 2)
            val cancelledSpan = spans.single { it.spanId != failedSpan.spanId }
            assertEquals(StatusCode.UNSET, cancelledSpan.status.statusCode)
            assertTrue(cancelledSpan.events.isEmpty())
            assertFalse(cancelledTerminal.await(200, TimeUnit.MILLISECONDS))
        } finally {
            client.close()
            telemetry.close()
        }
    }

    @Test
    fun `client remains no-op before provider registration and traces later calls`() {
        GlobalOpenTelemetry.resetForTest()
        val protocol = FakeProtocolClient()
        val client = CoreAgentKClient(FixedTestProvider(protocol), testIdentityHeaders)
        var telemetry: TestTelemetry? = null

        try {
            client.whoAmI(WhoAmIRequest.getDefaultInstance()).toCompletableFuture()
                .get(2, TimeUnit.SECONDS)
            assertEquals(null, protocol.headers.single()["traceparent"])

            telemetry = TestTelemetry(resetGlobal = false)
            client.whoAmI(WhoAmIRequest.getDefaultInstance()).toCompletableFuture()
                .get(2, TimeUnit.SECONDS)

            val rpcSpan = telemetry.awaitSpan(RPC_WHO_AM_I)
            assertTraceparent(protocol.headers.last()["traceparent"]?.single(), rpcSpan)
        } finally {
            client.close()
            telemetry?.close() ?: GlobalOpenTelemetry.resetForTest()
        }
    }

    private fun assertTraceparent(traceparent: String?, span: SpanData) {
        val parts = checkNotNull(traceparent).split("-")
        assertEquals(4, parts.size)
        assertEquals(span.traceId, parts[1])
        assertEquals(span.spanId, parts[2])
    }

    private companion object {
        const val RPC_WHO_AM_I = "agentk.api.v1.AgentKService/WhoAmI"
        const val RPC_WATCH_EVENTS = "agentk.api.v1.AgentKService/WatchEvents"
    }
}

internal class TestTelemetry(resetGlobal: Boolean = true) : AutoCloseable {
    val exporter: InMemorySpanExporter = InMemorySpanExporter.create()
    private val tracerProvider: SdkTracerProvider = SdkTracerProvider.builder()
        .addSpanProcessor(SimpleSpanProcessor.create(exporter))
        .build()

    init {
        if (resetGlobal) {
            GlobalOpenTelemetry.resetForTest()
        }
        OpenTelemetrySdk.builder()
            .setTracerProvider(tracerProvider)
            .buildAndRegisterGlobal()
    }

    val tracer = GlobalOpenTelemetry.get().getTracer("agentk-client-test")

    fun awaitSpan(name: String): SpanData = awaitSpans(name, 1).single()

    fun awaitSpans(name: String, count: Int): List<SpanData> {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(2)
        do {
            val spans = exporter.finishedSpanItems.filter { it.name == name }
            if (spans.size >= count) {
                return spans
            }
            Thread.sleep(10)
        } while (System.nanoTime() < deadline)
        error("timed out waiting for $count span(s) named $name")
    }

    override fun close() {
        tracerProvider.close()
        exporter.close()
        GlobalOpenTelemetry.resetForTest()
    }
}
