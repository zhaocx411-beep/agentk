package com.kuaishou.klingai.agentk.apiref

import org.jetbrains.dokka.links.Callable
import org.jetbrains.dokka.links.DRI
import org.jetbrains.dokka.links.JavaClassReference
import org.jetbrains.dokka.pages.ContentDRILink
import org.jetbrains.dokka.pages.ContentGroup
import org.jetbrains.dokka.pages.ContentKind
import org.jetbrains.dokka.pages.ContentText
import org.jetbrains.dokka.pages.DCI
import org.junit.Assert.assertEquals
import org.junit.Test

class StructuredTextTest {
    @Test
    fun `interface adapter corrects inheritance keyword and recalculates references`() {
        val client = DRI("example", "Client")
        val autoCloseable = DRI("java.lang", "AutoCloseable")
        val dci = DCI(setOf(client), ContentKind.Symbol)
        val symbols = SymbolIndex(ArtifactCoordinate("example", "example-api"), emptyList())
        symbols.register(client, symbols.typeId(client, listOf("Client")))
        val signature = ContentGroup(
            children = listOf(
                ContentText("public interface ", dci, emptySet()),
                ContentDRILink(
                    children = listOf(ContentText("Client", dci, emptySet())),
                    address = client,
                    dci = dci,
                    sourceSets = emptySet(),
                ),
                ContentText(" implements ", dci, emptySet()),
                ContentDRILink(
                    children = listOf(ContentText("AutoCloseable", dci, emptySet())),
                    address = autoCloseable,
                    dci = dci,
                    sourceSets = emptySet(),
                ),
            ),
            dci = dci,
            sourceSets = emptySet(),
            style = emptySet(),
        )

        val result = flattenInterfaceSignature(listOf(signature), symbols)

        assertEquals("public interface Client extends AutoCloseable", result.text)
        assertEquals(listOf(17 to 23, 32 to 45), result.references.map { it.start to it.end })
        assertEquals(listOf("Client", "AutoCloseable"), result.references.map { it.target.classNames.last() })
    }

    @Test
    fun `constructor adapter removes provider return type and recalculates references`() {
        val constructor = DRI(
            packageName = "example",
            classNames = "ClientOptions",
            callable = Callable("ClientOptions", null, emptyList()),
        )
        val dci = DCI(setOf(constructor), ContentKind.Symbol)
        val symbols = SymbolIndex(ArtifactCoordinate("example", "example-api"), emptyList())
        symbols.register(
            constructor,
            symbols.callableId(constructor, listOf("ClientOptions"), "ClientOptions"),
        )
        val signature = ContentGroup(
            children = listOf(
                ContentText("public ", dci, emptySet()),
                ContentText("void", dci, emptySet()),
                ContentText("\u00a0", dci, emptySet()),
                ContentDRILink(
                    children = listOf(ContentText("ClientOptions", dci, emptySet())),
                    address = constructor,
                    dci = dci,
                    sourceSets = emptySet(),
                ),
                ContentText("(", dci, emptySet()),
                ContentText(")", dci, emptySet()),
            ),
            dci = dci,
            sourceSets = emptySet(),
            style = emptySet(),
        )

        val result = flattenConstructorSignature(listOf(signature), constructor, symbols)

        assertEquals("public ClientOptions()", result.text)
        assertEquals(listOf(7 to 20), result.references.map { it.start to it.end })
        assertEquals(listOf("ClientOptions"), result.references.map { it.target.classNames.last() })
    }
}
