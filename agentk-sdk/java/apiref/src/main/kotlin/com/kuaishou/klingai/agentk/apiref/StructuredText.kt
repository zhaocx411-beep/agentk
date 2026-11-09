package com.kuaishou.klingai.agentk.apiref

import org.jetbrains.dokka.model.Bound
import org.jetbrains.dokka.model.Contravariance
import org.jetbrains.dokka.model.Covariance
import org.jetbrains.dokka.model.DefinitelyNonNullable
import org.jetbrains.dokka.model.Dynamic
import org.jetbrains.dokka.model.GenericTypeConstructor
import org.jetbrains.dokka.model.FunctionalTypeConstructor
import org.jetbrains.dokka.model.Invariance
import org.jetbrains.dokka.model.JavaObject
import org.jetbrains.dokka.model.Nullable
import org.jetbrains.dokka.model.PrimitiveJavaType
import org.jetbrains.dokka.model.Projection
import org.jetbrains.dokka.model.Star
import org.jetbrains.dokka.model.TypeAliased
import org.jetbrains.dokka.model.TypeConstructor
import org.jetbrains.dokka.model.TypeParameter
import org.jetbrains.dokka.model.UnresolvedBound
import org.jetbrains.dokka.model.Void
import org.jetbrains.dokka.pages.ContentBreakLine
import org.jetbrains.dokka.pages.ContentDRILink
import org.jetbrains.dokka.pages.ContentNode
import org.jetbrains.dokka.pages.ContentText
import org.jetbrains.dokka.links.DRI

internal fun flattenSignature(nodes: List<ContentNode>, symbols: SymbolIndex): AnnotatedText {
    val builder = AnnotatedTextBuilder(symbols)
    nodes.forEach(builder::append)
    return builder.build()
}

internal fun flattenInterfaceSignature(nodes: List<ContentNode>, symbols: SymbolIndex): AnnotatedText {
    val builder = AnnotatedTextBuilder(symbols, fixInterfaceInheritance = true)
    nodes.forEach(builder::append)
    return builder.build()
}

internal fun flattenConstructorSignature(
    nodes: List<ContentNode>,
    constructor: DRI,
    symbols: SymbolIndex,
): AnnotatedText {
    val builder = AnnotatedTextBuilder(symbols)
    nodes.forEach { node ->
        val callableIndex = node.children.indexOfFirst {
            it is ContentDRILink && it.address == constructor
        }
        if (callableIndex < 0) {
            builder.append(node)
        } else {
            val separatorIndex = node.children.subList(0, callableIndex).indexOfLast(::isNonEmptyWhitespace)
            require(separatorIndex > 0) { "Dokka constructor signature has no return-type separator: $constructor" }
            node.children.forEachIndexed { index, child ->
                if (index != separatorIndex - 1 && index != separatorIndex) builder.append(child)
            }
        }
    }
    return builder.build()
}

private fun isNonEmptyWhitespace(node: ContentNode): Boolean =
    node is ContentText && node.text.isNotEmpty() && node.text.all(Char::isWhitespace)

internal fun typeRef(bound: Bound, symbols: SymbolIndex): TypeRef {
    val builder = TypeTextBuilder(symbols)
    builder.append(bound)
    return TypeRef(builder.build())
}

private class AnnotatedTextBuilder(
    private val symbols: SymbolIndex,
    private val fixInterfaceInheritance: Boolean = false,
) {
    private val text = StringBuilder()
    private val references = mutableListOf<TextReference>()

    fun append(node: ContentNode) {
        when (node) {
            is ContentText -> {
                val normalized = node.text.replace('\u00a0', ' ')
                text.append(if (fixInterfaceInheritance) normalized.replace(INTERFACE_IMPLEMENTS, "extends") else normalized)
            }
            is ContentBreakLine -> text.append('\n')
            is ContentDRILink -> {
                val start = text.length
                node.children.forEach(::append)
                val end = text.length
                if (end > start) references += TextReference(start, end, symbols.resolve(node.address))
            }
            else -> node.children.forEach(::append)
        }
    }

    fun build(): AnnotatedText = AnnotatedText(
        text.toString(),
        references.sortedWith(compareBy(TextReference::start, TextReference::end)),
    )

    companion object {
        private val INTERFACE_IMPLEMENTS = Regex("\\bimplements\\b")
    }
}

private class TypeTextBuilder(private val symbols: SymbolIndex) {
    private val text = StringBuilder()
    private val references = mutableListOf<TextReference>()

    fun append(projection: Projection) {
        when (projection) {
            is GenericTypeConstructor -> appendConstructor(projection)
            is FunctionalTypeConstructor -> appendConstructor(projection)
            is TypeParameter -> appendLink(projection.presentableName ?: projection.name, symbols.resolve(projection.dri))
            is PrimitiveJavaType -> text.append(projection.name)
            is JavaObject -> text.append("Object")
            is UnresolvedBound -> text.append(projection.name)
            is Nullable -> append(projection.inner)
            is DefinitelyNonNullable -> append(projection.inner)
            is TypeAliased -> append(projection.inner)
            is Covariance<*> -> text.append("? extends ").also { append(projection.inner) }
            is Contravariance<*> -> text.append("? super ").also { append(projection.inner) }
            is Invariance<*> -> append(projection.inner)
            Star -> text.append('?')
            Void -> text.append("void")
            Dynamic -> text.append("Object")
        }
    }

    private fun appendConstructor(type: TypeConstructor) {
        val label = type.presentableName ?: type.dri.classNames.orEmpty().substringAfterLast('.')
        appendLink(label, symbols.resolve(type.dri))
        if (type.projections.isNotEmpty()) {
            text.append('<')
            type.projections.forEachIndexed { index, projection ->
                if (index > 0) text.append(", ")
                append(projection)
            }
            text.append('>')
        }
    }

    private fun appendLink(label: String, target: SymbolId) {
        val start = text.length
        text.append(label)
        references += TextReference(start, text.length, target)
    }

    fun build(): AnnotatedText = AnnotatedText(text.toString(), references)
}
