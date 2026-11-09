package com.kuaishou.klingai.agentk.apiref

import java.io.File
import java.util.Properties
import java.util.zip.ZipFile
import org.jetbrains.dokka.links.DRI
import org.jetbrains.dokka.plugability.DokkaContext

internal class SymbolIndex(
    private val currentArtifact: ArtifactCoordinate,
    classpath: List<File>,
) {
    private val aliases = mutableMapOf<DriKey, SymbolId>()
    private val owners = mutableMapOf<String, ArtifactCoordinate>()

    init {
        classpath.filter { it.isFile && it.extension == "jar" }.forEach(::indexJar)
    }

    fun register(raw: DRI, normalized: SymbolId) {
        aliases[key(raw)] = normalized
    }

    fun typeId(raw: DRI, ownerPath: List<String>? = null): SymbolId {
        aliases[key(raw)]?.let { return it.copy(callable = null) }
        val packageName = raw.packageName.orEmpty()
        val classNames = ownerPath ?: raw.classNames.orEmpty().split('.').filter(String::isNotEmpty)
        val artifact = when {
            ownerPath != null -> currentArtifact
            packageName.startsWith("java.") || packageName.startsWith("javax.") || packageName.startsWith("jdk.") -> null
            else -> owners[(listOf(packageName) + classNames).filter(String::isNotEmpty).joinToString(".")]
        }
        return SymbolId(artifact, packageName, classNames, null)
    }

    fun callableId(raw: DRI, ownerPath: List<String>, name: String): SymbolId {
        val callable = CallableId(
            name = name,
            parameterTypes = raw.callable?.params?.map { it.toString() }.orEmpty(),
        )
        return SymbolId(currentArtifact, raw.packageName.orEmpty(), ownerPath, callable)
    }

    fun resolve(raw: DRI): SymbolId {
        aliases[key(raw)]?.let { return it }
        val owner = typeId(raw)
        val callable = raw.callable?.let {
            CallableId(it.name, it.params.map { parameter -> parameter.toString() })
        }
        return owner.copy(callable = callable)
    }

    private fun key(dri: DRI): DriKey = DriKey(
        packageName = dri.packageName.orEmpty(),
        classNames = dri.classNames.orEmpty(),
        callableName = dri.callable?.name,
        parameterTypes = dri.callable?.params?.map { it.toString() }.orEmpty(),
    )

    private fun indexJar(file: File) {
        runCatching {
            ZipFile(file).use { jar ->
                val coordinate = jar.entries().asSequence()
                    .filter { it.name.startsWith("META-INF/maven/") && it.name.endsWith("/pom.properties") }
                    .mapNotNull { entry ->
                        val properties = Properties().apply { jar.getInputStream(entry).use(::load) }
                        val groupId = properties.getProperty("groupId") ?: return@mapNotNull null
                        val artifactId = properties.getProperty("artifactId") ?: return@mapNotNull null
                        ArtifactCoordinate(groupId, artifactId)
                    }
                    .firstOrNull() ?: return@use

                jar.entries().asSequence()
                    .filter { !it.isDirectory && it.name.endsWith(".class") && !it.name.startsWith("META-INF/versions/") }
                    .map { it.name.removeSuffix(".class").replace('/', '.').replace('$', '.') }
                    .forEach { owners.putIfAbsent(it, coordinate) }
            }
        }
    }

    private data class DriKey(
        val packageName: String,
        val classNames: String,
        val callableName: String?,
        val parameterTypes: List<String>,
    )

    companion object {
        fun from(context: DokkaContext, coordinate: ArtifactCoordinate): SymbolIndex = SymbolIndex(
            coordinate,
            context.configuration.sourceSets.flatMap { it.classpath }.distinct(),
        )
    }
}
