package com.kuaishou.agent.operation.backend.platform.service;

import java.net.URI;
import java.net.URL;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.time.LocalDateTime;
import java.time.ZoneId;
import java.time.ZonedDateTime;
import java.time.format.DateTimeFormatter;
import java.util.Date;
import java.util.Iterator;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.TimeUnit;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.ObjectProvider;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.kuaishou.blobstore.s3.BS3Client;

/**
 * 用媒体上传同一套 BlobStore AK/SK，按 bucket + key 签发 7 天有效的 blob 预签名 URL（不走 CDN）。
 */
@Service
public class BlobStoreUrlService {

    private static final Logger LOGGER = LoggerFactory.getLogger(BlobStoreUrlService.class);

    static final long PRESIGNED_TTL_MILLIS = TimeUnit.DAYS.toMillis(7);
    /** 剩余有效期不足此时长则续签，避免列表里的封面刚打开就过期。 */
    static final long REFRESH_MARGIN_MILLIS = TimeUnit.HOURS.toMillis(12);

    private static final DateTimeFormatter AMZ_DATE =
            DateTimeFormatter.ofPattern("yyyyMMdd'T'HHmmss'Z'").withZone(ZoneId.of("UTC"));

    private final ObjectProvider<BS3Client> bs3ClientProvider;
    private final ObjectMapper objectMapper;
    private final ConcurrentHashMap<String, CachedUrl> urlCache = new ConcurrentHashMap<>();

    public BlobStoreUrlService(ObjectProvider<BS3Client> bs3ClientProvider, ObjectMapper objectMapper) {
        this.bs3ClientProvider = bs3ClientProvider;
        this.objectMapper = objectMapper;
    }

    /**
     * 按桶名 + key 签发 7 天有效的 blob 预签名 URL，不生成 CDN URL。
     * 同 bucket+key 在有效期内走进程内缓存，避免列表每次现签。
     */
    public String resolveUrl(String bucket, String key) {
        if (!StringUtils.hasText(bucket) || !StringUtils.hasText(key)) {
            return null;
        }
        String b = bucket.trim();
        String k = key.trim();
        String cacheKey = b + "\n" + k;
        CachedUrl cached = urlCache.get(cacheKey);
        if (cached != null && cached.fresh()) {
            return cached.url;
        }
        BS3Client bs3 = bs3ClientProvider.getIfAvailable();
        if (bs3 != null) {
            Date expiration = new Date(System.currentTimeMillis() + PRESIGNED_TTL_MILLIS);
            try {
                URL signed = bs3.generatePresignedUrl(b, k, expiration);
                if (signed != null && StringUtils.hasText(signed.toString())) {
                    String url = signed.toString();
                    urlCache.put(cacheKey, new CachedUrl(url, expiration.getTime()));
                    LOGGER.debug("blob presigned url bucket={} key={} ttlDays=7", b, k);
                    return url;
                }
                LOGGER.warn("BS3Client.generatePresignedUrl 返回空 bucket={} key={}", b, k);
            } catch (Exception e) {
                LOGGER.error("BS3Client.generatePresignedUrl 失败 bucket={} key={}: {}",
                        b, k, e.getMessage(), e);
            }
        } else {
            LOGGER.warn("BS3Client 未初始化，bucket={} 无法签发 7 天预签名 URL。"
                    + "请开启 agent.operation.backend.media.enabled 并配置 access-key / secret-key", b);
        }
        return null;
    }

    /**
     * 已有预签名 URL 是否还能用：优先解析 URL 过期时间，否则用 {@code signedAt + 7 天}。
     * 剩余不足 12 小时视为过期，需要续签。
     */
    public boolean stillFresh(String url, LocalDateTime signedAt) {
        if (!StringUtils.hasText(url)) {
            return false;
        }
        Long expireAt = parseExpireAtMillis(url.trim());
        long now = System.currentTimeMillis();
        if (expireAt != null) {
            return expireAt - now > REFRESH_MARGIN_MILLIS;
        }
        if (signedAt == null) {
            return false;
        }
        long signedMillis = signedAt.atZone(ZoneId.systemDefault()).toInstant().toEpochMilli();
        return signedMillis + PRESIGNED_TTL_MILLIS - now > REFRESH_MARGIN_MILLIS;
    }

    public String enrichOutputJson(String outputJson, LocalDateTime signedAt) {
        if (!StringUtils.hasText(outputJson)) {
            return outputJson;
        }
        try {
            JsonNode node = objectMapper.readTree(outputJson.trim());
            JsonNode enriched = enrichTaskOutput(node, signedAt);
            if (enriched == null || enriched.isNull()) {
                return outputJson;
            }
            return objectMapper.writeValueAsString(enriched);
        } catch (Exception e) {
            LOGGER.warn("enrich outputJson blob urls failed: {}", e.getMessage());
            return outputJson;
        }
    }

    public JsonNode enrichTaskOutput(JsonNode upstream) {
        return enrichTaskOutput(upstream, null);
    }

    public JsonNode enrichTaskOutput(JsonNode upstream, LocalDateTime signedAt) {
        if (upstream == null || upstream.isNull() || upstream.isMissingNode()) {
            return upstream;
        }
        if (!upstream.isObject() && !upstream.isArray()) {
            return upstream;
        }
        JsonNode copy = upstream.deepCopy();
        enrichNode(copy, signedAt);
        return copy;
    }

    public String enrichOutputJson(String outputJson) {
        return enrichOutputJson(outputJson, null);
    }

    /**
     * 从 output JSON 取 bucket+key 签发 URL。
     * 优先 {@code sourceName} 对应嵌套字段；studio/scraper 常见形态是 output 自身带 bucket/key。
     */
    public String resolveFromOutput(String outputJson, String sourceName, int occurrence) {
        if (!StringUtils.hasText(outputJson)) {
            return null;
        }
        try {
            JsonNode root = objectMapper.readTree(outputJson.trim());
            JsonNode output = firstPresent(root, "output", "result", "results");
            if (output == null) {
                output = root;
            }
            String field = sourceField(sourceName);
            if (StringUtils.hasText(field) && output != null && output.isObject()) {
                String nested = nthBlobUrl(output.get(field), occurrence);
                if (StringUtils.hasText(nested)) {
                    return nested;
                }
            }
            if (occurrence > 0) {
                return null;
            }
            return blobUrlOf(output);
        } catch (Exception e) {
            LOGGER.warn("resolve blob from output failed: {}", e.getMessage());
            return null;
        }
    }

    static String sourceField(String sourceName) {
        if (!StringUtils.hasText(sourceName)) {
            return null;
        }
        int idx = sourceName.lastIndexOf(':');
        if (idx < 0 || idx >= sourceName.length() - 1) {
            return null;
        }
        return sourceName.substring(idx + 1).trim();
    }

    private String nthBlobUrl(JsonNode node, int occurrence) {
        if (node == null || node.isNull() || node.isMissingNode()) {
            return null;
        }
        if (node.isObject()) {
            if (occurrence > 0) {
                return null;
            }
            return blobUrlOf(node);
        }
        if (!node.isArray() || occurrence < 0 || occurrence >= node.size()) {
            return null;
        }
        return blobUrlOf(node.get(occurrence));
    }

    private String blobUrlOf(JsonNode node) {
        if (node == null || !node.isObject()) {
            return null;
        }
        String bucket = text(node, "bucket");
        String key = text(node, "key");
        return resolveUrl(bucket, key);
    }

    private void enrichNode(JsonNode node, LocalDateTime signedAt) {
        if (node == null || node.isNull() || node.isMissingNode()) {
            return;
        }
        if (node.isArray()) {
            ArrayNode array = (ArrayNode) node;
            for (JsonNode item : array) {
                enrichNode(item, signedAt);
            }
            return;
        }
        if (!node.isObject()) {
            return;
        }
        ObjectNode object = (ObjectNode) node;
        String bucket = text(object, "bucket");
        String key = text(object, "key");
        if (StringUtils.hasText(bucket) && StringUtils.hasText(key)) {
            String existing = firstUsableUrl(object, signedAt);
            String blobUrl = StringUtils.hasText(existing) ? existing : resolveUrl(bucket.trim(), key.trim());
            if (StringUtils.hasText(blobUrl)) {
                applySingleSignedUrl(object, blobUrl);
            }
        }
        Iterator<Map.Entry<String, JsonNode>> fields = object.fields();
        while (fields.hasNext()) {
            enrichNode(fields.next().getValue(), signedAt);
        }
    }

    private String firstUsableUrl(ObjectNode object, LocalDateTime signedAt) {
        for (String field : new String[] {"videoUrl", "blobUrl", "url"}) {
            String value = text(object, field);
            if (stillFresh(value, signedAt)) {
                return value.trim();
            }
        }
        return null;
    }

    /**
     * 只保留一个可播地址：上游已有 {@code videoUrl} 就写 videoUrl，否则写 blobUrl。
     * 去掉重复的 url / blobUrl / videoUrl 三份拷贝。
     */
    private static void applySingleSignedUrl(ObjectNode object, String signedUrl) {
        boolean hasVideoUrlField = object.has("videoUrl");
        object.remove("url");
        if (hasVideoUrlField) {
            object.put("videoUrl", signedUrl);
            object.remove("blobUrl");
        } else {
            object.put("blobUrl", signedUrl);
            object.remove("videoUrl");
        }
    }

    static Long parseExpireAtMillis(String url) {
        try {
            URI uri = URI.create(url);
            String query = uri.getRawQuery();
            if (!StringUtils.hasText(query)) {
                return null;
            }
            String amzDate = null;
            String amzExpires = null;
            String expires = null;
            for (String pair : query.split("&")) {
                int eq = pair.indexOf('=');
                if (eq <= 0) {
                    continue;
                }
                String name = URLDecoder.decode(pair.substring(0, eq), StandardCharsets.UTF_8.name());
                String value = URLDecoder.decode(pair.substring(eq + 1), StandardCharsets.UTF_8.name());
                if ("X-Amz-Date".equals(name)) {
                    amzDate = value;
                } else if ("X-Amz-Expires".equals(name)) {
                    amzExpires = value;
                } else if ("Expires".equals(name)) {
                    expires = value;
                }
            }
            if (StringUtils.hasText(amzDate) && StringUtils.hasText(amzExpires)) {
                Instant start = ZonedDateTime.parse(amzDate, AMZ_DATE).toInstant();
                return start.toEpochMilli() + Long.parseLong(amzExpires) * 1000L;
            }
            if (StringUtils.hasText(expires)) {
                long value = Long.parseLong(expires);
                return value < 1_000_000_000_000L ? value * 1000L : value;
            }
        } catch (Exception ignored) {
            return null;
        }
        return null;
    }

    private static final class CachedUrl {
        private final String url;
        private final long expireAtMillis;

        private CachedUrl(String url, long expireAtMillis) {
            this.url = url;
            this.expireAtMillis = expireAtMillis;
        }

        private boolean fresh() {
            return expireAtMillis - System.currentTimeMillis() > REFRESH_MARGIN_MILLIS;
        }
    }

    private static String text(JsonNode node, String field) {
        JsonNode v = node.get(field);
        if (v == null || v.isNull() || !v.isTextual()) {
            return null;
        }
        return v.asText();
    }

    private static JsonNode firstPresent(JsonNode root, String... fields) {
        if (root == null || !root.isObject()) {
            return null;
        }
        for (String field : fields) {
            JsonNode node = root.get(field);
            if (node != null && !node.isNull() && !node.isMissingNode()) {
                return node;
            }
        }
        return null;
    }
}
