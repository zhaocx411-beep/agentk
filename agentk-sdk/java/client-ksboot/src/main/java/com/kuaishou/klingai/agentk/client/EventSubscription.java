package com.kuaishou.klingai.agentk.client;

/** A cancellable subscription to a live AgentK event stream. */
public interface EventSubscription extends AutoCloseable {
    void cancel();

    boolean isCancelled();

    @Override
    default void close() {
        cancel();
    }
}
