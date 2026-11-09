package com.kuaishou.klingai.agentk.client.internal

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test
import java.util.Base64

class KessIdentityTest {
    @Test
    fun `environment combinations preserve values`() {
        assertEquals(emptyMap<String, List<String>>(), KessIdentity.headers { null })
        assertEquals(
            mapOf("kws-bin" to listOf(TEST_KWS_BINARY)),
            KessIdentity.headers { key ->
                if (key == "KWS_SERVICE_NAME") "agentk.sdk.fixture/原样" else null
            },
        )
        assertEquals(
            mapOf("kwsCredential" to listOf(" credential 原样 ")),
            KessIdentity.headers { key ->
                if (key == "KWS_SERVICE_CREDENTIAL") " credential 原样 " else null
            },
        )
        assertEquals(
            testIdentityHeaders,
            KessIdentity.headers { key ->
                when (key) {
                    "KWS_SERVICE_NAME" -> "agentk.sdk.fixture/原样"
                    "KWS_SERVICE_CREDENTIAL" -> TEST_CREDENTIAL
                    else -> null
                }
            },
        )
    }

    @Test
    fun `cross language fixture is stable`() {
        val encoded = KessIdentity.headers { key ->
            if (key == "KWS_SERVICE_NAME") "agentk.sdk.fixture/原样" else null
        }.getValue("kws-bin").single()

        assertEquals(TEST_KWS_BINARY, encoded)
        assertEquals(
            "0a196167656e746b2e73646b2e666978747572652fe58e9fe6a0b7",
            Base64.getDecoder().decode(encoded).toHex(),
        )
    }

    @Test
    fun `long service name uses protobuf varint length`() {
        val encoded = KessIdentity.headers { key ->
            if (key == "KWS_SERVICE_NAME") "x".repeat(128) else null
        }.getValue("kws-bin").single()

        val raw = Base64.getDecoder().decode(encoded)
        assertArrayEquals(byteArrayOf(0x0a, 0x80.toByte(), 0x01), raw.copyOfRange(0, 3))
        assertArrayEquals("x".repeat(128).toByteArray(), raw.copyOfRange(3, raw.size))
    }

    private fun ByteArray.toHex(): String = joinToString("") { "%02x".format(it) }
}
