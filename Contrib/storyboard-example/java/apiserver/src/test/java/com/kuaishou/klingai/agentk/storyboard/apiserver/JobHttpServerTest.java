package com.kuaishou.klingai.agentk.storyboard.apiserver;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.klingai.agentk.api.v1.Profile;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Clock;
import java.time.Instant;
import java.time.ZoneOffset;
import java.util.ArrayDeque;
import java.util.List;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

class JobHttpServerTest {
    private final ObjectMapper mapper = JsonSupport.createMapper();
    private final HttpClient http = HttpClient.newHttpClient();
    private JobHttpServer server;

    @AfterEach
    void closeServer() {
        if (server != null) {
            server.close();
        }
    }

    @Test
    void createsAndRefreshesAJobWithoutLeakingAgentKCoordinates() throws Exception {
        FakeStoryboardClient agentK = new FakeStoryboardClient();
        agentK.snapshot = new PipelineSnapshot(
                "contrib.storyboard_example.storyboard_film_go",
                JobStatus.COMPLETED,
                output(),
                null);
        startServer(agentK, new ArrayDeque<>(List.of("job_test")));

        HttpResponse<String> created = request("POST", "/v1/jobs", """
                {"topic":" rain city "}
                """);
        assertEquals(202, created.statusCode());
        JsonNode createBody = mapper.readTree(created.body());
        assertEquals("job_test", createBody.get("id").asText());
        assertEquals("running", createBody.get("status").asText());
        assertEquals("rain city", createBody.get("topic").asText());
        assertEquals(1, createBody.get("shots").asInt());
        assertFalse(createBody.has("sessionId"));
        assertFalse(createBody.has("executionId"));
        assertEquals("Storyboard Film: rain city", agentK.title);
        assertEquals("rain city", agentK.args.topic());
        assertEquals("deepseek-v4-pro", agentK.args.chatModel());
        assertEquals("z-image-turbo", agentK.args.imageModel());
        assertEquals("kling-3.0", agentK.args.videoModel());

        HttpResponse<String> fetched = request("GET", "/v1/jobs/job_test", null);
        assertEquals(200, fetched.statusCode());
        JsonNode body = mapper.readTree(fetched.body());
        assertEquals("completed", body.get("status").asText());
        assertEquals("https://video.example/film.mp4", body.at("/output/artifacts/0/uri").asText());
        assertEquals(1, body.at("/output/clipCount").asInt());
        assertNotNull(body.get("finishedAt"));
        assertFalse(body.has("sessionId"));
        assertFalse(body.has("executionId"));
        assertEquals(1, agentK.getCalls.get());

        request("GET", "/v1/jobs/job_test", null);
        assertEquals(1, agentK.getCalls.get(), "terminal jobs must not be refreshed twice");
    }

    @Test
    void listsJobsNewestFirstAndRefreshesRunningJobs() throws Exception {
        FakeStoryboardClient agentK = new FakeStoryboardClient();
        agentK.snapshot = new PipelineSnapshot(
                "contrib.storyboard_example.storyboard_film_go",
                JobStatus.RUNNING,
                null,
                null);
        startServer(agentK, new ArrayDeque<>(List.of("job_first", "job_second")));
        assertEquals(202, request("POST", "/v1/jobs", "{\"topic\":\"first\",\"shots\":2}").statusCode());
        assertEquals(202, request("POST", "/v1/jobs", "{\"topic\":\"second\",\"shots\":1}").statusCode());

        HttpResponse<String> response = request("GET", "/v1/jobs", null);
        assertEquals(200, response.statusCode());
        JsonNode jobs = mapper.readTree(response.body()).get("jobs");
        assertEquals(2, jobs.size());
        assertEquals("job_second", jobs.get(0).get("id").asText());
        assertEquals("job_first", jobs.get(1).get("id").asText());
        assertEquals(2, agentK.getCalls.get());
    }

    @Test
    void rejectsInvalidBodiesBeforeCallingAgentK() throws Exception {
        FakeStoryboardClient agentK = new FakeStoryboardClient();
        startServer(agentK, new ArrayDeque<>(List.of("unused")));
        List<String> invalid = List.of(
                "",
                "{\"topic\":",
                "{\"topic\":\" \"}",
                "{\"topic\":\"x\",\"shots\":0}",
                "{\"topic\":\"x\",\"shots\":9}",
                "{\"topic\":\"x\",\"other\":true}",
                "{\"topic\":\"x\"} {\"topic\":\"y\"}");
        for (String body : invalid) {
            assertEquals(400, request("POST", "/v1/jobs", body).statusCode(), body);
        }
        String oversized = "{\"topic\":\"" + "x".repeat((64 << 10) + 1) + "\"}";
        assertEquals(400, request("POST", "/v1/jobs", oversized).statusCode());
        assertEquals(0, agentK.createCalls.get());
    }

    @Test
    void mapsAgentKErrorsToBadGateway() throws Exception {
        FakeStoryboardClient agentK = new FakeStoryboardClient();
        startServer(agentK, new ArrayDeque<>(List.of("job_test")));
        agentK.createError = new AgentKCallException("unavailable");
        HttpResponse<String> createFailed = request("POST", "/v1/jobs", "{\"topic\":\"x\"}");
        assertEquals(502, createFailed.statusCode());
        assertEquals("failed to create AgentK session", mapper.readTree(createFailed.body()).get("error").asText());

        agentK.createError = null;
        agentK.startError = new AgentKCallException("unavailable");
        HttpResponse<String> startFailed = request("POST", "/v1/jobs", "{\"topic\":\"x\"}");
        assertEquals(502, startFailed.statusCode());
        assertEquals("failed to start AgentK pipeline", mapper.readTree(startFailed.body()).get("error").asText());

        agentK.startError = null;
        assertEquals(202, request("POST", "/v1/jobs", "{\"topic\":\"x\"}").statusCode());
        agentK.getError = new AgentKCallException("unavailable");
        HttpResponse<String> getFailed = request("GET", "/v1/jobs/job_test", null);
        assertEquals(502, getFailed.statusCode());
        assertEquals("failed to query AgentK pipeline", mapper.readTree(getFailed.body()).get("error").asText());
    }

    @Test
    void servesHealthAndApiDocsAndRejectsUnknownRoutes() throws Exception {
        startServer(new FakeStoryboardClient(), new ArrayDeque<>());
        HttpResponse<String> health = request("GET", "/healthz", null);
        assertEquals(200, health.statusCode());
        assertEquals("ok", health.body());
        assertEquals(200, request("GET", "/docs", null).statusCode());
        assertTrue(request("GET", "/openapi.yaml", null).body().contains("openapi: 3.1.0"));
        assertEquals(404, request("GET", "/missing", null).statusCode());
        assertEquals(404, request("GET", "/v1/jobs/missing", null).statusCode());
        assertEquals(405, request("PUT", "/v1/jobs", "{}").statusCode());
    }

    private void startServer(FakeStoryboardClient agentK, ArrayDeque<String> ids) throws Exception {
        server = new JobHttpServer(
                "127.0.0.1",
                0,
                agentK,
                new MemoryJobStore(),
                Profile.newBuilder().setDefaultModel("deepseek-v4-pro").build(),
                new Models("deepseek-v4-pro", "z-image-turbo", "kling-3.0"),
                mapper,
                Clock.fixed(Instant.parse("2026-08-18T02:00:00Z"), ZoneOffset.UTC),
                ids::removeFirst);
        server.start();
    }

    private HttpResponse<String> request(String method, String path, String body) throws Exception {
        HttpRequest.Builder request = HttpRequest.newBuilder()
                .uri(URI.create("http://127.0.0.1:" + server.port() + path));
        if (body == null) {
            request.method(method, HttpRequest.BodyPublishers.noBody());
        } else {
            request.header("Content-Type", "application/json")
                    .method(method, HttpRequest.BodyPublishers.ofString(body));
        }
        return http.send(request.build(), HttpResponse.BodyHandlers.ofString());
    }

    private static StoryboardOutput output() {
        return new StoryboardOutput(
                List.of(new Artifact(
                        "video",
                        "https://video.example/film.mp4",
                        "rain city film",
                        "kling-3.0",
                        null)),
                1);
    }

    private static final class FakeStoryboardClient implements StoryboardAgentKClient {
        final AtomicInteger createCalls = new AtomicInteger();
        final AtomicInteger getCalls = new AtomicInteger();
        String title;
        StoryboardArgs args;
        PipelineSnapshot snapshot = new PipelineSnapshot(
                "contrib.storyboard_example.storyboard_film_go",
                JobStatus.RUNNING,
                null,
                null);
        AgentKCallException createError;
        AgentKCallException startError;
        AgentKCallException getError;

        @Override
        public String createSession(String title, Profile profile) throws AgentKCallException {
            createCalls.incrementAndGet();
            this.title = title;
            if (createError != null) {
                throw createError;
            }
            return "ses_test";
        }

        @Override
        public String startPipeline(String sessionId, StoryboardArgs args) throws AgentKCallException {
            this.args = args;
            if (startError != null) {
                throw startError;
            }
            return "exe_test";
        }

        @Override
        public PipelineSnapshot getPipelineExecution(String sessionId, String executionId)
                throws AgentKCallException {
            getCalls.incrementAndGet();
            if (getError != null) {
                throw getError;
            }
            return snapshot;
        }

        @Override
        public void close() {}
    }
}
