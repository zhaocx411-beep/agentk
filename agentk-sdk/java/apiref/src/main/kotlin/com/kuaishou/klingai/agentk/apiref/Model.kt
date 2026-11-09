package com.kuaishou.klingai.agentk.apiref

internal data class Reference(
    val artifacts: List<ArtifactDoc>,
)

internal data class ArtifactCoordinate(
    val groupId: String,
    val artifactId: String,
)

internal data class ArtifactDoc(
    val groupId: String,
    val artifactId: String,
    val packages: List<PackageDoc>,
)

internal data class PackageDoc(
    val name: String,
    val documentation: Documentation,
    val types: List<TypeDoc>,
)

internal enum class TypeKind {
    CLASS,
    INTERFACE,
    ENUM,
    ANNOTATION,
}

internal data class TypeDoc(
    val id: SymbolId,
    val kind: TypeKind,
    val name: String,
    val modifiers: List<String>,
    val annotations: List<AnnotationDoc>,
    val typeParameters: List<TypeParameterDoc>,
    val superClass: TypeRef?,
    val interfaces: List<TypeRef>,
    val declaration: AnnotatedText,
    val documentation: Documentation,
    val nestedTypes: List<TypeDoc>,
    val enumConstants: List<FieldDoc>,
    val fields: List<FieldDoc>,
    val constructors: List<ExecutableDoc>,
    val methods: List<ExecutableDoc>,
    val inheritedMembers: List<InheritedMemberGroup>,
)

internal data class FieldDoc(
    val id: SymbolId,
    val name: String,
    val modifiers: List<String>,
    val annotations: List<AnnotationDoc>,
    val type: TypeRef?,
    val defaultValue: AnnotationValueDoc?,
    val declaration: AnnotatedText,
    val documentation: Documentation,
)

internal data class ExecutableDoc(
    val id: SymbolId,
    val name: String,
    val modifiers: List<String>,
    val annotations: List<AnnotationDoc>,
    val typeParameters: List<TypeParameterDoc>,
    val parameters: List<ParameterDoc>,
    val returnType: TypeRef?,
    val throwsTypes: List<TypeRef>,
    val defaultValue: AnnotationValueDoc?,
    val declaration: AnnotatedText,
    val documentation: Documentation,
)

internal data class ParameterDoc(
    val name: String,
    val annotations: List<AnnotationDoc>,
    val type: TypeRef,
    val defaultValue: AnnotationValueDoc?,
)

internal data class TypeParameterDoc(
    val name: String,
    val annotations: List<AnnotationDoc>,
    val bounds: List<TypeRef>,
    val documentation: Documentation,
)

internal data class TypeRef(
    val declaration: AnnotatedText,
)

internal data class InheritedMemberGroup(
    val declaringType: SymbolId,
    val fields: List<SymbolId>,
    val methods: List<SymbolId>,
)

internal data class SymbolId(
    val artifact: ArtifactCoordinate?,
    val packageName: String,
    val classNames: List<String>,
    val callable: CallableId?,
)

internal data class CallableId(
    val name: String,
    val parameterTypes: List<String>,
)

internal data class AnnotatedText(
    val text: String,
    val references: List<TextReference>,
)

internal data class TextReference(
    val start: Int,
    val end: Int,
    val target: SymbolId,
)

internal data class Documentation(
    val children: List<DocNode>,
)

internal data class DocNode(
    val kind: String,
    val text: String?,
    val name: String?,
    val target: SymbolId?,
    val attributes: Map<String, String>,
    val children: List<DocNode>,
)

internal data class AnnotationDoc(
    val type: SymbolId,
    val arguments: List<AnnotationArgumentDoc>,
)

internal data class AnnotationArgumentDoc(
    val name: String,
    val value: AnnotationValueDoc,
)

internal data class AnnotationValueDoc(
    val kind: String,
    val text: String?,
    val target: SymbolId?,
    val annotation: AnnotationDoc?,
    val values: List<AnnotationValueDoc>,
)
