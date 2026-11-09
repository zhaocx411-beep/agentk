package com.kuaishou.klingai.agentk.client.grpc;

import java.util.function.Function;

import com.kuaishou.kess.proto.scheduler.KwsModelPb;

import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ForwardingClientCall;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;

final class KessIdentity implements ClientInterceptor {
    private static final Metadata.Key<byte[]> KWS_BINARY = Metadata.Key.of(
            "kws-bin", Metadata.BINARY_BYTE_MARSHALLER);
    private static final Metadata.Key<String> KWS_CREDENTIAL = Metadata.Key.of(
            "kwsCredential", Metadata.ASCII_STRING_MARSHALLER);

    private final byte[] kwsBinary;
    private final String credential;

    private KessIdentity(byte[] kwsBinary, String credential) {
        this.kwsBinary = kwsBinary == null ? null : kwsBinary.clone();
        this.credential = credential;
    }

    static KessIdentity fromEnvironment() {
        return from(System::getenv);
    }

    static KessIdentity from(Function<String, String> getenv) {
        String serviceName = getenv.apply("KWS_SERVICE_NAME");
        String credential = getenv.apply("KWS_SERVICE_CREDENTIAL");
        byte[] binary = null;
        if (serviceName != null && !serviceName.isEmpty()) {
            binary = KwsModelPb.newBuilder()
                    .setServiceName(serviceName)
                    .build()
                    .toByteArray();
        }
        return new KessIdentity(
                binary,
                credential == null || credential.isEmpty() ? null : credential);
    }

    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
            MethodDescriptor<ReqT, RespT> method, CallOptions options, Channel next) {
        return new ForwardingClientCall.SimpleForwardingClientCall<>(
                next.newCall(method, options)) {
            @Override
            public void start(Listener<RespT> responseListener, Metadata headers) {
                apply(headers);
                super.start(responseListener, headers);
            }
        };
    }

    void apply(Metadata headers) {
        if (kwsBinary != null) {
            headers.put(KWS_BINARY, kwsBinary.clone());
        }
        if (credential != null) {
            headers.put(KWS_CREDENTIAL, credential);
        }
    }
}
