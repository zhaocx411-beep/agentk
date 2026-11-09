package com.kuaishou.klingai.agentk.client.grpc

import com.kuaishou.klingai.agentk.api.v1.WhoAmIRequest
import com.kuaishou.klingai.agentk.client.ClientOptions
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.util.concurrent.TimeUnit

class KessIntegrationTest {
    @Test
    fun `resolves KESS and calls the real service when explicitly enabled`() {
        assumeTrue(System.getenv("AGENTK_SDK_TEST_KESS") == "1")
        AgentKGrpcClient.create(ClientOptions()).use { client ->
            client.whoAmI(WhoAmIRequest.getDefaultInstance())
                .toCompletableFuture()
                .get(15, TimeUnit.SECONDS)
        }
    }
}
