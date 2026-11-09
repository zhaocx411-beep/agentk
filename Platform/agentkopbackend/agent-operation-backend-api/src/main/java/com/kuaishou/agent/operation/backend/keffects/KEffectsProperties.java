package com.kuaishou.agent.operation.backend.keffects;

import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * KEffects 出站配置：创建 / 查询 / 取消共用同一 baseUrl。
 */
@ConfigurationProperties(prefix = "agent.operation.backend.keffects")
public class KEffectsProperties {

    /**
     * 未匹配到 adapter 时的兜底 HTTP 根地址，不含 /v1。
     * 各 taskType 实际上游域名由对应 {@link com.kuaishou.agent.operation.backend.keffects.task.KEffectsTaskAdapter}
     * 硬编码。
     */
    private String baseUrl = "https://kling-aigc-engine.test.gifshow.com";

    /**
     * 产物桶（如 upload-ylab-stunt）签发 blobUrl 使用的线上 BlobStore HTTP endpoint。
     * 形如 {@code http://bs3-hb1.internal/{bucket}/{key}}。
     */
    private String blobHttpBase = "http://bs3-hb1.internal";

    /**
     * 使用 {@link #blobHttpBase} 拼 URL 的桶名；默认仅 upload-ylab-stunt。
     */
    private java.util.List<String> blobHttpBuckets = java.util.Collections.singletonList("upload-ylab-stunt");

    private int connectTimeoutMs = 3000;

    private int readTimeoutMs = 120_000;

    public String getBaseUrl() {
        return baseUrl;
    }

    public void setBaseUrl(String baseUrl) {
        this.baseUrl = baseUrl;
    }

    public String getBlobHttpBase() {
        return blobHttpBase;
    }

    public void setBlobHttpBase(String blobHttpBase) {
        this.blobHttpBase = blobHttpBase;
    }

    public java.util.List<String> getBlobHttpBuckets() {
        return blobHttpBuckets;
    }

    public void setBlobHttpBuckets(java.util.List<String> blobHttpBuckets) {
        this.blobHttpBuckets = blobHttpBuckets;
    }

    public int getConnectTimeoutMs() {
        return connectTimeoutMs;
    }

    public void setConnectTimeoutMs(int connectTimeoutMs) {
        this.connectTimeoutMs = connectTimeoutMs;
    }

    public int getReadTimeoutMs() {
        return readTimeoutMs;
    }

    public void setReadTimeoutMs(int readTimeoutMs) {
        this.readTimeoutMs = readTimeoutMs;
    }
}
