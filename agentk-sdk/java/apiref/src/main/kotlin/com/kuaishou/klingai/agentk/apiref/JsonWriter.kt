package com.kuaishou.klingai.agentk.apiref

internal object ApiReferenceJson {
    fun write(reference: Reference): String = JsonWriter().apply { reference(reference) }.toString()

    fun write(artifact: ArtifactDoc): String = JsonWriter().apply { artifact(artifact) }.toString()
}

private class JsonWriter {
    private val output = StringBuilder()
    private val first = ArrayDeque<Boolean>()
    private var afterName = false

    override fun toString(): String = output.toString()

    private fun beforeValue() {
        if (afterName) {
            afterName = false
            return
        }
        if (first.isNotEmpty()) {
            if (first.last()) first.removeLast().also { first.addLast(false) } else output.append(',')
        }
    }

    private fun beginObject() {
        beforeValue()
        output.append('{')
        first.addLast(true)
    }

    private fun endObject() {
        output.append('}')
        first.removeLast()
    }

    private fun beginArray() {
        beforeValue()
        output.append('[')
        first.addLast(true)
    }

    private fun endArray() {
        output.append(']')
        first.removeLast()
    }

    private fun name(value: String) {
        if (first.last()) first.removeLast().also { first.addLast(false) } else output.append(',')
        stringRaw(value)
        output.append(':')
        afterName = true
    }

    private fun string(value: String?) {
        beforeValue()
        if (value == null) output.append("null") else stringRaw(value)
    }

    private fun number(value: Int) {
        beforeValue()
        output.append(value)
    }

    private fun stringRaw(value: String) {
        output.append('"')
        value.forEach { char ->
            when (char) {
                '"' -> output.append("\\\"")
                '\\' -> output.append("\\\\")
                '\b' -> output.append("\\b")
                '\u000c' -> output.append("\\f")
                '\n' -> output.append("\\n")
                '\r' -> output.append("\\r")
                '\t' -> output.append("\\t")
                else -> if (char < ' ') output.append("\\u%04x".format(char.code)) else output.append(char)
            }
        }
        output.append('"')
    }

    private inline fun <T> array(values: List<T>, write: (T) -> Unit) {
        beginArray()
        values.forEach(write)
        endArray()
    }

    private fun strings(values: List<String>) = array(values, ::string)

    fun reference(value: Reference) {
        beginObject()
        name("artifacts"); array(value.artifacts, ::artifact)
        endObject()
    }

    fun artifact(value: ArtifactDoc) {
        beginObject()
        name("groupId"); string(value.groupId)
        name("artifactId"); string(value.artifactId)
        name("packages"); array(value.packages, ::packageDoc)
        endObject()
    }

    private fun coordinate(value: ArtifactCoordinate) {
        beginObject()
        name("groupId"); string(value.groupId)
        name("artifactId"); string(value.artifactId)
        endObject()
    }

    private fun packageDoc(value: PackageDoc) {
        beginObject()
        name("name"); string(value.name)
        name("documentation"); documentation(value.documentation)
        name("types"); array(value.types, ::typeDoc)
        endObject()
    }

    private fun typeDoc(value: TypeDoc) {
        beginObject()
        name("id"); symbol(value.id)
        name("kind"); string(value.kind.name.lowercase())
        name("name"); string(value.name)
        name("modifiers"); strings(value.modifiers)
        name("annotations"); array(value.annotations, ::annotation)
        name("typeParameters"); array(value.typeParameters, ::typeParameter)
        name("superClass"); nullable(value.superClass, ::typeRef)
        name("interfaces"); array(value.interfaces, ::typeRef)
        name("declaration"); annotatedText(value.declaration)
        name("documentation"); documentation(value.documentation)
        name("nestedTypes"); array(value.nestedTypes, ::typeDoc)
        name("enumConstants"); array(value.enumConstants, ::field)
        name("fields"); array(value.fields, ::field)
        name("constructors"); array(value.constructors, ::executable)
        name("methods"); array(value.methods, ::executable)
        name("inheritedMembers"); array(value.inheritedMembers, ::inheritedGroup)
        endObject()
    }

    private fun field(value: FieldDoc) {
        beginObject()
        name("id"); symbol(value.id)
        name("name"); string(value.name)
        name("modifiers"); strings(value.modifiers)
        name("annotations"); array(value.annotations, ::annotation)
        name("type"); nullable(value.type, ::typeRef)
        name("defaultValue"); nullable(value.defaultValue, ::annotationValue)
        name("declaration"); annotatedText(value.declaration)
        name("documentation"); documentation(value.documentation)
        endObject()
    }

    private fun executable(value: ExecutableDoc) {
        beginObject()
        name("id"); symbol(value.id)
        name("name"); string(value.name)
        name("modifiers"); strings(value.modifiers)
        name("annotations"); array(value.annotations, ::annotation)
        name("typeParameters"); array(value.typeParameters, ::typeParameter)
        name("parameters"); array(value.parameters, ::parameter)
        name("returnType"); nullable(value.returnType, ::typeRef)
        name("throwsTypes"); array(value.throwsTypes, ::typeRef)
        name("defaultValue"); nullable(value.defaultValue, ::annotationValue)
        name("declaration"); annotatedText(value.declaration)
        name("documentation"); documentation(value.documentation)
        endObject()
    }

    private fun parameter(value: ParameterDoc) {
        beginObject()
        name("name"); string(value.name)
        name("annotations"); array(value.annotations, ::annotation)
        name("type"); typeRef(value.type)
        name("defaultValue"); nullable(value.defaultValue, ::annotationValue)
        endObject()
    }

    private fun typeParameter(value: TypeParameterDoc) {
        beginObject()
        name("name"); string(value.name)
        name("annotations"); array(value.annotations, ::annotation)
        name("bounds"); array(value.bounds, ::typeRef)
        name("documentation"); documentation(value.documentation)
        endObject()
    }

    private fun typeRef(value: TypeRef) {
        beginObject()
        name("declaration"); annotatedText(value.declaration)
        endObject()
    }

    private fun inheritedGroup(value: InheritedMemberGroup) {
        beginObject()
        name("declaringType"); symbol(value.declaringType)
        name("fields"); array(value.fields, ::symbol)
        name("methods"); array(value.methods, ::symbol)
        endObject()
    }

    private fun symbol(value: SymbolId) {
        beginObject()
        name("artifact"); nullable(value.artifact, ::coordinate)
        name("packageName"); string(value.packageName)
        name("classNames"); strings(value.classNames)
        name("callable"); nullable(value.callable, ::callable)
        endObject()
    }

    private fun callable(value: CallableId) {
        beginObject()
        name("name"); string(value.name)
        name("parameterTypes"); strings(value.parameterTypes)
        endObject()
    }

    private fun annotatedText(value: AnnotatedText) {
        beginObject()
        name("text"); string(value.text)
        name("references"); array(value.references, ::textReference)
        endObject()
    }

    private fun textReference(value: TextReference) {
        beginObject()
        name("start"); number(value.start)
        name("end"); number(value.end)
        name("target"); symbol(value.target)
        endObject()
    }

    private fun documentation(value: Documentation) {
        beginObject()
        name("children"); array(value.children, ::docNode)
        endObject()
    }

    private fun docNode(value: DocNode) {
        beginObject()
        name("kind"); string(value.kind)
        name("text"); string(value.text)
        name("name"); string(value.name)
        name("target"); nullable(value.target, ::symbol)
        name("attributes")
        beginObject()
        value.attributes.toSortedMap().forEach { (key, attribute) -> name(key); string(attribute) }
        endObject()
        name("children"); array(value.children, ::docNode)
        endObject()
    }

    private fun annotation(value: AnnotationDoc) {
        beginObject()
        name("type"); symbol(value.type)
        name("arguments"); array(value.arguments, ::annotationArgument)
        endObject()
    }

    private fun annotationArgument(value: AnnotationArgumentDoc) {
        beginObject()
        name("name"); string(value.name)
        name("value"); annotationValue(value.value)
        endObject()
    }

    private fun annotationValue(value: AnnotationValueDoc) {
        beginObject()
        name("kind"); string(value.kind)
        name("text"); string(value.text)
        name("target"); nullable(value.target, ::symbol)
        name("annotation"); nullable(value.annotation, ::annotation)
        name("values"); array(value.values, ::annotationValue)
        endObject()
    }

    private fun <T> nullable(value: T?, write: (T) -> Unit) {
        if (value == null) string(null) else write(value)
    }
}
