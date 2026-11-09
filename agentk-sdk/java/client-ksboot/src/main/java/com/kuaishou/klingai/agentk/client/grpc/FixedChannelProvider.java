package com.kuaishou.klingai.agentk.client.grpc;

import java.net.URI;
import java.util.concurrent.atomic.AtomicBoolean;

import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;

final class FixedChannelProvider implements ChannelProvider {
    private final ManagedChannel channel;
    private final AtomicBoolean closed = new AtomicBoolean(false);

    FixedChannelProvider(String endpoint) {
        URI uri = URI.create(endpoint);
        int port = uri.getPort();
        ManagedChannelBuilder<?> builder;
        if ("http".equalsIgnoreCase(uri.getScheme())) {
            builder = ManagedChannelBuilder.forAddress(uri.getHost(), port < 0 ? 80 : port)
                    .usePlaintext();
        } else if ("https".equalsIgnoreCase(uri.getScheme())) {
            builder = ManagedChannelBuilder.forAddress(uri.getHost(), port < 0 ? 443 : port)
                    .useTransportSecurity();
        } else {
            throw new IllegalArgumentException(
                    "agentk grpc: endpoint must use http or https: \"" + endpoint + "\"");
        }
        this.channel = builder.build();
    }

    @Override
    public ManagedChannel acquire() {
        if (closed.get()) {
            throw new IllegalStateException("agentk client: client is closed");
        }
        return channel;
    }

    @Override
    public void close() {
        if (closed.compareAndSet(false, true)) {
            channel.shutdownNow();
        }
    }
}
