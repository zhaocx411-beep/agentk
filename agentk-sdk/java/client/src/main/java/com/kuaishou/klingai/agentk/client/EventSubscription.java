package com.kuaishou.klingai.agentk.client;

/** A cancellable subscription to a live AgentK event stream. */
public interface EventSubscription extends AutoCloseable {
    /**
     * Stops receiving events.
     *
     * <p>Cancellation does not invoke the observer's terminal callbacks.
     */
    void cancel();

    /**
     * Reports whether this subscription has been cancelled.
     *
     * @return {@code true} after cancellation
     */
    boolean isCancelled();

    /** Cancels this subscription. */
    @Override
    default void close() {
        cancel();
    }
}
