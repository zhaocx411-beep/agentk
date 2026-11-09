package com.kuaishou.klingai.agentk.client.grpc;

import java.util.Set;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CompletionException;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

import com.google.common.util.concurrent.ListenableFuture;
import com.kuaishou.klingai.agentk.api.v1.AgentKServiceGrpc;
import com.kuaishou.klingai.agentk.api.v1.CancelExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.CancelExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.CreateSessionRequest;
import com.kuaishou.klingai.agentk.api.v1.CreateSessionResponse;
import com.kuaishou.klingai.agentk.api.v1.GetExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.GetExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.InvokeCapabilityRequest;
import com.kuaishou.klingai.agentk.api.v1.InvokeCapabilityResponse;
import com.kuaishou.klingai.agentk.api.v1.ListCapabilitiesRequest;
import com.kuaishou.klingai.agentk.api.v1.ListCapabilitiesResponse;
import com.kuaishou.klingai.agentk.api.v1.ListEventsRequest;
import com.kuaishou.klingai.agentk.api.v1.ListEventsResponse;
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsRequest;
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsResponse;
import com.kuaishou.klingai.agentk.api.v1.ListPipelinesRequest;
import com.kuaishou.klingai.agentk.api.v1.ListPipelinesResponse;
import com.kuaishou.klingai.agentk.api.v1.ListSessionsRequest;
import com.kuaishou.klingai.agentk.api.v1.ListSessionsResponse;
import com.kuaishou.klingai.agentk.api.v1.StartExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.StartExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.WatchEventsRequest;
import com.kuaishou.klingai.agentk.api.v1.WatchEventsResponse;
import com.kuaishou.klingai.agentk.api.v1.WhoAmIRequest;
import com.kuaishou.klingai.agentk.api.v1.WhoAmIResponse;
import com.kuaishou.klingai.agentk.client.AgentKClient;
import com.kuaishou.klingai.agentk.client.EventObserver;
import com.kuaishou.klingai.agentk.client.EventSubscription;

import io.grpc.ClientInterceptor;
import io.grpc.stub.ClientCallStreamObserver;
import io.grpc.stub.ClientResponseObserver;

final class KsBootAgentKClient implements AgentKClient {
    private static final long UNARY_TIMEOUT_SECONDS = 10;
    private static final String CLOSED_MESSAGE = "agentk client: client is closed";

    private final ChannelProvider channels;
    private final ClientInterceptor identity;
    private final AtomicBoolean closed = new AtomicBoolean(false);
    private final Set<GrpcEventSubscription> subscriptions =
            java.util.concurrent.ConcurrentHashMap.newKeySet();

    KsBootAgentKClient(ChannelProvider channels, ClientInterceptor identity) {
        this.channels = channels;
        this.identity = identity;
    }

    @Override
    public CompletionStage<CreateSessionResponse> createSession(CreateSessionRequest request) {
        return unary(request, stub -> stub.createSession(request));
    }

    @Override
    public CompletionStage<ListSessionsResponse> listSessions(ListSessionsRequest request) {
        return unary(request, stub -> stub.listSessions(request));
    }

    @Override
    public CompletionStage<StartExecutionResponse> startExecution(StartExecutionRequest request) {
        return unary(request, stub -> stub.startExecution(request));
    }

    @Override
    public CompletionStage<CancelExecutionResponse> cancelExecution(CancelExecutionRequest request) {
        return unary(request, stub -> stub.cancelExecution(request));
    }

    @Override
    public CompletionStage<ListEventsResponse> listEvents(ListEventsRequest request) {
        return unary(request, stub -> stub.listEvents(request));
    }

    @Override
    public EventSubscription watchEvents(WatchEventsRequest request, EventObserver observer) {
        requireOpen();
        if (request == null || observer == null) {
            throw new IllegalArgumentException(
                    "agentk client: watch request and observer are required");
        }

        GrpcEventSubscription subscription = new GrpcEventSubscription(observer);
        subscriptions.add(subscription);
        try {
            AgentKServiceGrpc.newStub(channels.acquire())
                    .withInterceptors(identity)
                    .watchEvents(request, subscription.responseObserver());
        } catch (Throwable error) {
            subscription.fail(error);
        }
        return subscription;
    }

    @Override
    public CompletionStage<ListPipelinesResponse> listPipelines(ListPipelinesRequest request) {
        return unary(request, stub -> stub.listPipelines(request));
    }

    @Override
    public CompletionStage<ListExecutionsResponse> listExecutions(ListExecutionsRequest request) {
        return unary(request, stub -> stub.listExecutions(request));
    }

    @Override
    public CompletionStage<GetExecutionResponse> getExecution(GetExecutionRequest request) {
        return unary(request, stub -> stub.getExecution(request));
    }

    @Override
    public CompletionStage<ListCapabilitiesResponse> listCapabilities(
            ListCapabilitiesRequest request) {
        return unary(request, stub -> stub.listCapabilities(request));
    }

    @Override
    public CompletionStage<InvokeCapabilityResponse> invokeCapability(
            InvokeCapabilityRequest request) {
        return unary(request, stub -> stub.invokeCapability(request), false);
    }

    @Override
    public CompletionStage<WhoAmIResponse> whoAmI(WhoAmIRequest request) {
        return unary(request, stub -> stub.whoAmI(request));
    }

    @Override
    public void close() {
        if (!closed.compareAndSet(false, true)) {
            return;
        }
        for (GrpcEventSubscription subscription :
                subscriptions.toArray(new GrpcEventSubscription[0])) {
            subscription.cancel();
        }
        channels.close();
    }

    private <T> CompletionStage<T> unary(
            Object request,
            java.util.function.Function<AgentKServiceGrpc.AgentKServiceFutureStub,
                    ListenableFuture<T>>
                    call) {
        return unary(request, call, true);
    }

    private <T> CompletionStage<T> unary(
            Object request,
            java.util.function.Function<AgentKServiceGrpc.AgentKServiceFutureStub,
                    ListenableFuture<T>>
                    call,
            boolean withDeadline) {
        if (closed.get()) {
            return failedStage(new IllegalStateException(CLOSED_MESSAGE));
        }
        if (request == null) {
            return failedStage(
                    new IllegalArgumentException("agentk client: request is required"));
        }
        try {
            AgentKServiceGrpc.AgentKServiceFutureStub stub =
                    AgentKServiceGrpc.newFutureStub(channels.acquire())
                            .withInterceptors(identity);
            if (withDeadline) {
                stub = stub.withDeadlineAfter(UNARY_TIMEOUT_SECONDS, TimeUnit.SECONDS);
            }
            return bridge(call.apply(stub));
        } catch (Throwable error) {
            return failedStage(error);
        }
    }

    private void requireOpen() {
        if (closed.get()) {
            throw new IllegalStateException(CLOSED_MESSAGE);
        }
    }

    private static <T> CompletionStage<T> bridge(ListenableFuture<T> source) {
        CompletableFuture<T> target = new CompletableFuture<>();
        source.addListener(
                () -> {
                    try {
                        target.complete(source.get());
                    } catch (CancellationException cancelled) {
                        target.cancel(false);
                    } catch (ExecutionException failed) {
                        target.completeExceptionally(failed.getCause());
                    } catch (InterruptedException interrupted) {
                        Thread.currentThread().interrupt();
                        target.completeExceptionally(interrupted);
                    }
                },
                Runnable::run);
        target.whenComplete(
                (ignored, failure) -> {
                    if (target.isCancelled()) {
                        source.cancel(true);
                    }
                });
        return target;
    }

    private static <T> CompletionStage<T> failedStage(Throwable error) {
        CompletableFuture<T> failed = new CompletableFuture<>();
        failed.completeExceptionally(error);
        return failed;
    }

    private final class GrpcEventSubscription implements EventSubscription {
        private final EventObserver observer;
        private final AtomicBoolean cancelled = new AtomicBoolean(false);
        private final AtomicBoolean terminal = new AtomicBoolean(false);
        private volatile ClientCallStreamObserver<WatchEventsRequest> call;

        private GrpcEventSubscription(EventObserver observer) {
            this.observer = observer;
        }

        private ClientResponseObserver<WatchEventsRequest, WatchEventsResponse> responseObserver() {
            return new ClientResponseObserver<WatchEventsRequest, WatchEventsResponse>() {
                @Override
                public void beforeStart(
                        ClientCallStreamObserver<WatchEventsRequest> requestStream) {
                    call = requestStream;
                    if (cancelled.get()) {
                        requestStream.cancel("agentk client: subscription cancelled", null);
                    }
                }

                @Override
                public void onNext(WatchEventsResponse value) {
                    if (cancelled.get() || terminal.get()) {
                        return;
                    }
                    try {
                        observer.onNext(value);
                    } catch (Throwable error) {
                        fail(error);
                    }
                }

                @Override
                public void onError(Throwable error) {
                    fail(error);
                }

                @Override
                public void onCompleted() {
                    if (cancelled.get() || !terminal.compareAndSet(false, true)) {
                        return;
                    }
                    subscriptions.remove(GrpcEventSubscription.this);
                    try {
                        observer.onComplete();
                    } catch (Throwable ignored) {
                        // No callback remains after the observer's terminal notification.
                    }
                }
            };
        }

        private void fail(Throwable error) {
            if (cancelled.get() || !terminal.compareAndSet(false, true)) {
                return;
            }
            ClientCallStreamObserver<WatchEventsRequest> activeCall = call;
            if (activeCall != null) {
                activeCall.cancel("agentk client: event observer failed", error);
            }
            subscriptions.remove(this);
            try {
                observer.onError(unwrap(error));
            } catch (Throwable ignored) {
                // No callback remains after the observer's terminal notification.
            }
        }

        @Override
        public void cancel() {
            if (!cancelled.compareAndSet(false, true)) {
                return;
            }
            terminal.set(true);
            subscriptions.remove(this);
            ClientCallStreamObserver<WatchEventsRequest> activeCall = call;
            if (activeCall != null) {
                activeCall.cancel("agentk client: subscription cancelled", null);
            }
        }

        @Override
        public boolean isCancelled() {
            return cancelled.get();
        }
    }

    private static Throwable unwrap(Throwable error) {
        if ((error instanceof CompletionException || error instanceof ExecutionException)
                && error.getCause() != null) {
            return error.getCause();
        }
        return error;
    }

}
