package com.kuaishou.klingai.agentk.client.grpc;

import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicBoolean;

import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;

final class KessChannelProvider implements ChannelProvider {
    private final EndpointSource endpointSource = new KessEndpointSource();
    private final Map<EndpointAddress, ManagedChannel> channels = new ConcurrentHashMap<>();
    private final AtomicBoolean closed = new AtomicBoolean(false);

    @Override
    public ManagedChannel acquire() {
        if (closed.get()) {
            throw new IllegalStateException("agentk client: client is closed");
        }
        Endpoint endpoint = WeightedEndpointPicker.pickBaseline(endpointSource.endpoints());
        EndpointAddress address = new EndpointAddress(endpoint.host, endpoint.port);
        return channels.computeIfAbsent(
                address,
                ignored -> ManagedChannelBuilder.forAddress(endpoint.host, endpoint.port)
                        .usePlaintext()
                        .build());
    }

    @Override
    public void close() {
        if (!closed.compareAndSet(false, true)) {
            return;
        }
        for (ManagedChannel channel : channels.values()) {
            channel.shutdownNow();
        }
        channels.clear();
    }
}
