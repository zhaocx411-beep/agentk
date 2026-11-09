package com.kuaishou.agent.operation.backend.platform.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import java.net.URL;
import java.util.Date;

import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.ObjectProvider;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.blobstore.s3.BS3Client;

class BlobStoreUrlServiceTest {

    private final ObjectMapper objectMapper = new ObjectMapper();

    @Test
    void enrichFillsEmptyVideoUrlFromFlatOutputBucketAndKey() throws Exception {
        BlobStoreUrlService service = serviceWithPresign(
                "upload-ylab-stunt",
                "brandstudio/finals/egs-baae24a9d8c8.mp4",
                "https://blob.example.com/signed/egs.mp4?X-Amz-Expires=604800");

        JsonNode enriched = service.enrichTaskOutput(objectMapper.readTree(
                "{\"taskId\":\"egs-baae24a9d8c8\",\"taskType\":\"e-commerce-goods-studio\","
                        + "\"output\":{\"videoUrl\":\"\",\"bucket\":\"upload-ylab-stunt\","
                        + "\"key\":\"brandstudio/finals/egs-baae24a9d8c8.mp4\"}}"));

        JsonNode output = enriched.path("output");
        assertThat(output.path("videoUrl").asText())
                .isEqualTo("https://blob.example.com/signed/egs.mp4?X-Amz-Expires=604800");
        assertThat(output.has("blobUrl")).isFalse();
        assertThat(output.has("url")).isFalse();
        assertThat(output.path("bucket").asText()).isEqualTo("upload-ylab-stunt");
        assertThat(output.path("key").asText()).isEqualTo("brandstudio/finals/egs-baae24a9d8c8.mp4");
    }

    @Test
    void enrichOutputJsonResignsHistoryFlatOutput() throws Exception {
        BlobStoreUrlService service = serviceWithPresign(
                "upload-ylab-stunt",
                "brandstudio/finals/egs-baae24a9d8c8.mp4",
                "https://blob.example.com/signed/history.mp4");

        String json = service.enrichOutputJson(
                "{\"videoUrl\":\"\",\"bucket\":\"upload-ylab-stunt\","
                        + "\"key\":\"brandstudio/finals/egs-baae24a9d8c8.mp4\"}");

        JsonNode node = objectMapper.readTree(json);
        assertThat(node.path("videoUrl").asText()).isEqualTo("https://blob.example.com/signed/history.mp4");
        assertThat(node.has("blobUrl")).isFalse();
        assertThat(node.has("url")).isFalse();
    }

    @Test
    void resolveFromOutputReadsFlatBucketKeyWhenFieldMissing() throws Exception {
        BlobStoreUrlService service = serviceWithPresign(
                "upload-ylab-stunt", "out.mp4", "https://blob.example.com/out.mp4");

        String url = service.resolveFromOutput(
                "{\"videoUrl\":\"\",\"bucket\":\"upload-ylab-stunt\",\"key\":\"out.mp4\"}",
                "e-commerce-goods-studio:productVideo",
                0);

        assertThat(url).isEqualTo("https://blob.example.com/out.mp4");
    }

    @Test
    void stillFreshParsesAmzExpires() {
        BlobStoreUrlService service = new BlobStoreUrlService(mockProvider(), objectMapper);
        long start = System.currentTimeMillis() - 60_000L;
        String date = java.time.format.DateTimeFormatter.ofPattern("yyyyMMdd'T'HHmmss'Z'")
                .withZone(java.time.ZoneOffset.UTC)
                .format(java.time.Instant.ofEpochMilli(start));
        String url = "https://blob.example.com/a.mp4?X-Amz-Date=" + date + "&X-Amz-Expires=604800";
        assertThat(service.stillFresh(url, null)).isTrue();
        assertThat(service.stillFresh("", java.time.LocalDateTime.now())).isFalse();
        assertThat(service.stillFresh("https://blob.example.com/a.mp4",
                java.time.LocalDateTime.now().minusDays(8))).isFalse();
        assertThat(service.stillFresh("https://blob.example.com/a.mp4",
                java.time.LocalDateTime.now().minusHours(1))).isTrue();
    }

    @SuppressWarnings("unchecked")
    private ObjectProvider<BS3Client> mockProvider() {
        ObjectProvider<BS3Client> provider = mock(ObjectProvider.class);
        when(provider.getIfAvailable()).thenReturn(null);
        return provider;
    }

    private BlobStoreUrlService serviceWithPresign(String bucket, String key, String url) throws Exception {
        @SuppressWarnings("unchecked")
        ObjectProvider<BS3Client> provider = mock(ObjectProvider.class);
        BS3Client bs3 = mock(BS3Client.class);
        when(provider.getIfAvailable()).thenReturn(bs3);
        when(bs3.generatePresignedUrl(eq(bucket), eq(key), any(Date.class)))
                .thenReturn(new URL(url));
        return new BlobStoreUrlService(provider, objectMapper);
    }
}
