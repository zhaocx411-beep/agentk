package com.kuaishou.agent.operation.backend.platform.support;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.ObjectMapper;

class WorkflowPipelineRefsTest {

    private final ObjectMapper objectMapper = new ObjectMapper();

    @Test
    void extractCapabilityReadsCapabilityFieldOnly() {
        String definition = "{\"capability\":\"contrib.storyboard_film_go\","
                + "\"ref\":\"pipeline:agentk.storyboard_film_go\","
                + "\"name\":\"storyboard_film_go\"}";
        assertThat(WorkflowPipelineRefs.extractCapability(definition, objectMapper))
                .isEqualTo("contrib.storyboard_film_go");
        assertThat(WorkflowPipelineRefs.extractRef(definition, objectMapper))
                .isEqualTo("pipeline:agentk.storyboard_film_go");
    }

    @Test
    void extractCapabilityReturnsNullWhenMissing() {
        String definition = "{\"ref\":\"pipeline:contrib.storyboard_film_go\",\"name\":\"storyboard_film_go\"}";
        assertThat(WorkflowPipelineRefs.extractCapability(definition, objectMapper)).isNull();
    }

    @Test
    void matchesWorkflowKeyAcceptsCapabilityId() {
        String definition = "{\"capability\":\"contrib.storyboard_film_go\",\"name\":\"storyboard_film_go\"}";
        assertThat(WorkflowPipelineRefs.matchesWorkflowKey(
                "contrib.storyboard_film_go", definition, null, "视频分镜理解", objectMapper))
                .isTrue();
        assertThat(WorkflowPipelineRefs.matchesWorkflowKey(
                "pipeline:contrib.storyboard_film_go", definition, null, "视频分镜理解", objectMapper))
                .isTrue();
    }

    @Test
    void extractAgentkIdUsesCapabilityBareId() {
        assertThat(WorkflowPipelineRefs.extractAgentkId(
                "{\"capability\":\"contrib.studio.comic_drama_film\",\"name\":\"comic_drama_film\"}",
                objectMapper))
                .isEqualTo("contrib.studio.comic_drama_film");
        assertThat(WorkflowPipelineRefs.extractAgentkId(
                "{\"capability\":\"pipeline:contrib.studio.comic_drama_film\"}",
                objectMapper))
                .isEqualTo("contrib.studio.comic_drama_film");
        assertThat(WorkflowPipelineRefs.extractAgentkId("{\"name\":\"漫剧成片\"}", objectMapper))
                .isEmpty();
    }
}
