package com.kuaishou.klingai.agentk.client.grpc;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

import java.io.IOException;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

import com.kuaishou.klingai.agentk.api.v1.AgentKServiceGrpc;
import com.kuaishou.klingai.agentk.api.v1.InvokeCapabilityRequest;
import com.kuaishou.klingai.agentk.api.v1.InvokeCapabilityResponse;
import com.kuaishou.klingai.agentk.api.v1.WatchEventsRequest;
import com.kuaishou.klingai.agentk.api.v1.WatchEventsResponse;
import com.kuaishou.klingai.agentk.api.v1.WhoAmIRequest;
import com.kuaishou.klingai.agentk.api.v1.WhoAmIResponse;
import com.kuaishou.klingai.agentk.client.ClientOptions;
import com.kuaishou.klingai.agentk.client.AgentKClient;
import com.kuaishou.klingai.agentk.client.EventObserver;
import com.kuaishou.klingai.agentk.client.EventSubscription;

import io.grpc.Context;
import io.grpc.Contexts;
import io.grpc.Metadata;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.ServerInterceptors;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;
import org.junit.After;
import org.junit.Test;

public class GrpcWireTest {
    private static final Metadata.Key<String> AUTHORIZATION = Metadata.Key.of(
            "authorization", Metadata.ASCII_STRING_MARSHALLER);
    private static final Metadata.Key<byte[]> KWS_BINARY = Metadata.Key.of(
            "kws-bin", Metadata.BINARY_BYTE_MARSHALLER);
    private static final Metadata.Key<String> KWS_CREDENTIAL = Metadata.Key.of(
            "kwsCredential", Metadata.ASCII_STRING_MARSHALLER);
    private static final byte[] TEST_KWS_BINARY = java.util.Base64.getDecoder()
            .decode("ChlhZ2VudGsuc2RrLmZpeHR1cmUv5Y6f5qC3");

    private Server server;
    private AgentKClient client;

    @After
    public void tearDown() throws InterruptedException {
        if (client != null) {
            client.close();
        }
        if (server != null) {
            server.shutdownNow();
            server.awaitTermination(5, TimeUnit.SECONDS);
        }
    }

    @Test
    public void unaryUsesH2cAndKessMetadata() throws Exception {
        AtomicReference<Metadata> metadata = new AtomicReference<>();
        startServer(
                new AgentKServiceGrpc.AgentKServiceImplBase() {
                    @Override
                    public void whoAmI(
                            WhoAmIRequest request, StreamObserver<WhoAmIResponse> observer) {
                        observer.onNext(WhoAmIResponse.newBuilder().setId("caller").build());
                        observer.onCompleted();
                    }

                    @Override
                    public void invokeCapability(
                            InvokeCapabilityRequest request,
                            StreamObserver<InvokeCapabilityResponse> observer) {
                        observer.onNext(InvokeCapabilityResponse.getDefaultInstance());
                        observer.onCompleted();
                    }
                },
                captureMetadata(metadata));

        client = newClient(server.getPort());
        WhoAmIResponse response = client.whoAmI(WhoAmIRequest.getDefaultInstance())
                .toCompletableFuture()
                .get(2, TimeUnit.SECONDS);
        InvokeCapabilityResponse invokeResponse = client.invokeCapability(
                        InvokeCapabilityRequest.newBuilder()
                                .setCapabilityId("contrib.test.render")
                                .build())
                .toCompletableFuture()
                .get(2, TimeUnit.SECONDS);

        assertEquals("caller", response.getId());
        assertEquals(InvokeCapabilityResponse.getDefaultInstance(), invokeResponse);
        assertArrayEquals(TEST_KWS_BINARY, metadata.get().get(KWS_BINARY));
        assertEquals("test-credential", metadata.get().get(KWS_CREDENTIAL));
        assertNull(metadata.get().get(AUTHORIZATION));
    }

    @Test
    public void watchEventsDeliversFramesAndCompletion() throws Exception {
        AtomicReference<Metadata> metadata = new AtomicReference<>();
        startServer(
                new AgentKServiceGrpc.AgentKServiceImplBase() {
                    @Override
                    public void watchEvents(
                            WatchEventsRequest request,
                            StreamObserver<WatchEventsResponse> observer) {
                        observer.onNext(WatchEventsResponse.getDefaultInstance());
                        observer.onNext(WatchEventsResponse.getDefaultInstance());
                        observer.onCompleted();
                    }
                },
                captureMetadata(metadata));
        client = newClient(server.getPort());
        CountDownLatch done = new CountDownLatch(1);
        AtomicInteger frames = new AtomicInteger();
        AtomicReference<Throwable> failure = new AtomicReference<>();

        client.watchEvents(
                WatchEventsRequest.getDefaultInstance(),
                new EventObserver() {
                    @Override
                    public void onNext(WatchEventsResponse event) {
                        frames.incrementAndGet();
                    }

                    @Override
                    public void onError(Throwable error) {
                        failure.set(error);
                        done.countDown();
                    }

                    @Override
                    public void onComplete() {
                        done.countDown();
                    }
                });

        assertTrue(done.await(2, TimeUnit.SECONDS));
        assertEquals(2, frames.get());
        assertNull(failure.get());
        assertArrayEquals(TEST_KWS_BINARY, metadata.get().get(KWS_BINARY));
        assertEquals("test-credential", metadata.get().get(KWS_CREDENTIAL));
        assertNull(metadata.get().get(AUTHORIZATION));
    }

    @Test
    public void watchEventsDeliversGrpcError() throws Exception {
        startServer(new AgentKServiceGrpc.AgentKServiceImplBase() {
            @Override
            public void watchEvents(
                    WatchEventsRequest request, StreamObserver<WatchEventsResponse> observer) {
                observer.onError(Status.PERMISSION_DENIED.asRuntimeException());
            }
        });
        client = newClient(server.getPort());
        CountDownLatch done = new CountDownLatch(1);
        AtomicReference<Throwable> failure = new AtomicReference<>();

        client.watchEvents(
                WatchEventsRequest.getDefaultInstance(),
                observer(failure, done));

        assertTrue(done.await(2, TimeUnit.SECONDS));
        assertEquals(
                Status.Code.PERMISSION_DENIED,
                Status.fromThrowable(failure.get()).getCode());
    }

    @Test
    public void observerFailureCancelsStreamAndIsReported() throws Exception {
        CountDownLatch serverCancelled = new CountDownLatch(1);
        startServer(new AgentKServiceGrpc.AgentKServiceImplBase() {
            @Override
            public void watchEvents(
                    WatchEventsRequest request, StreamObserver<WatchEventsResponse> observer) {
                Context.current().addListener(
                        ignored -> serverCancelled.countDown(), Runnable::run);
                observer.onNext(WatchEventsResponse.getDefaultInstance());
            }
        });
        client = newClient(server.getPort());
        CountDownLatch done = new CountDownLatch(1);
        AtomicReference<Throwable> failure = new AtomicReference<>();
        IllegalStateException expected = new IllegalStateException("observer failed");

        client.watchEvents(
                WatchEventsRequest.getDefaultInstance(),
                new EventObserver() {
                    @Override
                    public void onNext(WatchEventsResponse event) {
                        throw expected;
                    }

                    @Override
                    public void onError(Throwable error) {
                        failure.set(error);
                        done.countDown();
                    }

                    @Override
                    public void onComplete() {
                        done.countDown();
                    }
                });

        assertTrue(done.await(2, TimeUnit.SECONDS));
        assertSame(expected, failure.get());
        assertTrue(serverCancelled.await(2, TimeUnit.SECONDS));
    }

    @Test
    public void cancellationStopsStreamWithoutTerminalCallback() throws Exception {
        CountDownLatch frameSent = new CountDownLatch(1);
        CountDownLatch serverCancelled = new CountDownLatch(1);
        startServer(new AgentKServiceGrpc.AgentKServiceImplBase() {
            @Override
            public void watchEvents(
                    WatchEventsRequest request, StreamObserver<WatchEventsResponse> observer) {
                Context.current().addListener(
                        ignored -> serverCancelled.countDown(), Runnable::run);
                observer.onNext(WatchEventsResponse.getDefaultInstance());
                frameSent.countDown();
            }
        });
        client = newClient(server.getPort());
        AtomicInteger terminalCallbacks = new AtomicInteger();
        EventSubscription subscription = client.watchEvents(
                WatchEventsRequest.getDefaultInstance(),
                new EventObserver() {
                    @Override
                    public void onNext(WatchEventsResponse event) {
                    }

                    @Override
                    public void onError(Throwable error) {
                        terminalCallbacks.incrementAndGet();
                    }

                    @Override
                    public void onComplete() {
                        terminalCallbacks.incrementAndGet();
                    }
                });

        assertTrue(frameSent.await(2, TimeUnit.SECONDS));
        subscription.cancel();
        assertTrue(subscription.isCancelled());
        assertTrue(serverCancelled.await(2, TimeUnit.SECONDS));
        assertEquals(0, terminalCallbacks.get());
    }

    @Test
    public void cancellingUnaryStageCancelsGrpcCall() throws Exception {
        CountDownLatch callStarted = new CountDownLatch(1);
        CountDownLatch serverCancelled = new CountDownLatch(1);
        startServer(new AgentKServiceGrpc.AgentKServiceImplBase() {
            @Override
            public void whoAmI(
                    WhoAmIRequest request, StreamObserver<WhoAmIResponse> observer) {
                Context.current().addListener(
                        ignored -> serverCancelled.countDown(), Runnable::run);
                callStarted.countDown();
            }
        });
        client = newClient(server.getPort());
        CompletableFuture<WhoAmIResponse> stage = client
                .whoAmI(WhoAmIRequest.getDefaultInstance())
                .toCompletableFuture();

        assertTrue(callStarted.await(2, TimeUnit.SECONDS));
        assertTrue(stage.cancel(true));
        assertTrue(serverCancelled.await(2, TimeUnit.SECONDS));
        assertTrue(stage.isCancelled());
    }

    @Test
    public void closingClientStopsStreamWithoutTerminalCallback() throws Exception {
        CountDownLatch frameSent = new CountDownLatch(1);
        CountDownLatch serverCancelled = new CountDownLatch(1);
        startServer(new AgentKServiceGrpc.AgentKServiceImplBase() {
            @Override
            public void watchEvents(
                    WatchEventsRequest request, StreamObserver<WatchEventsResponse> observer) {
                Context.current().addListener(
                        ignored -> serverCancelled.countDown(), Runnable::run);
                observer.onNext(WatchEventsResponse.getDefaultInstance());
                frameSent.countDown();
            }
        });
        client = newClient(server.getPort());
        AtomicInteger terminalCallbacks = new AtomicInteger();
        client.watchEvents(
                WatchEventsRequest.getDefaultInstance(),
                new EventObserver() {
                    @Override
                    public void onNext(WatchEventsResponse event) {
                    }

                    @Override
                    public void onError(Throwable error) {
                        terminalCallbacks.incrementAndGet();
                    }

                    @Override
                    public void onComplete() {
                        terminalCallbacks.incrementAndGet();
                    }
                });

        assertTrue(frameSent.await(2, TimeUnit.SECONDS));
        client.close();
        client = null;
        assertTrue(serverCancelled.await(2, TimeUnit.SECONDS));
        assertEquals(0, terminalCallbacks.get());
    }

    private void startServer(AgentKServiceGrpc.AgentKServiceImplBase service)
            throws IOException {
        server = ServerBuilder.forPort(0).addService(service).build().start();
    }

    private void startServer(
            AgentKServiceGrpc.AgentKServiceImplBase service, ServerInterceptor interceptor)
            throws IOException {
        server = ServerBuilder.forPort(0)
                .addService(ServerInterceptors.intercept(service, interceptor))
                .build()
                .start();
    }

    private AgentKClient newClient(int port) {
        return AgentKGrpcClient.create(
                ClientOptions.forEndpoint("http://127.0.0.1:" + port),
                KessIdentity.from(key -> {
                    if ("KWS_SERVICE_NAME".equals(key)) {
                        return "agentk.sdk.fixture/原样";
                    }
                    if ("KWS_SERVICE_CREDENTIAL".equals(key)) {
                        return "test-credential";
                    }
                    return null;
                }));
    }

    private static ServerInterceptor captureMetadata(AtomicReference<Metadata> value) {
        return new ServerInterceptor() {
            @Override
            public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
                    ServerCall<ReqT, RespT> call,
                    Metadata headers,
                    ServerCallHandler<ReqT, RespT> next) {
                value.set(headers);
                return Contexts.interceptCall(Context.current(), call, headers, next);
            }
        };
    }

    private static EventObserver observer(
            AtomicReference<Throwable> failure, CountDownLatch done) {
        return new EventObserver() {
            @Override
            public void onNext(WatchEventsResponse event) {
            }

            @Override
            public void onError(Throwable error) {
                failure.set(error);
                done.countDown();
            }

            @Override
            public void onComplete() {
                done.countDown();
            }
        };
    }
}
