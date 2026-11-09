package com.kuaishou.klingai.agentk.apiref

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class JsonWriterTest {
    @Test
    fun `writes compact deterministic json and empty arrays`() {
        val artifact = ArtifactDoc(
            groupId = "com.example",
            artifactId = "example-api",
            packages = emptyList(),
        )

        val first = ApiReferenceJson.write(Reference(listOf(artifact)))
        val second = ApiReferenceJson.write(Reference(listOf(artifact)))

        assertEquals(
            "{\"artifacts\":[{\"groupId\":\"com.example\",\"artifactId\":\"example-api\",\"packages\":[]}]}",
            first,
        )
        assertEquals(first, second)
        assertFalse(first.contains("\n"))
        assertFalse(first.contains(":null") && first.contains("packages"))
    }

    @Test
    fun `escapes documentation text and sorts attributes`() {
        val doc = Documentation(
            listOf(
                DocNode(
                    kind = "Text",
                    text = "line 1\n\"line 2\"",
                    name = null,
                    target = null,
                    attributes = linkedMapOf("z" to "last", "a" to "first"),
                    children = emptyList(),
                ),
            ),
        )
        val artifact = ArtifactDoc(
            groupId = "g",
            artifactId = "a",
            packages = listOf(PackageDoc("p", doc, emptyList())),
        )

        val json = ApiReferenceJson.write(artifact)

        assertEquals(true, json.contains("line 1\\n\\\"line 2\\\""))
        assertEquals(true, json.indexOf("\"a\"") < json.indexOf("\"z\""))
    }
}
