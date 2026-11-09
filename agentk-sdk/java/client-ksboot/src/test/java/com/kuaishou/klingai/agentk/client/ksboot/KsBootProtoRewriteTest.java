package com.kuaishou.klingai.agentk.client.ksboot;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertTrue;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.stream.Stream;

import org.junit.Test;

public class KsBootProtoRewriteTest {
    @Test
    public void generatedViewContainsOnlyTheTwoAllowedChanges() throws IOException {
        Path canonicalRoot = Path.of("../..", "protocol").toAbsolutePath().normalize();
        Path ksbootRoot = Path.of("target", "ksboot-proto").toAbsolutePath().normalize();
        try (Stream<Path> paths = Files.walk(canonicalRoot)) {
            paths.filter(path -> path.getFileName().toString().endsWith(".proto"))
                    .forEach(path -> assertExpectedRewrite(canonicalRoot, ksbootRoot, path));
        }
    }

    @Test
    public void unknownEditionsFeatureFailsClosed() throws Exception {
        Path root = Files.createTempDirectory(Path.of("target"), "rewrite-negative-");
        Path source = Files.createDirectories(root.resolve("source"));
        Path target = root.resolve("target");
        Files.writeString(
                source.resolve("unexpected.proto"),
                "edition = \"2023\";\n"
                        + "package test;\n"
                        + "option features.field_presence = IMPLICIT;\n"
                        + "option features.enum_type = CLOSED;\n",
                StandardCharsets.UTF_8);

        Process process = new ProcessBuilder(
                        Path.of(System.getProperty("java.home"), "bin", "java").toString(),
                        Path.of("..", "tools", "RewriteKsBootProto.java")
                                .toAbsolutePath()
                                .normalize()
                                .toString(),
                        source.toString(),
                        target.toString())
                .redirectErrorStream(true)
                .start();
        String output = new String(process.getInputStream().readAllBytes(), StandardCharsets.UTF_8);

        assertNotEquals(output, 0, process.waitFor());
        assertTrue(output, output.contains("supported field_presence feature"));
        assertFalse(Files.exists(target.resolve("unexpected.proto")));
    }

    private static void assertExpectedRewrite(
            Path canonicalRoot, Path ksbootRoot, Path canonical) {
        try {
            String original = Files.readString(canonical, StandardCharsets.UTF_8);
            String expected = original
                    .replace("edition = \"2023\";", "syntax = \"proto3\";")
                    .replace("option features.field_presence = IMPLICIT;\n", "");
            Path relative = canonicalRoot.relativize(canonical);
            String actual = Files.readString(ksbootRoot.resolve(relative), StandardCharsets.UTF_8);
            assertEquals(relative.toString(), expected, actual);
        } catch (IOException error) {
            throw new AssertionError(error);
        }
    }
}
