package com.kuaishou.klingai.agentk.client.grpc;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.concurrent.ThreadLocalRandom;
import java.util.function.DoubleSupplier;

import com.kuaishou.kess.configurator.ConfigFacade;
import com.kuaishou.kess.scheduler.OptionsForCaller;
import com.kuaishou.kess.scheduler.SchedulerFacade;
import com.kuaishou.kess.scheduler.SchedulerForCaller2;
import com.kuaishou.kess.scheduler.ServiceInstanceAttributes;

final class Endpoint {
    final String host;
    final int port;
    final double weight;
    final String lane;

    Endpoint(String host, int port, double weight) {
        this(host, port, weight, "");
    }

    Endpoint(String host, int port, double weight, String lane) {
        this.host = host;
        this.port = port;
        this.weight = weight;
        this.lane = lane == null ? "" : lane;
    }
}

interface EndpointSource {
    List<Endpoint> endpoints();
}

final class EndpointAddress {
    final String host;
    final int port;

    EndpointAddress(String host, int port) {
        this.host = host;
        this.port = port;
    }

    @Override
    public boolean equals(Object other) {
        if (this == other) {
            return true;
        }
        if (!(other instanceof EndpointAddress)) {
            return false;
        }
        EndpointAddress that = (EndpointAddress) other;
        return port == that.port && host.equals(that.host);
    }

    @Override
    public int hashCode() {
        return Objects.hash(host, port);
    }
}

final class KessEndpointSource implements EndpointSource {
    private static final String SERVICE_NAME = "klingai-agentk-api";

    private final SchedulerForCaller2 scheduler;

    KessEndpointSource() {
        this(ConfigFacade.localDivision(), normalizedCaller(System.getenv("KWS_SERVICE_NAME")));
    }

    KessEndpointSource(String division, String caller) {
        scheduler = SchedulerFacade.getProvider(division)
                .getSchedulerForCaller2(
                        new OptionsForCaller(caller, SERVICE_NAME).setLocationSelector(null));
    }

    @Override
    public List<Endpoint> endpoints() {
        List<ServiceInstanceAttributes> instances = scheduler.getRouteTable().getInstances();
        List<Endpoint> endpoints = new ArrayList<>(instances.size());
        for (ServiceInstanceAttributes instance : instances) {
            endpoints.add(new Endpoint(
                    instance.getHost(), selectPort(instance.getEntries()), instance.getWeight(), instance.getLane()));
        }
        return endpoints;
    }

    static int selectPort(Map<String, Integer> entries) {
        Integer grpc = entries.get("grpc");
        if (grpc != null) {
            return grpc;
        }
        Integer http = entries.get("http");
        return http == null ? 0 : http;
    }

    private static String normalizedCaller(String caller) {
        return caller == null ? "" : caller.trim();
    }
}

final class WeightedEndpointPicker {
    private WeightedEndpointPicker() {}

    static Endpoint pickBaseline(List<Endpoint> endpoints) {
        return pickBaseline(endpoints, () -> ThreadLocalRandom.current().nextDouble());
    }

    /** Uncolored requests only go to baseline nodes; lane nodes never receive them. */
    static Endpoint pickBaseline(List<Endpoint> endpoints, DoubleSupplier random) {
        List<Endpoint> baseline = new ArrayList<>();
        for (Endpoint endpoint : endpoints) {
            if (endpoint.lane.isEmpty()) {
                baseline.add(endpoint);
            }
        }
        return pick(baseline, random);
    }

    static Endpoint pick(List<Endpoint> endpoints) {
        return pick(endpoints, () -> ThreadLocalRandom.current().nextDouble());
    }

    static Endpoint pick(List<Endpoint> endpoints, DoubleSupplier random) {
        if (endpoints.isEmpty()) {
            throw new IllegalStateException("agentk grpc: KESS returned no nodes");
        }

        List<Endpoint> eligible = new ArrayList<>(endpoints.size());
        double total = 0.0;
        for (int index = 0; index < endpoints.size(); index++) {
            Endpoint endpoint = endpoints.get(index);
            if (endpoint.host == null || endpoint.host.trim().isEmpty()) {
                throw new IllegalStateException(
                        "agentk grpc: KESS node " + index + " has empty host");
            }
            if (endpoint.port < 1 || endpoint.port > 65535) {
                throw new IllegalStateException(
                        "agentk grpc: KESS node " + index + " has invalid port " + endpoint.port);
            }
            if (endpoint.weight <= 0.0) {
                continue;
            }
            if (!Double.isFinite(endpoint.weight)) {
                throw new IllegalStateException(
                        "agentk grpc: KESS node " + index + " has invalid weight " + endpoint.weight);
            }
            eligible.add(endpoint);
            total += endpoint.weight;
        }
        if (eligible.isEmpty()) {
            throw new IllegalStateException("agentk grpc: KESS returned no positive-weight nodes");
        }

        double target = random.getAsDouble() * total;
        double cumulative = 0.0;
        for (Endpoint endpoint : eligible) {
            cumulative += endpoint.weight;
            if (target < cumulative) {
                return endpoint;
            }
        }
        return eligible.get(eligible.size() - 1);
    }
}
