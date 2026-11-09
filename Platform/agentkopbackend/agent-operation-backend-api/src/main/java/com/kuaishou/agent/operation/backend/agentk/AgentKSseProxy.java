package com.kuaishou.agent.operation.backend.agentk;

import java.io.IOException;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.grpc.AgentKEventWireConverter;
import com.kuaishou.agent.operation.backend.agentk.grpc.AgentKSdkHolder;
import com.kuaishou.agent.operation.backend.platform.service.BlobStoreOfficeUrlService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsRequest;
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsResponse;
import com.kuaishou.klingai.agentk.api.v1.WatchEventsRequest;
import com.kuaishou.klingai.agentk.api.v1.WatchEventsResponse;
import com.kuaishou.klingai.agentk.client.EventObserver;
import com.kuaishou.klingai.agentk.client.EventSubscription;

/**
 * 会话 SSE：将 gRPC {@code WatchEvents} server stream 转成前端沿用的 event-stream。
 * 不是 ListEvents 轮询伪造；断连时取消上游订阅。
 */
@Component
public class AgentKSseProxy {

    private static final String HEADER_X_ACCEL_BUFFERING = "X-Accel-Buffering";
    private static final Pattern SESSION_STREAM = Pattern.compile("^/sessions/([^/]+)/stream$");
    private static final long KEEPALIVE_INTERVAL_MS = 15_000L;
    private static final byte[] KEEPALIVE = ": keepalive\n\n".getBytes(StandardCharsets.UTF_8);

    private final AgentKSdkHolder sdkHolder;
    private final AgentKProxyAccessLogger accessLogger;
    private final ObjectMapper objectMapper;
    private final ChatPersistService chatPersistService;
    private final AgentKProperties properties;
    private final BlobStoreOfficeUrlService officeUrlService;

    public AgentKSseProxy(AgentKSdkHolder sdkHolder, AgentKProxyAccessLogger accessLogger,
            ObjectMapper objectMapper, ChatPersistService chatPersistService, AgentKProperties properties,
            BlobStoreOfficeUrlService officeUrlService) {
        this.sdkHolder = sdkHolder;
        this.accessLogger = accessLogger;
        this.objectMapper = objectMapper;
        this.chatPersistService = chatPersistService;
        this.properties = properties;
        this.officeUrlService = officeUrlService;
    }

    public void proxyStream(HttpServletRequest request, HttpServletResponse response, String upstreamPath,
            AgentKOutboundIdentity identity) throws IOException {
        proxyStream(request, response, upstreamPath, request.getQueryString(), identity);
    }

    public void proxyStream(HttpServletRequest request, HttpServletResponse response, String upstreamPath,
            String queryOverride, AgentKOutboundIdentity identity) throws IOException {
        Matcher matcher = SESSION_STREAM.matcher(upstreamPath.startsWith("/") ? upstreamPath : "/" + upstreamPath);
        if (!matcher.matches()) {
            AgentKClient.writeBadGateway(response, "unsupported stream path: " + upstreamPath);
            return;
        }
        String sessionId = matcher.group(1);
        String executionId = resolveExecutionId(sessionId, queryOverride);
        if (!StringUtils.hasText(executionId)) {
            AgentKClient.writeBadGateway(response,
                    "executionId is required; pass ?executionId= from POST /sessions/{id}/messages response");
            return;
        }
        long afterSeq = resolveAfterSeq(queryOverride, request.getHeader("Last-Event-ID"));

        accessLogger.logSseOpen(upstreamPath, queryOverride, identity, request.getHeader("Last-Event-ID"));
        long started = System.currentTimeMillis();
        AtomicLong bytesTransferred = new AtomicLong(0L);
        AtomicLong lastEmittedSeq = new AtomicLong(afterSeq);

        WatchEventsRequest.Builder watchBuilder = WatchEventsRequest.newBuilder()
                .setSessionId(sessionId)
                .setExecutionId(executionId.trim());
        WatchEventsRequest watchRequest = watchBuilder.build();

        response.setStatus(HttpServletResponse.SC_OK);
        response.setCharacterEncoding(StandardCharsets.UTF_8.name());
        response.setContentType("text/event-stream");
        response.setHeader("Cache-Control", "no-cache");
        response.setHeader(HEADER_X_ACCEL_BUFFERING, "no");
        response.setBufferSize(512);

        CountDownLatch done = new CountDownLatch(1);
        AtomicReference<Throwable> failure = new AtomicReference<>();
        AtomicLong nextId = new AtomicLong(1L);
        Object writeLock = new Object();

        EventSubscription subscription = sdkHolder.client().watchEvents(watchRequest, new EventObserver() {
            @Override
            public void onNext(WatchEventsResponse event) {
                try {
                    AgentKEventWireConverter.SseFrame frame =
                            AgentKEventWireConverter.toSseFrame(event, objectMapper);
                    if (frame == null) {
                        return;
                    }
                    if (shouldDropCommittedSeq(frame.seq, lastEmittedSeq.get())) {
                        return;
                    }
                    boolean streamEnd = "stream.end".equals(frame.event);
                    boolean delta = AgentKSseDeltaEnricher.isDeltaEvent(frame.event);
                    Long ts = delta ? System.currentTimeMillis() : null;
                    long idCursor = nextId.getAndIncrement();
                    String data = AgentKSseDeltaEnricher.enrichData(frame.dataJson, idCursor, ts, objectMapper);
                    data = officeUrlService.toOfficeUrl(data);
                    synchronized (writeLock) {
                        bytesTransferred.addAndGet(writeSseFrame(response, frame.event, frame.seq, data));
                    }
                    if (frame.seq > 0) {
                        lastEmittedSeq.updateAndGet(prev -> Math.max(prev, frame.seq));
                    }
                    if (streamEnd) {
                        done.countDown();
                    }
                } catch (IOException e) {
                    failure.set(e);
                    done.countDown();
                }
            }

            @Override
            public void onError(Throwable error) {
                failure.set(error);
                done.countDown();
            }

            @Override
            public void onComplete() {
                done.countDown();
            }
        });

        try {
            while (!done.await(KEEPALIVE_INTERVAL_MS, TimeUnit.MILLISECONDS)) {
                try {
                    synchronized (writeLock) {
                        OutputStream out = response.getOutputStream();
                        out.write(KEEPALIVE);
                        out.flush();
                        response.flushBuffer();
                    }
                    bytesTransferred.addAndGet(KEEPALIVE.length);
                } catch (IOException e) {
                    failure.set(e);
                    break;
                }
            }
            if (failure.get() != null) {
                accessLogger.logSseFailed(upstreamPath, System.currentTimeMillis() - started,
                        failure.get().getMessage());
                synchronized (writeLock) {
                    if (!response.isCommitted()) {
                        AgentKClient.writeBadGateway(response,
                                "agent-k unavailable: " + failure.get().getMessage());
                    }
                }
                return;
            }
            accessLogger.logSseComplete(upstreamPath, HttpServletResponse.SC_OK,
                    System.currentTimeMillis() - started, bytesTransferred.get());
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            synchronized (writeLock) {
                if (!response.isCommitted()) {
                    AgentKClient.writeBadGateway(response, "agent-k stream interrupted");
                }
            }
        } finally {
            subscription.cancel();
        }
    }

    static boolean shouldDropCommittedSeq(long seq, long lastEmittedSeq) {
        return seq > 0 && seq <= lastEmittedSeq;
    }

    private String resolveExecutionId(String sessionId, String queryOverride) {
        String fromQuery = queryParam(queryOverride, "executionId");
        if (!StringUtils.hasText(fromQuery)) {
            fromQuery = queryParam(queryOverride, "execution_id");
        }
        if (StringUtils.hasText(fromQuery)) {
            return fromQuery.trim();
        }
        return chatPersistService.findByAgentkSessionId(sessionId)
                .map(session -> session.getLastAgentkRunId())
                .filter(StringUtils::hasText)
                .map(String::trim)
                .filter(id -> !id.startsWith("turn_"))
                .orElseGet(() -> fetchLatestExecutionId(sessionId));
    }

    private String fetchLatestExecutionId(String sessionId) {
        try {
            ListExecutionsRequest request = ListExecutionsRequest.newBuilder()
                    .setSessionId(sessionId)
                    .setPageSize(1)
                    .build();
            ListExecutionsResponse response = sdkHolder.client().listExecutions(request)
                    .toCompletableFuture()
                    .get(properties.getGrpcReadTimeoutMs(), TimeUnit.MILLISECONDS);
            if (response.getExecutionsCount() > 0 && StringUtils.hasText(response.getExecutions(0).getId())) {
                return response.getExecutions(0).getId().trim();
            }
        } catch (Exception ignored) {
            // best-effort fallback when query/platform cursor are absent
        }
        return null;
    }

    static long resolveAfterSeq(String query, String lastEventId) {
        String raw = queryParam(query, "after");
        if (!StringUtils.hasText(raw) && StringUtils.hasText(lastEventId)) {
            raw = lastEventId.trim();
        }
        if (!StringUtils.hasText(raw)) {
            return 0L;
        }
        try {
            long parsed = Long.parseLong(raw.trim());
            return parsed < 0 ? 0L : parsed;
        } catch (NumberFormatException e) {
            return 0L;
        }
    }

    private int writeSseFrame(HttpServletResponse response, String event, long seq, String dataJson)
            throws IOException {
        StringBuilder frame = new StringBuilder();
        if (seq > 0) {
            frame.append("id: ").append(seq).append('\n');
        }
        if (StringUtils.hasText(event)) {
            frame.append("event: ").append(event).append('\n');
        }
        frame.append("data: ").append(dataJson == null ? "{}" : dataJson).append('\n').append('\n');
        byte[] bytes = frame.toString().getBytes(StandardCharsets.UTF_8);
        OutputStream out = response.getOutputStream();
        out.write(bytes);
        out.flush();
        response.flushBuffer();
        return bytes.length;
    }

    private static String queryParam(String query, String key) {
        if (!StringUtils.hasText(query) || !StringUtils.hasText(key)) {
            return null;
        }
        for (String part : query.split("&")) {
            int eq = part.indexOf('=');
            if (eq > 0 && part.substring(0, eq).equals(key)) {
                return part.substring(eq + 1);
            }
        }
        return null;
    }
}
