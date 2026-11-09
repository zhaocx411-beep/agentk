package com.kuaishou.klingai.agentk.client.grpc;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

import java.nio.charset.StandardCharsets;
import java.util.Base64;
import java.util.HashMap;
import java.util.Map;
import java.util.Objects;

import io.grpc.Metadata;
import org.junit.Test;

public class KessIdentityTest {
    private static final Metadata.Key<byte[]> KWS_BINARY = Metadata.Key.of(
            "kws-bin", Metadata.BINARY_BYTE_MARSHALLER);
    private static final Metadata.Key<String> KWS_CREDENTIAL = Metadata.Key.of(
            "kwsCredential", Metadata.ASCII_STRING_MARSHALLER);

    @Test
    public void environmentCombinationsPreserveValues() {
        assertIdentity(new HashMap<>(), null, null);

        Map<String, String> service = new HashMap<>();
        service.put("KWS_SERVICE_NAME", "agentk.sdk.fixture/原样");
        assertIdentity(service, fixtureBytes(), null);

        Map<String, String> credential = new HashMap<>();
        credential.put("KWS_SERVICE_CREDENTIAL", " Credential-RAW ");
        assertIdentity(credential, null, " Credential-RAW ");

        service.put("KWS_SERVICE_CREDENTIAL", "credential");
        assertIdentity(service, fixtureBytes(), "credential");
    }

    @Test
    public void longServiceNameUsesMultibyteVarint() {
        Map<String, String> values = new HashMap<>();
        values.put("KWS_SERVICE_NAME", "x".repeat(128));
        Metadata metadata = capture(KessIdentity.from(values::get));
        byte[] raw = Objects.requireNonNull(metadata.get(KWS_BINARY));

        assertArrayEquals(new byte[]{0x0a, (byte) 0x80, 0x01}, slice(raw, 0, 3));
        assertArrayEquals("x".repeat(128).getBytes(StandardCharsets.UTF_8), slice(raw, 3, raw.length));
    }

    private static void assertIdentity(
            Map<String, String> values, byte[] binary, String credential) {
        Metadata metadata = capture(KessIdentity.from(values::get));
        if (binary == null) {
            assertNull(metadata.get(KWS_BINARY));
        } else {
            assertArrayEquals(binary, metadata.get(KWS_BINARY));
        }
        assertEquals(credential, metadata.get(KWS_CREDENTIAL));
    }

    private static byte[] fixtureBytes() {
        return Base64.getDecoder().decode("ChlhZ2VudGsuc2RrLmZpeHR1cmUv5Y6f5qC3");
    }

    private static Metadata capture(KessIdentity identity) {
        Metadata metadata = new Metadata();
        identity.apply(metadata);
        return metadata;
    }

    private static byte[] slice(byte[] value, int from, int to) {
        byte[] result = new byte[to - from];
        System.arraycopy(value, from, result, 0, result.length);
        return result;
    }
}
