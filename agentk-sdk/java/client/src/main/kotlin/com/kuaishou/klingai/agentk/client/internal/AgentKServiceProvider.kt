package com.kuaishou.klingai.agentk.client.internal

import com.kuaishou.klingai.agentk.api.v1.AgentKServiceClient

/** Internal transport seam shared by the base client and optional transports. */
public interface AgentKServiceProvider : AutoCloseable {
    public fun acquire(): AgentKServiceClient

    override fun close()
}
