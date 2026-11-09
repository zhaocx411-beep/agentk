package com.kuaishou.agent.operation.backend.keffects;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.TimeUnit;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import okhttp3.MediaType;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.RequestBody;
import okhttp3.Response;
import okhttp3.ResponseBody;

/**
 * KEffects / kling-aigc-engine HTTP 客户端。
 */
@Component
public class KEffectsClient {

    private static final Logger LOGGER = LoggerFactory.getLogger(KEffectsClient.class);
    private static final MediaType JSON = MediaType.parse("application/json; charset=utf-8");

    private final KEffectsProperties properties;
    private final ObjectMapper objectMapper;
    private final OkHttpClient httpClient;

    public KEffectsClient(KEffectsProperties properties, ObjectMapper objectMapper) {
        this.properties = properties;
        this.objectMapper = objectMapper;
        long connect = Math.max(properties.getConnectTimeoutMs(), 1L);
        long read = Math.max(properties.getReadTimeoutMs(), 1L);
        this.httpClient = new OkHttpClient.Builder()
                .connectTimeout(connect, TimeUnit.MILLISECONDS)
                .readTimeout(read, TimeUnit.MILLISECONDS)
                .writeTimeout(read, TimeUnit.MILLISECONDS)
                .build();
    }

    public JsonNode createTask(Object body, String baseUrl) throws IOException {
        String url = join(baseUrl, "/v1/tasks");
        byte[] bytes = objectMapper.writeValueAsBytes(body);
        Request request = new Request.Builder()
                .url(url)
                .header("Content-Type", "application/json")
                .post(RequestBody.create(JSON, bytes))
                .build();
        return execute(request, "POST", url);
    }

    public JsonNode getTask(String taskId, String baseUrl) throws IOException {
        String url = join(baseUrl, "/v1/tasks/" + taskId);
        Request request = new Request.Builder().url(url).get().build();
        return execute(request, "GET", url);
    }

    public JsonNode cancelTask(String taskId, String baseUrl) throws IOException {
        String url = join(baseUrl, "/v1/tasks/" + taskId);
        Request request = new Request.Builder().url(url).delete().build();
        return execute(request, "DELETE", url);
    }

    private JsonNode execute(Request request, String method, String url) throws IOException {
        long start = System.currentTimeMillis();
        try (Response response = httpClient.newCall(request).execute()) {
            ResponseBody responseBody = response.body();
            byte[] bytes = responseBody == null ? new byte[0] : responseBody.bytes();
            String raw = new String(bytes, StandardCharsets.UTF_8);
            LOGGER.info("keffects {} {} status={} costMs={} bodyLen={}",
                    method, url, response.code(), System.currentTimeMillis() - start, bytes.length);
            if (!response.isSuccessful()) {
                throw new KEffectsUpstreamException(response.code(), raw);
            }
            if (bytes.length == 0) {
                return objectMapper.createObjectNode();
            }
            return objectMapper.readTree(bytes);
        }
    }

    private static String join(String baseUrl, String path) {
        String base = baseUrl == null ? "" : baseUrl.trim();
        if (base.endsWith("/")) {
            base = base.substring(0, base.length() - 1);
        }
        if (!StringUtils.hasText(base)) {
            throw new IllegalStateException("keffects baseUrl is blank");
        }
        return base + (path.startsWith("/") ? path : "/" + path);
    }

    /** 上游非 2xx。 */
    public static final class KEffectsUpstreamException extends IOException {
        private final int status;
        private final String responseBody;

        public KEffectsUpstreamException(int status, String responseBody) {
            super("keffects upstream status=" + status + " body=" + responseBody);
            this.status = status;
            this.responseBody = responseBody;
        }

        public int getStatus() {
            return status;
        }

        public String getResponseBody() {
            return responseBody;
        }
    }
}
