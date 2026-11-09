package com.kuaishou.klingai.agentk.storyboard.apiserver;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.klingai.agentk.api.v1.Profile;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.InputStream;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import java.time.Clock;
import java.time.Instant;
import java.util.HexFormat;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.function.Supplier;

final class JobHttpServer implements AutoCloseable {
    private static final int MAX_REQUEST_BODY = 64 << 10;
    private static final SecureRandom RANDOM = new SecureRandom();

    private final HttpServer server;
    private final ExecutorService executor;
    private final StoryboardAgentKClient agentK;
    private final MemoryJobStore store;
    private final Profile profile;
    private final Models models;
    private final ObjectMapper mapper;
    private final Clock clock;
    private final Supplier<String> newId;
    private final byte[] openApi;
    private final byte[] scalar;
    private final AtomicBoolean closed = new AtomicBoolean();

    JobHttpServer(
            String host,
            int port,
            StoryboardAgentKClient agentK,
            MemoryJobStore store,
            Profile profile,
            Models models,
            ObjectMapper mapper,
            Clock clock,
            Supplier<String> newId) throws IOException {
        this.server = HttpServer.create(new InetSocketAddress(host, port), 0);
        this.executor = Executors.newCachedThreadPool();
        this.agentK = Objects.requireNonNull(agentK);
        this.store = Objects.requireNonNull(store);
        this.profile = Objects.requireNonNull(profile);
        this.models = Objects.requireNonNull(models);
        this.mapper = Objects.requireNonNull(mapper);
        this.clock = Objects.requireNonNull(clock);
        this.newId = Objects.requireNonNull(newId);
        this.openApi = readResource("/openapi.yaml");
        this.scalar = readResource("/scalar.html");
        server.setExecutor(executor);
        server.createContext("/", this::handle);
    }

    static JobHttpServer create(AppConfig config, StoryboardAgentKClient agentK, Profile profile)
            throws IOException {
        return new JobHttpServer(
                config.host(),
                config.port(),
                agentK,
                new MemoryJobStore(),
                profile,
                config.models(),
                JsonSupport.createMapper(),
                Clock.systemUTC(),
                JobHttpServer::newJobId);
    }

    void start() {
        server.start();
    }

    int port() {
        return server.getAddress().getPort();
    }

    private void handle(HttpExchange exchange) throws IOException {
        try {
            dispatch(exchange);
        } catch (Exception error) {
            System.err.println("api: unexpected error: " + error);
            writeError(exchange, 500, "internal server error");
        } finally {
            exchange.close();
        }
    }

    private void dispatch(HttpExchange exchange) throws IOException {
        String method = exchange.getRequestMethod();
        String path = exchange.getRequestURI().getPath();

        if (path.equals("/healthz")) {
            if (!method.equals("GET")) {
                methodNotAllowed(exchange, "GET");
                return;
            }
            writeBytes(exchange, 200, "text/plain; charset=utf-8", "ok".getBytes(StandardCharsets.UTF_8));
            return;
        }
        if (path.equals("/docs")) {
            if (!method.equals("GET")) {
                methodNotAllowed(exchange, "GET");
                return;
            }
            writeBytes(exchange, 200, "text/html; charset=utf-8", scalar);
            return;
        }
        if (path.equals("/docs/")) {
            exchange.getResponseHeaders().set("Location", "/docs");
            exchange.sendResponseHeaders(308, -1);
            return;
        }
        if (path.equals("/openapi.yaml")) {
            if (!method.equals("GET")) {
                methodNotAllowed(exchange, "GET");
                return;
            }
            writeBytes(exchange, 200, "application/yaml; charset=utf-8", openApi);
            return;
        }
        if (path.equals("/v1/jobs")) {
            if (method.equals("POST")) {
                createJob(exchange);
            } else if (method.equals("GET")) {
                listJobs(exchange);
            } else {
                methodNotAllowed(exchange, "GET, POST");
            }
            return;
        }
        if (path.startsWith("/v1/jobs/") && path.length() > "/v1/jobs/".length()) {
            if (!method.equals("GET")) {
                methodNotAllowed(exchange, "GET");
                return;
            }
            getJob(exchange, path.substring("/v1/jobs/".length()));
            return;
        }
        writeError(exchange, 404, "not found");
    }

    private void createJob(HttpExchange exchange) throws IOException {
        CreateJobRequest request;
        try {
            request = mapper.readValue(readLimited(exchange.getRequestBody()), CreateJobRequest.class);
        } catch (RequestTooLargeException error) {
            writeError(exchange, 400, "invalid JSON body");
            return;
        } catch (JsonProcessingException error) {
            writeError(exchange, 400, "invalid JSON body");
            return;
        }

        String topic = request.topic() == null ? "" : request.topic().trim();
        int shots = request.shots() == null ? 1 : request.shots();
        if (topic.isEmpty()) {
            writeError(exchange, 400, "topic is required");
            return;
        }
        if (shots < 1 || shots > 8) {
            writeError(exchange, 400, "shots must be an integer between 1 and 8");
            return;
        }

        String sessionId;
        try {
            sessionId = agentK.createSession("Storyboard Film: " + topic, profile);
        } catch (AgentKCallException error) {
            System.err.println("api: create AgentK session: " + error.getMessage());
            writeError(exchange, 502, "failed to create AgentK session");
            return;
        }
        String executionId;
        try {
            executionId = agentK.startPipeline(
                    sessionId,
                    new StoryboardArgs(
                            topic,
                            shots,
                            models.chat(),
                            models.image(),
                            models.video()));
        } catch (AgentKCallException error) {
            System.err.println("api: start AgentK pipeline: " + error.getMessage());
            writeError(exchange, 502, "failed to start AgentK pipeline");
            return;
        }

        Job job = new Job(
                newId.get(),
                sessionId,
                executionId,
                topic,
                shots,
                JobStatus.RUNNING,
                null,
                null,
                clock.instant(),
                null);
        try {
            store.create(job);
        } catch (RuntimeException error) {
            System.err.println("api: store job " + job.id() + ": " + error.getMessage());
            writeError(exchange, 500, "failed to create job");
            return;
        }
        writeJson(exchange, 202, response(job));
    }

    private void getJob(HttpExchange exchange, String jobId) throws IOException {
        Job job = store.get(jobId);
        if (job == null) {
            writeError(exchange, 404, "job not found");
            return;
        }
        try {
            writeJson(exchange, 200, response(refresh(job)));
        } catch (AgentKCallException error) {
            System.err.println("api: refresh job " + job.id() + ": " + error.getMessage());
            writeError(exchange, 502, "failed to query AgentK pipeline");
        }
    }

    private void listJobs(HttpExchange exchange) throws IOException {
        List<Job> jobs = store.list();
        java.util.ArrayList<JobResponse> responses = new java.util.ArrayList<>(jobs.size());
        try {
            for (Job job : jobs) {
                responses.add(response(refresh(job)));
            }
        } catch (AgentKCallException error) {
            System.err.println("api: refresh jobs: " + error.getMessage());
            writeError(exchange, 502, "failed to query AgentK pipeline");
            return;
        }
        writeJson(exchange, 200, Map.of("jobs", responses));
    }

    private Job refresh(Job job) throws AgentKCallException {
        if (job.status() != JobStatus.RUNNING) {
            return job;
        }
        PipelineSnapshot snapshot = agentK.getPipelineExecution(job.sessionId(), job.executionId());
        if (snapshot.status() == JobStatus.RUNNING) {
            return job;
        }
        if (snapshot.status() == JobStatus.COMPLETED) {
            if (!validOutput(snapshot.output())) {
                throw new AgentKCallException("completed pipeline returned an invalid output");
            }
            return store.finish(
                    job.id(), JobStatus.COMPLETED, snapshot.output(), null, clock.instant());
        }
        String error = snapshot.error() == null || snapshot.error().trim().isEmpty()
                ? "pipeline failed"
                : snapshot.error().trim();
        return store.finish(job.id(), JobStatus.FAILED, null, error, clock.instant());
    }

    private static boolean validOutput(StoryboardOutput output) {
        return output != null
                && output.artifacts() != null
                && output.artifacts().size() == 1
                && "video".equals(output.artifacts().get(0).type())
                && output.artifacts().get(0).uri() != null
                && !output.artifacts().get(0).uri().trim().isEmpty()
                && output.clipCount() >= 1;
    }

    private static JobResponse response(Job job) {
        return new JobResponse(
                job.id(),
                job.status(),
                job.topic(),
                job.shots(),
                job.output(),
                blankToNull(job.error()),
                job.createdAt(),
                job.finishedAt());
    }

    private static String blankToNull(String value) {
        return value == null || value.isBlank() ? null : value;
    }

    private byte[] readLimited(InputStream input) throws IOException, RequestTooLargeException {
        byte[] body = input.readNBytes(MAX_REQUEST_BODY + 1);
        if (body.length > MAX_REQUEST_BODY) {
            throw new RequestTooLargeException();
        }
        return body;
    }

    private void writeJson(HttpExchange exchange, int status, Object value) throws IOException {
        writeBytes(exchange, status, "application/json", mapper.writeValueAsBytes(value));
    }

    private void writeError(HttpExchange exchange, int status, String message) throws IOException {
        writeJson(exchange, status, Map.of("error", message));
    }

    private static void writeBytes(
            HttpExchange exchange, int status, String contentType, byte[] body) throws IOException {
        exchange.getResponseHeaders().set("Content-Type", contentType);
        exchange.getResponseHeaders().set("Cache-Control", "no-cache");
        exchange.sendResponseHeaders(status, body.length);
        exchange.getResponseBody().write(body);
    }

    private static void methodNotAllowed(HttpExchange exchange, String allow) throws IOException {
        exchange.getResponseHeaders().set("Allow", allow);
        exchange.sendResponseHeaders(405, -1);
    }

    private static byte[] readResource(String name) throws IOException {
        try (InputStream input = JobHttpServer.class.getResourceAsStream(name)) {
            if (input == null) {
                throw new IOException("missing classpath resource " + name);
            }
            return input.readAllBytes();
        }
    }

    private static String newJobId() {
        byte[] raw = new byte[12];
        RANDOM.nextBytes(raw);
        return "job_" + HexFormat.of().formatHex(raw);
    }

    @Override
    public void close() {
        if (!closed.compareAndSet(false, true)) {
            return;
        }
        server.stop(1);
        executor.shutdown();
        try {
            if (!executor.awaitTermination(5, TimeUnit.SECONDS)) {
                executor.shutdownNow();
            }
        } catch (InterruptedException error) {
            Thread.currentThread().interrupt();
            executor.shutdownNow();
        }
    }

    private static final class RequestTooLargeException extends Exception {}
}
