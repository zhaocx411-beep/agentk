package com.kuaishou.klingai.agentk.client.grpc

import com.kuaishou.kess.configurator.ConfigFacade
import com.kuaishou.kess.scheduler.OptionsForCaller
import com.kuaishou.kess.scheduler.SchedulerFacade

internal data class Endpoint(
    val host: String,
    val port: Int,
    val weight: Double,
    val lane: String = "",
) {
    fun baseUrl(): String {
        val urlHost = if (host.contains(':') && !host.startsWith('[')) "[$host]" else host
        return "http://$urlHost:$port"
    }
}

internal fun interface EndpointSource {
    fun endpoints(): List<Endpoint>
}

internal data class EndpointAddress(
    val host: String,
    val port: Int,
)

internal class KessEndpointSource(
    division: String = ConfigFacade.localDivision(),
    caller: String = System.getenv("KWS_SERVICE_NAME").orEmpty().trim(),
) : EndpointSource {
    private val scheduler = SchedulerFacade
        .getProvider(division)
        .getSchedulerForCaller2(
            OptionsForCaller(caller, SERVICE_NAME).setLocationSelector(null),
        )

    override fun endpoints(): List<Endpoint> =
        scheduler.routeTable.instances.map { instance ->
            Endpoint(
                host = instance.host,
                port = selectPort(instance.entries),
                weight = instance.weight,
                lane = instance.lane.orEmpty(),
            )
        }

    private companion object {
        const val SERVICE_NAME = "klingai-agentk-api"
    }
}

internal fun selectPort(entries: Map<String, Int>): Int =
    entries["grpc"] ?: entries["http"] ?: 0

internal object WeightedEndpointPicker {
    /** Uncolored requests only go to baseline nodes; lane nodes never receive them. */
    fun pickBaseline(endpoints: List<Endpoint>, random: () -> Double): Endpoint =
        pick(endpoints.filter { it.lane.isEmpty() }, random)

    fun pick(
        endpoints: List<Endpoint>,
        random: () -> Double,
    ): Endpoint {
        check(endpoints.isNotEmpty()) { "agentk grpc: KESS returned no nodes" }

        val eligible = ArrayList<Endpoint>(endpoints.size)
        var total = 0.0
        endpoints.forEachIndexed { index, endpoint ->
            check(endpoint.host.isNotBlank()) {
                "agentk grpc: KESS node $index has empty host"
            }
            check(endpoint.port in 1..65535) {
                "agentk grpc: KESS node $index has invalid port ${endpoint.port}"
            }
            if (endpoint.weight <= 0.0) {
                return@forEachIndexed
            }
            check(endpoint.weight.isFinite()) {
                "agentk grpc: KESS node $index has invalid weight ${endpoint.weight}"
            }
            eligible += endpoint
            total += endpoint.weight
        }

        check(eligible.isNotEmpty()) {
            "agentk grpc: KESS returned no positive-weight nodes"
        }

        val target = random() * total
        var cumulative = 0.0
        for (endpoint in eligible) {
            cumulative += endpoint.weight
            if (target < cumulative) {
                return endpoint
            }
        }
        return eligible.last()
    }
}
