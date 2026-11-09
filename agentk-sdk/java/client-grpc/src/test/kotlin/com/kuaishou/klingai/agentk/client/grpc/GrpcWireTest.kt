package com.kuaishou.klingai.agentk.client.grpc

import com.kuaishou.klingai.agentk.api.v1.WatchEventsRequest
import com.kuaishou.klingai.agentk.api.v1.WatchEventsResponse
import com.kuaishou.klingai.agentk.api.v1.WhoAmIRequest
import com.kuaishou.klingai.agentk.api.v1.WhoAmIResponse
import com.kuaishou.klingai.agentk.client.AgentKClient
import com.kuaishou.klingai.agentk.client.ClientOptions
import com.kuaishou.klingai.agentk.client.EventObserver
import io.opentelemetry.api.GlobalOpenTelemetry
import io.opentelemetry.sdk.OpenTelemetrySdk
import io.opentelemetry.sdk.trace.SdkTracerProvider
import io.opentelemetry.sdk.trace.data.SpanData
import io.opentelemetry.sdk.trace.export.SimpleSpanProcessor
import io.opentelemetry.sdk.testing.exporter.InMemorySpanExporter
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import okhttp3.Headers
import okhttp3.Protocol
import okio.Buffer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

private const val TEST_KWS_BINARY = "ChlhZ2VudGsuc2RrLmZpeHR1cmUv5Y6f5qC3"
private const val TEST_CREDENTIAL = "test-credential"
private val testIdentityHeaders = mapOf(
    "kws-bin" to listOf(TEST_KWS_BINARY),
    "kwsCredential" to listOf(TEST_CREDENTIAL),
)

class GrpcWireTest {
    @Test
    fun `grpc h2c unary uses real wire`() {
        val server = grpcServer()
        try {
            val message = WhoAmIResponse.getDefaultInstance().toByteArray()
            server.enqueue(grpcResponse(grpcFrames(message)))
            val client = grpcClient(server)

            client.whoAmI(WhoAmIRequest.getDefaultInstance())
                .toCompletableFuture()
                .get(2, TimeUnit.SECONDS)

            val request = server.takeRequest(2, TimeUnit.SECONDS)
            assertEquals("/agentk.api.v1.AgentKService/WhoAmI", request?.target)
            assertEquals(TEST_KWS_BINARY, request?.headers?.get("kws-bin"))
            assertEquals(TEST_CREDENTIAL, request?.headers?.get("kwsCredential"))
            assertNull(request?.headers?.get("authorization"))
            assertNull(request?.headers?.get("trace_context-bin"))
            assertTrue(request?.headers?.get("content-type")?.startsWith("application/grpc") == true)
            client.close()
        } finally {
            server.close()
        }
    }

    @Test
    fun `grpc h2c unary propagates active trace context on the wire`() {
        val telemetry = GrpcTestTelemetry()
        val server = grpcServer()
        val client = grpcClient(server)
        val parent = telemetry.tracer.spanBuilder("caller").startSpan()
        try {
            val message = WhoAmIResponse.getDefaultInstance().toByteArray()
            server.enqueue(grpcResponse(grpcFrames(message)))

            parent.makeCurrent().use {
                client.whoAmI(WhoAmIRequest.getDefaultInstance())
                    .toCompletableFuture()
                    .get(2, TimeUnit.SECONDS)
            }
            parent.end()

            val rpcSpan = telemetry.awaitSpan("agentk.api.v1.AgentKService/WhoAmI")
            val traceparent = checkNotNull(
                server.takeRequest(2, TimeUnit.SECONDS)?.headers?.get("traceparent"),
            ).split("-")
            assertEquals(4, traceparent.size)
            assertEquals(parent.spanContext.traceId, rpcSpan.spanContext.traceId)
            assertEquals(parent.spanContext.spanId, rpcSpan.parentSpanId)
            assertEquals(rpcSpan.spanContext.traceId, traceparent[1])
            assertEquals(rpcSpan.spanContext.spanId, traceparent[2])
        } finally {
            if (parent.isRecording) {
                parent.end()
            }
            client.close()
            server.close()
            telemetry.close()
        }
    }

    @Test
    fun `grpc h2c server stream uses real wire`() {
        val server = grpcServer()
        try {
            val message = WatchEventsResponse.getDefaultInstance().toByteArray()
            server.enqueue(grpcResponse(grpcFrames(message, message)))
            val client = grpcClient(server)
            val done = CountDownLatch(1)
            var frames = 0
            var error: Throwable? = null

            client.watchEvents(
                WatchEventsRequest.getDefaultInstance(),
                object : EventObserver {
                    override fun onNext(event: WatchEventsResponse) {
                        frames++
                    }

                    override fun onError(errorValue: Throwable) {
                        error = errorValue
                        done.countDown()
                    }

                    override fun onComplete() {
                        done.countDown()
                    }
                },
            )

            assertTrue(done.await(2, TimeUnit.SECONDS))
            assertEquals(2, frames)
            assertNull(error)
            val request = server.takeRequest(2, TimeUnit.SECONDS)
            assertEquals("/agentk.api.v1.AgentKService/WatchEvents", request?.target)
            assertEquals(TEST_KWS_BINARY, request?.headers?.get("kws-bin"))
            assertEquals(TEST_CREDENTIAL, request?.headers?.get("kwsCredential"))
            assertNull(request?.headers?.get("authorization"))
            client.close()
        } finally {
            server.close()
        }
    }

    private fun grpcServer(): MockWebServer = MockWebServer().apply {
        protocols = listOf(Protocol.H2_PRIOR_KNOWLEDGE)
        start()
    }

    private fun grpcClient(server: MockWebServer): AgentKClient =
        AgentKGrpcClient.create(
            ClientOptions.forEndpoint(server.url("/").toString()),
            testIdentityHeaders,
        )

    private fun grpcFrames(vararg messages: ByteArray): Buffer = Buffer().apply {
        messages.forEach { message ->
            writeByte(0)
            writeInt(message.size)
            write(message)
        }
    }

    private fun grpcResponse(body: Buffer): MockResponse =
        MockResponse.Builder()
            .code(200)
            .addHeader("content-type", "application/grpc+proto")
            .body(body)
            .trailers(Headers.Builder().add("grpc-status", "0").build())
            .build()

}

private class GrpcTestTelemetry : AutoCloseable {
    private val exporter = InMemorySpanExporter.create()
    private val tracerProvider = SdkTracerProvider.builder()
        .addSpanProcessor(SimpleSpanProcessor.create(exporter))
        .build()

    init {
        GlobalOpenTelemetry.resetForTest()
        OpenTelemetrySdk.builder()
            .setTracerProvider(tracerProvider)
            .buildAndRegisterGlobal()
    }

    val tracer = GlobalOpenTelemetry.get().getTracer("agentk-grpc-client-test")

    fun awaitSpan(name: String): SpanData {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(2)
        do {
            exporter.finishedSpanItems.singleOrNull { it.name == name }?.let { return it }
            Thread.sleep(10)
        } while (System.nanoTime() < deadline)
        error("timed out waiting for span named $name")
    }

    override fun close() {
        tracerProvider.close()
        exporter.close()
        GlobalOpenTelemetry.resetForTest()
    }
}
