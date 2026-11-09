package com.kuaishou.klingai.agentk.client.internal

import com.connectrpc.Headers
import com.connectrpc.ResponseMessage
import com.connectrpc.getOrThrow
import com.kuaishou.klingai.agentk.api.v1.AgentKServiceClient
import com.kuaishou.klingai.agentk.api.v1.CancelExecutionRequest
import com.kuaishou.klingai.agentk.api.v1.CancelExecutionResponse
import com.kuaishou.klingai.agentk.api.v1.CreateSessionRequest
import com.kuaishou.klingai.agentk.api.v1.CreateSessionResponse
import com.kuaishou.klingai.agentk.api.v1.GetExecutionRequest
import com.kuaishou.klingai.agentk.api.v1.GetExecutionResponse
import com.kuaishou.klingai.agentk.api.v1.InvokeCapabilityRequest
import com.kuaishou.klingai.agentk.api.v1.InvokeCapabilityResponse
import com.kuaishou.klingai.agentk.api.v1.ListCapabilitiesRequest
import com.kuaishou.klingai.agentk.api.v1.ListCapabilitiesResponse
import com.kuaishou.klingai.agentk.api.v1.ListEventsRequest
import com.kuaishou.klingai.agentk.api.v1.ListEventsResponse
import com.kuaishou.klingai.agentk.api.v1.ListPipelinesRequest
import com.kuaishou.klingai.agentk.api.v1.ListPipelinesResponse
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsRequest
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsResponse
import com.kuaishou.klingai.agentk.api.v1.ListSessionsRequest
import com.kuaishou.klingai.agentk.api.v1.ListSessionsResponse
import com.kuaishou.klingai.agentk.api.v1.StartExecutionRequest
import com.kuaishou.klingai.agentk.api.v1.StartExecutionResponse
import com.kuaishou.klingai.agentk.api.v1.WatchEventsRequest
import com.kuaishou.klingai.agentk.api.v1.WhoAmIRequest
import com.kuaishou.klingai.agentk.api.v1.WhoAmIResponse
import com.kuaishou.klingai.agentk.client.AgentKClient
import com.kuaishou.klingai.agentk.client.EventObserver
import com.kuaishou.klingai.agentk.client.EventSubscription
import io.opentelemetry.api.GlobalOpenTelemetry
import io.opentelemetry.api.trace.Span
import io.opentelemetry.api.trace.SpanKind
import io.opentelemetry.api.trace.StatusCode
import io.opentelemetry.api.trace.propagation.W3CTraceContextPropagator
import io.opentelemetry.context.Context
import io.opentelemetry.context.propagation.TextMapSetter
import io.opentelemetry.extension.kotlin.asContextElement
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.future.future
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.concurrent.CompletableFuture
import java.util.concurrent.CompletionStage
import java.util.concurrent.atomic.AtomicBoolean

/** Internal implementation behind every public transport factory. */
public class CoreAgentKClient(
    private val provider: AgentKServiceProvider,
    baseHeaders: Map<String, List<String>> = KessIdentity.headers(),
) : AgentKClient {
    private val closed = AtomicBoolean(false)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val baseHeaders: Headers = baseHeaders.mapValues { (_, values) -> values.toList() }

    override fun createSession(request: CreateSessionRequest): CompletionStage<CreateSessionResponse> =
        unary("CreateSession") { rpc, headers -> rpc.createSession(request, headers) }

    override fun listSessions(request: ListSessionsRequest): CompletionStage<ListSessionsResponse> =
        unary("ListSessions") { rpc, headers -> rpc.listSessions(request, headers) }

    override fun startExecution(request: StartExecutionRequest): CompletionStage<StartExecutionResponse> =
        unary("StartExecution") { rpc, headers -> rpc.startExecution(request, headers) }

    override fun cancelExecution(request: CancelExecutionRequest): CompletionStage<CancelExecutionResponse> =
        unary("CancelExecution") { rpc, headers -> rpc.cancelExecution(request, headers) }

    override fun listEvents(request: ListEventsRequest): CompletionStage<ListEventsResponse> =
        unary("ListEvents") { rpc, headers -> rpc.listEvents(request, headers) }

    override fun listPipelines(request: ListPipelinesRequest): CompletionStage<ListPipelinesResponse> =
        unary("ListPipelines") { rpc, headers -> rpc.listPipelines(request, headers) }

    override fun listExecutions(request: ListExecutionsRequest): CompletionStage<ListExecutionsResponse> =
        unary("ListExecutions") { rpc, headers -> rpc.listExecutions(request, headers) }

    override fun getExecution(request: GetExecutionRequest): CompletionStage<GetExecutionResponse> =
        unary("GetExecution") { rpc, headers -> rpc.getExecution(request, headers) }

    override fun listCapabilities(request: ListCapabilitiesRequest): CompletionStage<ListCapabilitiesResponse> =
        unary("ListCapabilities") { rpc, headers -> rpc.listCapabilities(request, headers) }

    override fun invokeCapability(request: InvokeCapabilityRequest): CompletionStage<InvokeCapabilityResponse> =
        unary("InvokeCapability") { rpc, headers -> rpc.invokeCapability(request, headers) }

    override fun whoAmI(request: WhoAmIRequest): CompletionStage<WhoAmIResponse> =
        unary("WhoAmI") { rpc, headers -> rpc.whoAmI(request, headers) }

    override fun watchEvents(
        request: WatchEventsRequest,
        observer: EventObserver,
    ): EventSubscription {
        checkOpen()
        val parent = Context.current()
        val job = scope.launch {
            val span = startSpan("WatchEvents", parent)
            val rpcContext = parent.with(span)
            try {
                withContext(rpcContext.asContextElement()) {
                    var stream: com.connectrpc.ServerOnlyStreamInterface<WatchEventsRequest, com.kuaishou.klingai.agentk.api.v1.WatchEventsResponse>? =
                        null
                    try {
                        val headers = requestHeaders(rpcContext)
                        stream = provider.acquire().watchEvents(headers)
                        stream.sendAndClose(request).getOrThrow()
                        for (event in stream.responseChannel()) {
                            observer.onNext(event)
                        }
                        observer.onComplete()
                    } catch (cancelled: CancellationException) {
                        throw cancelled
                    } catch (error: Throwable) {
                        recordError(span, error)
                        observer.onError(error)
                    } finally {
                        withContext(NonCancellable) {
                            stream?.receiveClose()
                        }
                    }
                }
            } finally {
                span.end()
            }
        }
        return JobEventSubscription(job)
    }

    override fun close() {
        if (!closed.compareAndSet(false, true)) {
            return
        }
        scope.cancel()
        provider.close()
    }

    private fun <T : Any> unary(
        method: String,
        call: suspend (AgentKServiceClient, Headers) -> ResponseMessage<T>,
    ): CompletionStage<T> {
        if (closed.get()) {
            return failedClosedStage()
        }
        val parent = Context.current()
        return scope.future {
            val span = startSpan(method, parent)
            val rpcContext = parent.with(span)
            try {
                withContext(rpcContext.asContextElement()) {
                    checkOpen()
                    val headers = requestHeaders(rpcContext)
                    call(provider.acquire(), headers).getOrThrow()
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                recordError(span, error)
                throw error
            } finally {
                span.end()
            }
        }
    }

    private fun startSpan(method: String, parent: Context): Span =
        GlobalOpenTelemetry.getOrNoop()
            .getTracer(INSTRUMENTATION_SCOPE)
            .spanBuilder("$SERVICE_NAME/$method")
            .setParent(parent)
            .setSpanKind(SpanKind.CLIENT)
            .startSpan()

    private fun requestHeaders(context: Context): Headers =
        baseHeaders.toMutableMap().also { headers ->
            W3CTraceContextPropagator.getInstance().inject(context, headers, HeaderSetter)
        }

    private fun recordError(span: Span, error: Throwable) {
        span.recordException(error)
        span.setStatus(StatusCode.ERROR)
    }

    private fun checkOpen() {
        check(!closed.get()) { CLOSED_MESSAGE }
    }

    private fun <T> failedClosedStage(): CompletionStage<T> =
        CompletableFuture<T>().also {
            it.completeExceptionally(IllegalStateException(CLOSED_MESSAGE))
        }

    private class JobEventSubscription(
        private val job: Job,
    ) : EventSubscription {
        override fun cancel() {
            job.cancel()
        }

        override fun isCancelled(): Boolean = job.isCancelled
    }

    private companion object {
        const val INSTRUMENTATION_SCOPE = "com.kuaishou.klingai.agentk.client"
        const val SERVICE_NAME = "agentk.api.v1.AgentKService"
        const val CLOSED_MESSAGE = "agentk client: client is closed"

        object HeaderSetter : TextMapSetter<MutableMap<String, List<String>>> {
            override fun set(carrier: MutableMap<String, List<String>>?, key: String, value: String) {
                carrier?.set(key, listOf(value))
            }
        }
    }
}
