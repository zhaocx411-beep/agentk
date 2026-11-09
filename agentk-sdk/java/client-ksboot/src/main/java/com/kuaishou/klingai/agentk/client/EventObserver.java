package com.kuaishou.klingai.agentk.client;

import com.kuaishou.klingai.agentk.api.v1.WatchEventsResponse;

/** Receives frames and terminal signals from a WatchEvents stream. */
public interface EventObserver {
    void onNext(WatchEventsResponse event);

    void onError(Throwable error);

    void onComplete();
}
