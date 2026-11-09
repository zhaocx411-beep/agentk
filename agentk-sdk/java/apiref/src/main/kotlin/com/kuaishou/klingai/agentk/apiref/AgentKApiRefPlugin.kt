package com.kuaishou.klingai.agentk.apiref

import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import org.jetbrains.dokka.CoreExtensions
import org.jetbrains.dokka.base.DokkaBase
import org.jetbrains.dokka.kotlinAsJava.KotlinAsJavaPlugin
import org.jetbrains.dokka.kotlinAsJava.signatures.JavaSignatureProvider
import org.jetbrains.dokka.model.DModule
import org.jetbrains.dokka.pages.PageNode
import org.jetbrains.dokka.pages.RootPageNode
import org.jetbrains.dokka.plugability.DokkaContext
import org.jetbrains.dokka.plugability.DokkaPlugin
import org.jetbrains.dokka.plugability.DokkaPluginApiPreview
import org.jetbrains.dokka.plugability.PluginApiPreviewAcknowledgement
import org.jetbrains.dokka.renderers.Renderer
import org.jetbrains.dokka.transformers.documentation.DocumentableToPageTranslator

public class AgentKApiRefPlugin : DokkaPlugin() {
    private val kotlinAsJava: KotlinAsJavaPlugin by lazy { plugin<KotlinAsJavaPlugin>() }
    private val dokkaBase: DokkaBase by lazy { plugin<DokkaBase>() }

    public val translator by extending {
        CoreExtensions.documentableToPageTranslator providing ::ApiReferenceTranslator override
            kotlinAsJava.kotlinAsJavaDocumentableToPageTranslator
    }

    public val renderer by extending {
        CoreExtensions.renderer providing ::JsonFragmentRenderer override dokkaBase.htmlRenderer
    }

    @OptIn(DokkaPluginApiPreview::class)
    override fun pluginApiPreviewAcknowledgement(): PluginApiPreviewAcknowledgement =
        PluginApiPreviewAcknowledgement
}

private class ApiReferenceTranslator(private val context: DokkaContext) : DocumentableToPageTranslator {
    override fun invoke(module: DModule): RootPageNode {
        val (groupId, artifactId) = parseCoordinate(context.configuration.moduleName)
        val coordinate = ArtifactCoordinate(groupId, artifactId)
        val symbols = SymbolIndex.from(context, coordinate)
        val artifact = ApiReferenceMapper(
            coordinate = coordinate,
            symbols = symbols,
            signatures = JavaSignatureProvider(context),
        ).map(module)
        return ApiReferenceRootPage(artifact)
    }

    private fun parseCoordinate(moduleName: String): Pair<String, String> {
        val parts = moduleName.split(':', limit = 2)
        require(parts.size == 2 && parts.all(String::isNotBlank)) {
            "Dokka moduleName must be '<groupId>:<artifactId>', got '$moduleName'"
        }
        return parts[0] to parts[1]
    }
}

private class ApiReferenceRootPage(val artifact: ArtifactDoc) : RootPageNode() {
    override val name: String = artifact.artifactId
    override val children: List<PageNode> = emptyList()

    override fun modified(name: String, children: List<PageNode>): RootPageNode = this
}

private class JsonFragmentRenderer(@Suppress("UNUSED_PARAMETER") context: DokkaContext) : Renderer {
    override fun render(root: RootPageNode) {
        require(root is ApiReferenceRootPage) { "Unexpected root page ${root.javaClass.name}" }
        val configured = System.getProperty("apiref.output")
            ?: throw IllegalArgumentException("Missing required -Dapiref.output=<file>")
        val output = java.io.File(configured).toPath().toAbsolutePath().normalize()
        Files.createDirectories(output.parent)
        val temporary = Files.createTempFile(output.parent, ".${output.fileName}.", ".tmp")
        try {
            Files.writeString(temporary, ApiReferenceJson.write(root.artifact) + "\n", StandardCharsets.UTF_8)
            try {
                Files.move(temporary, output, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
            } catch (_: java.nio.file.AtomicMoveNotSupportedException) {
                Files.move(temporary, output, StandardCopyOption.REPLACE_EXISTING)
            }
        } finally {
            Files.deleteIfExists(temporary)
        }
    }
}
