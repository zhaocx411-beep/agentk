package com.kuaishou.klingai.agentk.client.grpc

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class KessEndpointsTest {
    @Test
    fun `prefers grpc port and falls back to http`() {
        assertEquals(9001, selectPort(mapOf("grpc" to 9001, "http" to 8001)))
        assertEquals(8001, selectPort(mapOf("http" to 8001)))
        assertEquals(0, selectPort(emptyMap()))
    }

    @Test
    fun `weighted picker is deterministic`() {
        val endpoints = listOf(
            Endpoint("one", 1, 1.0),
            Endpoint("two", 2, 3.0),
        )

        assertEquals("one", WeightedEndpointPicker.pick(endpoints) { 0.0 }.host)
        assertEquals("one", WeightedEndpointPicker.pick(endpoints) { 0.24 }.host)
        assertEquals("two", WeightedEndpointPicker.pick(endpoints) { 0.25 }.host)
        assertEquals("two", WeightedEndpointPicker.pick(endpoints) { 0.99 }.host)
    }

    @Test
    fun `weighted picker rejects unusable route tables`() {
        assertThrows(IllegalStateException::class.java) {
            WeightedEndpointPicker.pick(emptyList()) { 0.0 }
        }
        assertThrows(IllegalStateException::class.java) {
            WeightedEndpointPicker.pick(listOf(Endpoint("", 1, 1.0))) { 0.0 }
        }
        assertThrows(IllegalStateException::class.java) {
            WeightedEndpointPicker.pick(listOf(Endpoint("host", 0, 1.0))) { 0.0 }
        }
        assertThrows(IllegalStateException::class.java) {
            WeightedEndpointPicker.pick(listOf(Endpoint("host", 1, Double.NaN))) { 0.0 }
        }
        assertThrows(IllegalStateException::class.java) {
            WeightedEndpointPicker.pick(listOf(Endpoint("host", 1, 0.0))) { 0.0 }
        }
    }

    @Test
    fun `baseline selection never uses lane nodes`() {
        val nodes = listOf(Endpoint("prod", 80, 1.0), Endpoint("prt", 80, 100.0, "prt"), Endpoint("dev", 80, 100.0, "dev_a"))
        assertEquals("prod", WeightedEndpointPicker.pickBaseline(nodes) { 0.99 }.host)
        assertThrows(IllegalStateException::class.java) {
            WeightedEndpointPicker.pickBaseline(nodes.drop(1)) { 0.0 }
        }
    }
}
