package com.kuaishou.klingai.agentk.client.internal

import com.connectrpc.Headers
import com.kuaishou.kess.proto.scheduler.KwsModelPb
import java.util.Base64

internal object KessIdentity {
    fun headers(getenv: (String) -> String? = System::getenv): Headers = buildMap {
        getenv("KWS_SERVICE_NAME")?.takeIf(String::isNotEmpty)?.let { serviceName ->
            val model = KwsModelPb.newBuilder()
                .setServiceName(serviceName)
                .build()
            put(
                "kws-bin",
                listOf(Base64.getEncoder().withoutPadding().encodeToString(model.toByteArray())),
            )
        }
        getenv("KWS_SERVICE_CREDENTIAL")?.takeIf(String::isNotEmpty)?.let { credential ->
            put("kwsCredential", listOf(credential))
        }
    }
}
