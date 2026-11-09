package com.kuaishou.klingai.agentk.client.grpc;

import io.grpc.ManagedChannel;

interface ChannelProvider extends AutoCloseable {
    ManagedChannel acquire();

    @Override
    void close();
}
