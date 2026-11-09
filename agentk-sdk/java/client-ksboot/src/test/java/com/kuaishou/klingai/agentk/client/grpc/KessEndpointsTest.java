package com.kuaishou.klingai.agentk.client.grpc;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertThrows;

import java.util.Arrays;
import java.util.Collections;
import java.util.HashMap;
import java.util.Map;

import org.junit.Test;

public class KessEndpointsTest {
    @Test
    public void prefersGrpcPortAndFallsBackToHttp() {
        Map<String, Integer> both = new HashMap<>();
        both.put("grpc", 9001);
        both.put("http", 8001);
        assertEquals(9001, KessEndpointSource.selectPort(both));
        assertEquals(8001, KessEndpointSource.selectPort(Collections.singletonMap("http", 8001)));
        assertEquals(0, KessEndpointSource.selectPort(Collections.emptyMap()));
    }

    @Test
    public void weightedPickerIsDeterministic() {
        java.util.List<Endpoint> endpoints =
                Arrays.asList(new Endpoint("one", 1, 1.0), new Endpoint("two", 2, 3.0));

        assertEquals("one", WeightedEndpointPicker.pick(endpoints, () -> 0.0).host);
        assertEquals("one", WeightedEndpointPicker.pick(endpoints, () -> 0.24).host);
        assertEquals("two", WeightedEndpointPicker.pick(endpoints, () -> 0.25).host);
        assertEquals("two", WeightedEndpointPicker.pick(endpoints, () -> 0.99).host);
    }

    @Test
    public void weightedPickerRejectsUnusableRouteTables() {
        assertThrows(
                IllegalStateException.class,
                () -> WeightedEndpointPicker.pick(Collections.emptyList(), () -> 0.0));
        assertThrows(
                IllegalStateException.class,
                () -> WeightedEndpointPicker.pick(
                        Collections.singletonList(new Endpoint("", 1, 1.0)), () -> 0.0));
        assertThrows(
                IllegalStateException.class,
                () -> WeightedEndpointPicker.pick(
                        Collections.singletonList(new Endpoint("host", 0, 1.0)), () -> 0.0));
        assertThrows(
                IllegalStateException.class,
                () -> WeightedEndpointPicker.pick(
                        Collections.singletonList(new Endpoint("host", 1, Double.NaN)),
                        () -> 0.0));
        assertThrows(
                IllegalStateException.class,
                () -> WeightedEndpointPicker.pick(
                        Collections.singletonList(new Endpoint("host", 1, 0.0)), () -> 0.0));
    }
    @Test
    public void baselineSelectionNeverUsesLaneNodes() {
        java.util.List<Endpoint> nodes = Arrays.asList(
                new Endpoint("prod", 80, 1), new Endpoint("prt", 80, 100, "prt"),
                new Endpoint("dev", 80, 100, "dev_a"));
        assertEquals("prod", WeightedEndpointPicker.pickBaseline(nodes, () -> 0.99).host);
        assertThrows(IllegalStateException.class,
                () -> WeightedEndpointPicker.pickBaseline(nodes.subList(1, 3), () -> 0));
    }
}
