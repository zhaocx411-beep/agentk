package com.kuaishou.klingai.agentk.storyboard.apiserver;

import java.time.Instant;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

final class MemoryJobStore {
    private final Map<String, Job> jobs = new LinkedHashMap<>();

    synchronized void create(Job job) {
        if (jobs.containsKey(job.id())) {
            throw new IllegalArgumentException("job " + job.id() + " already exists");
        }
        jobs.put(job.id(), job);
    }

    synchronized Job get(String id) {
        return jobs.get(id);
    }

    synchronized List<Job> list() {
        List<Job> result = new ArrayList<>(jobs.values());
        java.util.Collections.reverse(result);
        return List.copyOf(result);
    }

    synchronized Job finish(
            String id,
            JobStatus status,
            StoryboardOutput output,
            String error,
            Instant finishedAt) {
        if (status != JobStatus.COMPLETED && status != JobStatus.FAILED) {
            throw new IllegalArgumentException("cannot finish job with status " + status);
        }
        Job current = jobs.get(id);
        if (current == null) {
            throw new IllegalArgumentException("job " + id + " not found");
        }
        if (current.status() != JobStatus.RUNNING) {
            return current;
        }
        Job updated = new Job(
                current.id(),
                current.sessionId(),
                current.executionId(),
                current.topic(),
                current.shots(),
                status,
                output,
                error,
                current.createdAt(),
                finishedAt);
        jobs.put(id, updated);
        return updated;
    }
}
