package com.kuaishou.klingai.agentk.storyboard.apiserver;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertSame;
import static org.junit.jupiter.api.Assertions.assertThrows;

import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Test;

class MemoryJobStoreTest {
    @Test
    void listsNewestFirstAndFinishesOnce() {
        MemoryJobStore store = new MemoryJobStore();
        Job first = job("job_1", Instant.parse("2026-08-18T01:00:00Z"));
        Job second = job("job_2", Instant.parse("2026-08-18T02:00:00Z"));
        store.create(first);
        store.create(second);

        assertEquals(List.of("job_2", "job_1"), store.list().stream().map(Job::id).toList());
        Instant finished = Instant.parse("2026-08-18T03:00:00Z");
        Job completed = store.finish("job_1", JobStatus.COMPLETED, output(), null, finished);
        assertEquals(JobStatus.COMPLETED, completed.status());
        assertEquals(finished, completed.finishedAt());
        assertSame(completed, store.finish(
                "job_1", JobStatus.FAILED, null, "late failure", finished.plusSeconds(1)));
        assertThrows(IllegalArgumentException.class, () -> store.create(first));
    }

    @Test
    void supportsConcurrentCreates() throws Exception {
        MemoryJobStore store = new MemoryJobStore();
        var executor = Executors.newFixedThreadPool(8);
        try {
            List<java.util.concurrent.Future<?>> futures = new ArrayList<>();
            for (int index = 0; index < 100; index++) {
                int id = index;
                futures.add(executor.submit(() -> store.create(job(
                        "job_" + id, Instant.ofEpochSecond(id)))));
            }
            for (var future : futures) {
                future.get(5, TimeUnit.SECONDS);
            }
        } finally {
            executor.shutdownNow();
        }
        assertEquals(100, store.list().size());
    }

    private static Job job(String id, Instant createdAt) {
        return new Job(
                id,
                "ses_" + id,
                "exe_" + id,
                "topic",
                1,
                JobStatus.RUNNING,
                null,
                null,
                createdAt,
                null);
    }

    private static StoryboardOutput output() {
        return new StoryboardOutput(
                List.of(new Artifact("video", "https://video.example/film.mp4", null, null, null)),
                1);
    }
}
