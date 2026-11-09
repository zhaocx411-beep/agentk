package com.kuaishou.agent.operation.backend.common.web;

import static org.assertj.core.api.Assertions.assertThat;

import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;

import org.junit.jupiter.api.Test;

class ContentDispositionsTest {

    @Test
    void shouldPercentEncodeChineseSoHeaderStaysAscii() throws Exception {
        String original = "测试1-ceshi1-2026-08-20T18.yaml";
        String header = ContentDispositions.attachment(original, "dataset.bin");

        assertThat(header).isEqualTo("attachment; filename=\"%E6%B5%8B%E8%AF%951-ceshi1-2026-08-20T18.yaml\";"
                + " filename*=UTF-8''%E6%B5%8B%E8%AF%951-ceshi1-2026-08-20T18.yaml");
        assertThat(header.chars().allMatch(c -> c < 128)).isTrue();

        String encoded = ContentDispositions.fileNameHeader(original, "dataset.bin");
        assertThat(URLDecoder.decode(encoded, StandardCharsets.UTF_8.name())).isEqualTo(original);
    }

    @Test
    void shouldEncodeSpaceAsPercent20() {
        String header = ContentDispositions.attachment("my dataset.yaml", "dataset.bin");

        assertThat(header).contains("my%20dataset.yaml");
        assertThat(header).doesNotContain("+");
    }

    @Test
    void shouldKeepPlainAsciiFilenameReadable() {
        String header = ContentDispositions.attachment("demo-v1.yaml", "dataset.bin");

        assertThat(header).isEqualTo("attachment; filename=\"demo-v1.yaml\"; filename*=UTF-8''demo-v1.yaml");
    }

    @Test
    void shouldFallbackWhenFilenameBlank() {
        String header = ContentDispositions.attachment("  ", "dataset.bin");

        assertThat(header).contains("filename=\"dataset.bin\"");
        assertThat(ContentDispositions.fileNameHeader(null, "dataset.bin")).isEqualTo("dataset.bin");
    }
}
