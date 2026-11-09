package com.kuaishou.klingai.agentk.client.http

import com.kuaishou.klingai.agentk.api.v1.WatchEventsRequest
import com.kuaishou.klingai.agentk.api.v1.WatchEventsResponse
import com.kuaishou.klingai.agentk.api.v1.WhoAmIRequest
import com.kuaishou.klingai.agentk.api.v1.WhoAmIResponse
import com.kuaishou.klingai.agentk.client.ClientOptions
import com.kuaishou.klingai.agentk.client.EventObserver
import com.kuaishou.klingai.agentk.client.internal.TestTelemetry
import com.kuaishou.klingai.agentk.client.internal.TEST_CREDENTIAL
import com.kuaishou.klingai.agentk.client.internal.TEST_KWS_BINARY
import com.kuaishou.klingai.agentk.client.internal.testIdentityHeaders
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import okio.Buffer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

class AgentKHttpClientTest {
    @Test
    fun `selects gateway from service stage`() {
        assertEquals(
            "https://agentk-api.staging.kuaishou.com",
            AgentKHttpClient.gatewayUrl { "STAGING" },
        )
        assertEquals(
            "https://agentk-api.staging.kuaishou.com",
            AgentKHttpClient.gatewayUrl { " candidate " },
        )
        assertEquals("http://agentk-api.internal", AgentKHttpClient.gatewayUrl { "PRODUCTION" })
        assertEquals("http://agentk-api.internal", AgentKHttpClient.gatewayUrl { null })
    }

    @Test
    fun `connect unary uses real wire and KESS identity`() {
        val server = MockWebServer()
        server.start()
        try {
            server.enqueue(
                MockResponse.Builder()
                    .code(200)
                    .addHeader("content-type", "application/proto")
                    .body(Buffer().write(WhoAmIResponse.getDefaultInstance().toByteArray()))
                    .build(),
            )
            val client = AgentKHttpClient.create(
                ClientOptions.forEndpoint(server.url("/").toString()),
                testIdentityHeaders,
            )

            client.whoAmI(WhoAmIRequest.getDefaultInstance())
                .toCompletableFuture()
                .get(2, TimeUnit.SECONDS)

            val request = server.takeRequest(2, TimeUnit.SECONDS)
            assertEquals("/agentk.api.v1.AgentKService/WhoAmI", request?.target)
            assertEquals(TEST_KWS_BINARY, request?.headers?.get("kws-bin"))
            assertEquals(TEST_CREDENTIAL, request?.headers?.get("kwsCredential"))
            assertNull(request?.headers?.get("authorization"))
            assertNull(request?.headers?.get("trace_context-bin"))
            assertEquals("application/proto", request?.headers?.get("content-type"))
            client.close()
        } finally {
            server.close()
        }
    }

    @Test
    fun `connect unary propagates active trace context on the wire`() {
        val telemetry = TestTelemetry()
        val server = MockWebServer()
        server.start()
        val client = AgentKHttpClient.create(
            ClientOptions.forEndpoint(server.url("/").toString()),
            testIdentityHeaders,
        )
        val parent = telemetry.tracer.spanBuilder("caller").startSpan()
        try {
            server.enqueue(
                MockResponse.Builder()
                    .code(200)
                    .addHeader("content-type", "application/proto")
                    .body(Buffer().write(WhoAmIResponse.getDefaultInstance().toByteArray()))
                    .build(),
            )

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
    fun `connect server stream uses real wire`() {
        val server = MockWebServer()
        server.start()
        try {
            val message = WatchEventsResponse.getDefaultInstance().toByteArray()
            val body = Buffer()
                .writeByte(0)
                .writeInt(message.size)
                .write(message)
                .writeByte(0)
                .writeInt(message.size)
                .write(message)
                .writeByte(2)
                .writeInt(2)
                .writeUtf8("{}")
            server.enqueue(
                MockResponse.Builder()
                    .code(200)
                    .addHeader("content-type", "application/connect+proto")
                    .body(body)
                    .build(),
            )
            val client = AgentKHttpClient.create(
                ClientOptions.forEndpoint(server.url("/").toString()),
                testIdentityHeaders,
            )
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
            assertEquals("application/connect+proto", request?.headers?.get("content-type"))
            assertEquals(TEST_KWS_BINARY, request?.headers?.get("kws-bin"))
            assertEquals(TEST_CREDENTIAL, request?.headers?.get("kwsCredential"))
            assertNull(request?.headers?.get("authorization"))
            client.close()
        } finally {
            server.close()
        }
    }
}
