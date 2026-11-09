package com.kuaishou.klingai.agentk.client;

import com.kuaishou.klingai.agentk.api.v1.WatchEventsResponse;

/**
 * Receives frames and terminal signals from a WatchEvents stream.
 *
 * <p>A normally completed stream calls {@link #onComplete()}; a stream failure or an exception
 * thrown by {@link #onNext(WatchEventsResponse)} is delivered to {@link #onError(Throwable)}.
 * Cancelling the stream does not invoke either terminal callback.
 */
public interface EventObserver {
    /**
     * Handles the next event frame.
     *
     * @param event a committed event, transient delta, or terminal stream frame
     */
    void onNext(WatchEventsResponse event);

    /**
     * Handles a stream or observer failure.
     *
     * @param error the failure that ended the stream
     */
    void onError(Throwable error);

    /** Handles a normal end of the event stream. */
    void onComplete();
}
