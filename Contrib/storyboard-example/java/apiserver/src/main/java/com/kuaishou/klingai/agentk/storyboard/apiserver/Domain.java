package com.kuaishou.klingai.agentk.storyboard.apiserver;

import com.fasterxml.jackson.annotation.JsonValue;
import java.time.Instant;
import java.util.List;
import java.util.Map;

record Models(String chat, String image, String video) {}

record StoryboardArgs(
        String topic,
        int shots,
        String chatModel,
        String imageModel,
        String videoModel) {}

record Artifact(
        String type,
        String uri,
        String description,
        String model,
        Map<String, Object> params) {}

record StoryboardOutput(List<Artifact> artifacts, int clipCount) {}

enum JobStatus {
    RUNNING("running"),
    COMPLETED("completed"),
    FAILED("failed");

    private final String wireValue;

    JobStatus(String wireValue) {
        this.wireValue = wireValue;
    }

    @JsonValue
    String wireValue() {
        return wireValue;
    }
}

record PipelineSnapshot(
        String capabilityId,
        JobStatus status,
        StoryboardOutput output,
        String error) {}

record Job(
        String id,
        String sessionId,
        String executionId,
        String topic,
        int shots,
        JobStatus status,
        StoryboardOutput output,
        String error,
        Instant createdAt,
        Instant finishedAt) {}

record CreateJobRequest(String topic, Integer shots) {}

record JobResponse(
        String id,
        JobStatus status,
        String topic,
        int shots,
        StoryboardOutput output,
        String error,
        Instant createdAt,
        Instant finishedAt) {}
