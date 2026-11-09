package com.kuaishou.klingai.agentk.apiref

import org.jetbrains.dokka.base.signatures.SignatureProvider
import org.jetbrains.dokka.links.DRI
import org.jetbrains.dokka.model.AdditionalModifiers
import org.jetbrains.dokka.model.AnnotationParameterValue
import org.jetbrains.dokka.model.AnnotationValue
import org.jetbrains.dokka.model.Annotations
import org.jetbrains.dokka.model.ArrayValue
import org.jetbrains.dokka.model.BooleanConstant
import org.jetbrains.dokka.model.CheckedExceptions
import org.jetbrains.dokka.model.ClassValue
import org.jetbrains.dokka.model.ComplexExpression
import org.jetbrains.dokka.model.DAnnotation
import org.jetbrains.dokka.model.DClass
import org.jetbrains.dokka.model.DClasslike
import org.jetbrains.dokka.model.DEnum
import org.jetbrains.dokka.model.DEnumEntry
import org.jetbrains.dokka.model.DFunction
import org.jetbrains.dokka.model.DInterface
import org.jetbrains.dokka.model.DModule
import org.jetbrains.dokka.model.DObject
import org.jetbrains.dokka.model.DParameter
import org.jetbrains.dokka.model.DProperty
import org.jetbrains.dokka.model.DTypeParameter
import org.jetbrains.dokka.model.DefaultValue
import org.jetbrains.dokka.model.DoubleConstant
import org.jetbrains.dokka.model.EnumValue
import org.jetbrains.dokka.model.Expression
import org.jetbrains.dokka.model.FloatConstant
import org.jetbrains.dokka.model.InheritedMember
import org.jetbrains.dokka.model.IntegerConstant
import org.jetbrains.dokka.model.JavaClassKindTypes
import org.jetbrains.dokka.model.JavaVisibility
import org.jetbrains.dokka.model.KotlinClassKindTypes
import org.jetbrains.dokka.model.KotlinVisibility
import org.jetbrains.dokka.model.LiteralValue
import org.jetbrains.dokka.model.StringConstant
import org.jetbrains.dokka.model.TypeConstructorWithKind
import org.jetbrains.dokka.model.WithAbstraction
import org.jetbrains.dokka.model.WithConstructors
import org.jetbrains.dokka.model.WithGenerics
import org.jetbrains.dokka.model.WithSupertypes
import org.jetbrains.dokka.model.WithVisibility

internal class ApiReferenceMapper(
    private val coordinate: ArtifactCoordinate,
    private val symbols: SymbolIndex,
    private val signatures: SignatureProvider,
) {
    private val docs = DocumentationMapper(symbols)

    fun map(module: DModule): ArtifactDoc {
        module.packages.forEach { pkg ->
            pkg.classlikes.filter(::isVisible).forEach { registerType(it, emptyList()) }
        }

        return ArtifactDoc(
            groupId = coordinate.groupId,
            artifactId = coordinate.artifactId,
            packages = module.packages
                .map { pkg ->
                    PackageDoc(
                        name = pkg.packageName,
                        documentation = docs.documentation(pkg),
                        types = pkg.classlikes.filter(::isVisible).map { mirrorType(it, emptyList()) }.sortedBy { it.name },
                    )
                }
                .filter { it.types.isNotEmpty() || it.documentation.children.isNotEmpty() }
                .sortedBy { it.name },
        )
    }

    private fun registerType(type: DClasslike, parentPath: List<String>) {
        val ownerPath = parentPath + type.name.orEmpty()
        symbols.register(type.dri, symbols.typeId(type.dri, ownerPath))
        type.properties.filter(::isVisible).forEach { property ->
            symbols.register(property.dri, propertyId(property, ownerPath))
        }
        functions(type).filter(::isVisible).forEach { function ->
            symbols.register(function.dri, symbols.callableId(function.dri, ownerPath, callableName(type, function)))
        }
        constructors(type).filter(::isVisible).forEach { function ->
            symbols.register(function.dri, symbols.callableId(function.dri, ownerPath, type.name.orEmpty()))
        }
        if (type is DEnum) {
            type.entries.forEach { entry -> symbols.register(entry.dri, enumEntryId(entry, ownerPath)) }
        }
        type.classlikes.filter(::isVisible).forEach { registerType(it, ownerPath) }
    }

    private fun mirrorType(type: DClasslike, parentPath: List<String>): TypeDoc {
        val ownerPath = parentPath + type.name.orEmpty()
        val declaredFields = type.properties.filter { isVisible(it) && inheritedFrom(it) == null }
        val declaredMethods = functions(type).filter { isVisible(it) && inheritedFrom(it) == null }
        val inheritedProperties = type.properties.filter { isVisible(it) && inheritedFrom(it) != null }
        val inheritedFunctions = functions(type).filter { isVisible(it) && inheritedFrom(it) != null }
        val supertypes = (type as? WithSupertypes)?.supertypes?.values?.flatten().orEmpty().distinctBy { it.typeConstructor.dri }
        val (superClasses, interfaces) = supertypes.partition(::isClass)

        return TypeDoc(
            id = symbols.typeId(type.dri, ownerPath),
            kind = typeKind(type),
            name = type.name.orEmpty(),
            modifiers = modifiers(type),
            annotations = annotations(type),
            typeParameters = generics(type).map(::typeParameter),
            superClass = superClasses.firstOrNull()?.typeConstructor?.let { typeRef(it, symbols) },
            interfaces = interfaces.map { typeRef(it.typeConstructor, symbols) }.sortedBy { it.declaration.text },
            declaration = declaration(type),
            documentation = docs.documentation(type),
            nestedTypes = type.classlikes.filter(::isVisible).map { mirrorType(it, ownerPath) }.sortedBy { it.name },
            enumConstants = (type as? DEnum)?.entries.orEmpty().map { enumEntry(it, ownerPath) }.sortedBy { it.name },
            fields = declaredFields.map { field(it, ownerPath) }.sortedBy { it.name },
            constructors = constructors(type).filter(::isVisible).map { executable(type, it, ownerPath) }.sortedWith(executableOrder),
            methods = declaredMethods.map { executable(type, it, ownerPath) }.sortedWith(executableOrder),
            inheritedMembers = inheritedGroups(inheritedProperties, inheritedFunctions),
        )
    }

    private fun field(value: DProperty, ownerPath: List<String>): FieldDoc = FieldDoc(
        id = propertyId(value, ownerPath),
        name = value.name,
        modifiers = modifiers(value),
        annotations = annotations(value),
        type = typeRef(value.type, symbols),
        defaultValue = defaultValue(value),
        declaration = declaration(value),
        documentation = docs.documentation(value),
    )

    private fun enumEntry(value: DEnumEntry, ownerPath: List<String>): FieldDoc = FieldDoc(
        id = enumEntryId(value, ownerPath),
        name = value.name,
        modifiers = listOf("public", "static", "final"),
        annotations = annotations(value),
        type = null,
        defaultValue = null,
        declaration = declaration(value),
        documentation = docs.documentation(value),
    )

    private fun executable(owner: DClasslike, value: DFunction, ownerPath: List<String>): ExecutableDoc = ExecutableDoc(
        id = symbols.callableId(value.dri, ownerPath, callableName(owner, value)),
        name = callableName(owner, value),
        modifiers = modifiers(value),
        annotations = annotations(value),
        typeParameters = value.generics.map(::typeParameter),
        parameters = value.parameters.map(::parameter),
        returnType = if (value.isConstructor) null else typeRef(value.type, symbols),
        throwsTypes = checkedExceptions(value).map(::typeRef),
        defaultValue = defaultValue(value),
        declaration = declaration(value),
        documentation = docs.documentation(value),
    )

    private fun parameter(value: DParameter): ParameterDoc = ParameterDoc(
        name = value.name.orEmpty(),
        annotations = annotations(value),
        type = typeRef(value.type, symbols),
        defaultValue = defaultValue(value),
    )

    private fun typeParameter(value: DTypeParameter): TypeParameterDoc = TypeParameterDoc(
        name = value.name.substringAfterLast('.'),
        annotations = annotations(value),
        bounds = value.bounds.map { typeRef(it, symbols) },
        documentation = docs.documentation(value),
    )

    private fun inheritedGroups(
        properties: List<DProperty>,
        functions: List<DFunction>,
    ): List<InheritedMemberGroup> {
        val propertyGroups = properties.groupBy { inheritedFrom(it)!! }
        val functionGroups = functions.groupBy { inheritedFrom(it)!! }
        return (propertyGroups.keys + functionGroups.keys).distinct().map { declaring ->
            InheritedMemberGroup(
                declaringType = symbols.resolve(declaring).copy(callable = null),
                fields = propertyGroups[declaring].orEmpty().map { symbols.resolve(it.dri) }.sortedBy(::symbolOrder),
                methods = functionGroups[declaring].orEmpty().map { symbols.resolve(it.dri) }.sortedBy(::symbolOrder),
            )
        }.sortedBy { symbolOrder(it.declaringType) }
    }

    private fun declaration(value: org.jetbrains.dokka.model.Documentable): AnnotatedText {
        val signature = signatures.signature(value)
        return if (value is DFunction && value.isConstructor) {
            flattenConstructorSignature(signature, value.dri, symbols)
        } else if (value is DInterface) {
            flattenInterfaceSignature(signature, symbols)
        } else {
            flattenSignature(signature, symbols)
        }
    }

    private fun checkedExceptions(value: DFunction): List<DRI> =
        value.extra[CheckedExceptions]?.exceptions?.values?.flatten().orEmpty().distinct()

    private fun typeRef(dri: DRI): TypeRef {
        val label = dri.classNames.orEmpty().substringAfterLast('.').ifEmpty { dri.packageName.orEmpty() }
        return TypeRef(AnnotatedText(label, listOf(TextReference(0, label.length, symbols.resolve(dri)))))
    }

    private fun propertyId(value: DProperty, ownerPath: List<String>): SymbolId = SymbolId(
        artifact = coordinate,
        packageName = value.dri.packageName.orEmpty(),
        classNames = ownerPath,
        callable = CallableId(value.name, emptyList()),
    )

    private fun enumEntryId(value: DEnumEntry, ownerPath: List<String>): SymbolId = SymbolId(
        artifact = coordinate,
        packageName = value.dri.packageName.orEmpty(),
        classNames = ownerPath,
        callable = CallableId(value.name, emptyList()),
    )

    private fun callableName(owner: DClasslike, value: DFunction): String =
        if (value.isConstructor) owner.name.orEmpty() else value.name

    private fun functions(value: DClasslike): List<DFunction> = value.functions

    private fun constructors(value: DClasslike): List<DFunction> =
        (value as? WithConstructors)?.constructors.orEmpty()

    private fun generics(value: DClasslike): List<DTypeParameter> =
        (value as? WithGenerics)?.generics.orEmpty()

    private fun isClass(value: TypeConstructorWithKind): Boolean =
        value.kind == JavaClassKindTypes.CLASS || value.kind == KotlinClassKindTypes.CLASS

    private fun typeKind(value: DClasslike): TypeKind = when (value) {
        is DInterface -> TypeKind.INTERFACE
        is DEnum -> TypeKind.ENUM
        is DAnnotation -> TypeKind.ANNOTATION
        is DClass, is DObject -> TypeKind.CLASS
    }

    private fun isVisible(value: WithVisibility): Boolean = value.visibility.values.any {
        it == JavaVisibility.Public || it == JavaVisibility.Protected ||
            it == KotlinVisibility.Public || it == KotlinVisibility.Protected
    }

    private fun inheritedFrom(value: DFunction): DRI? =
        value.extra[InheritedMember]?.inheritedFrom?.values?.filterNotNull()?.firstOrNull()

    private fun inheritedFrom(value: DProperty): DRI? =
        value.extra[InheritedMember]?.inheritedFrom?.values?.filterNotNull()?.firstOrNull()

    private fun modifiers(value: WithVisibility): List<String> {
        val result = mutableSetOf<String>()
        result += value.visibility.values.map { it.name }.filter(String::isNotBlank)
        if (value is WithAbstraction) result += value.modifier.values.map { it.name }.filter(String::isNotBlank)
        additionalModifiers(value)?.content?.values?.flatten()?.mapTo(result) { it.name }
        return result.sortedWith(compareBy({ modifierRank[it] ?: Int.MAX_VALUE }, { it }))
    }

    private fun additionalModifiers(value: WithVisibility): AdditionalModifiers? = when (value) {
        is DClass -> value.extra[AdditionalModifiers]
        is DInterface -> value.extra[AdditionalModifiers]
        is DEnum -> value.extra[AdditionalModifiers]
        is DAnnotation -> value.extra[AdditionalModifiers]
        is DObject -> value.extra[AdditionalModifiers]
        is DFunction -> value.extra[AdditionalModifiers]
        is DProperty -> value.extra[AdditionalModifiers]
        else -> null
    }

    private fun annotations(value: DClasslike): List<AnnotationDoc> = when (value) {
        is DClass -> annotations(value.extra[Annotations])
        is DInterface -> annotations(value.extra[Annotations])
        is DEnum -> annotations(value.extra[Annotations])
        is DAnnotation -> annotations(value.extra[Annotations])
        is DObject -> annotations(value.extra[Annotations])
    }

    private fun annotations(value: DFunction): List<AnnotationDoc> = annotations(value.extra[Annotations])
    private fun annotations(value: DProperty): List<AnnotationDoc> = annotations(value.extra[Annotations])
    private fun annotations(value: DParameter): List<AnnotationDoc> = annotations(value.extra[Annotations])
    private fun annotations(value: DTypeParameter): List<AnnotationDoc> = annotations(value.extra[Annotations])
    private fun annotations(value: DEnumEntry): List<AnnotationDoc> = annotations(value.extra[Annotations])

    private fun annotations(value: Annotations?): List<AnnotationDoc> = value?.directAnnotations?.values
        ?.flatten()
        ?.distinctBy { it.dri }
        ?.map(::annotation)
        ?.sortedBy { symbolOrder(it.type) }
        .orEmpty()

    private fun annotation(value: Annotations.Annotation): AnnotationDoc = AnnotationDoc(
        type = symbols.resolve(value.dri).copy(callable = null),
        arguments = value.params.entries.sortedBy { it.key }.map { (name, argument) ->
            AnnotationArgumentDoc(name, annotationValue(argument))
        },
    )

    private fun annotationValue(value: AnnotationParameterValue): AnnotationValueDoc = when (value) {
        is LiteralValue -> AnnotationValueDoc("literal", value.text(), null, null, emptyList())
        is ClassValue -> AnnotationValueDoc("class", value.className, symbols.resolve(value.classDRI), null, emptyList())
        is EnumValue -> AnnotationValueDoc("enum", value.enumName, symbols.resolve(value.enumDri), null, emptyList())
        is AnnotationValue -> AnnotationValueDoc("annotation", null, null, annotation(value.annotation), emptyList())
        is ArrayValue -> AnnotationValueDoc("array", null, null, null, value.value.map(::annotationValue))
    }

    private fun defaultValue(value: DFunction): AnnotationValueDoc? =
        value.extra[DefaultValue]?.expression?.values?.firstOrNull()?.let(::expressionValue)

    private fun defaultValue(value: DProperty): AnnotationValueDoc? =
        value.extra[DefaultValue]?.expression?.values?.firstOrNull()?.let(::expressionValue)

    private fun defaultValue(value: DParameter): AnnotationValueDoc? =
        value.extra[DefaultValue]?.expression?.values?.firstOrNull()?.let(::expressionValue)

    private fun expressionValue(value: Expression): AnnotationValueDoc = AnnotationValueDoc(
        kind = "literal",
        text = when (value) {
            is ComplexExpression -> value.value
            is IntegerConstant -> value.value.toString()
            is StringConstant -> value.value
            is DoubleConstant -> value.value.toString()
            is FloatConstant -> value.value.toString()
            is BooleanConstant -> value.value.toString()
            else -> value.toString()
        },
        target = null,
        annotation = null,
        values = emptyList(),
    )

    companion object {
        private val executableOrder = compareBy<ExecutableDoc>({ it.name }, { it.id.callable?.parameterTypes?.joinToString() })
        private val modifierRank = listOf(
            "public", "protected", "private", "abstract", "static", "final", "native", "synchronized",
            "strictfp", "transient", "volatile", "transitive",
        ).withIndex().associate { it.value to it.index }

        private fun symbolOrder(value: SymbolId): String = buildString {
            append(value.artifact?.groupId).append(':').append(value.artifact?.artifactId).append(':')
            append(value.packageName).append('.').append(value.classNames.joinToString("."))
            append('#').append(value.callable?.name).append('(')
            append(value.callable?.parameterTypes?.joinToString()).append(')')
        }
    }
}
