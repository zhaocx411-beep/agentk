package com.kuaishou.klingai.agentk.apiref

import java.nio.file.Files
import java.util.jar.JarEntry
import java.util.jar.JarOutputStream
import org.jetbrains.dokka.links.Callable
import org.jetbrains.dokka.links.DRI
import org.jetbrains.dokka.links.JavaClassReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class SymbolIndexTest {
    @Test
    fun `resolves classpath artifact and keeps jdk artifact null`() {
        val jar = Files.createTempFile("apiref-owner", ".jar")
        JarOutputStream(Files.newOutputStream(jar)).use { output ->
            output.putNextEntry(JarEntry("META-INF/maven/com.example/example-api/pom.properties"))
            output.write("groupId=com.example\nartifactId=example-api\nversion=1\n".toByteArray())
            output.closeEntry()
            output.putNextEntry(JarEntry("com/example/api/Outer\$Nested.class"))
            output.closeEntry()
        }

        try {
            val index = SymbolIndex(ArtifactCoordinate("current", "current-api"), listOf(jar.toFile()))
            val external = index.resolve(DRI("com.example.api", "Outer.Nested"))
            val jdk = index.resolve(DRI("java.lang", "String"))

            assertEquals(ArtifactCoordinate("com.example", "example-api"), external.artifact)
            assertEquals(listOf("Outer", "Nested"), external.classNames)
            assertNull(jdk.artifact)
        } finally {
            Files.deleteIfExists(jar)
        }
    }

    @Test
    fun `normalizes companion callable to java view owner`() {
        val index = SymbolIndex(ArtifactCoordinate("g", "a"), emptyList())
        val raw = DRI(
            packageName = "example",
            classNames = "AgentGrpcClient.Companion",
            callable = Callable("create", null, listOf(JavaClassReference("example.Options"))),
        )
        val normalized = index.callableId(raw, listOf("AgentGrpcClient"), "create")
        index.register(raw, normalized)

        val resolved = index.resolve(raw)

        assertEquals(listOf("AgentGrpcClient"), resolved.classNames)
        assertEquals(listOf("example.Options"), resolved.callable?.parameterTypes)
    }
}
