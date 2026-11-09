package com.kuaishou.klingai.agentk.apiref

import org.jetbrains.dokka.model.Documentable
import org.jetbrains.dokka.model.doc.CustomDocTag
import org.jetbrains.dokka.model.doc.DocTag
import org.jetbrains.dokka.model.doc.DocumentationLink
import org.jetbrains.dokka.model.doc.DocumentationNode
import org.jetbrains.dokka.model.doc.NamedTagWrapper
import org.jetbrains.dokka.model.doc.Param
import org.jetbrains.dokka.model.doc.See
import org.jetbrains.dokka.model.doc.TagWrapper
import org.jetbrains.dokka.model.doc.Text
import org.jetbrains.dokka.model.doc.Throws

internal class DocumentationMapper(private val symbols: SymbolIndex) {
    fun documentation(value: Documentable): Documentation {
        val node = value.documentation.entries
            .sortedBy { it.key.sourceSetID.toString() }
            .firstOrNull()
            ?.value
        return documentation(node)
    }

    fun documentation(value: DocumentationNode?): Documentation = Documentation(
        value?.children.orEmpty().map(::wrapper),
    )

    private fun wrapper(value: TagWrapper): DocNode {
        val target = when (value) {
            is See -> value.address
            is Param -> value.address
            is Throws -> value.exceptionAddress
            else -> null
        }
        return DocNode(
            kind = value.javaClass.simpleName,
            text = null,
            name = (value as? NamedTagWrapper)?.name,
            target = target?.let(symbols::resolve),
            attributes = emptyMap(),
            children = listOf(tag(value.root)),
        )
    }

    private fun tag(value: DocTag): DocNode = DocNode(
        kind = when (value) {
            is CustomDocTag -> value.name
            else -> value.javaClass.simpleName
        },
        text = (value as? Text)?.body,
        name = (value as? CustomDocTag)?.name,
        target = (value as? DocumentationLink)?.dri?.let(symbols::resolve),
        attributes = value.params.toSortedMap(),
        children = value.children.map(::tag),
    )
}
