package com.kuaishou.agent.operation.backend.keffects.task.adapter;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.keffects.dto.KEffectsCreateTaskRequest;

class KEffectsTaskAdapterTest {

    private final ObjectMapper objectMapper = new ObjectMapper();

    @Test
    void goodsScraperRequiresUrl() throws Exception {
        GoodsScraperTaskAdapter adapter = new GoodsScraperTaskAdapter();
        KEffectsCreateTaskRequest request = request("e-commerce-goods-scraper", "{\"url\":\"\"}");

        assertThatThrownBy(() -> adapter.validateCreate(request))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("input.url is required");
    }

    @Test
    void goodsStudioValidatesRequiredContentsAndSettings() throws Exception {
        GoodsStudioTaskAdapter adapter = new GoodsStudioTaskAdapter(objectMapper);
        KEffectsCreateTaskRequest request = request("e-commerce-goods-studio",
                "{\"contents\":{"
                        + "\"ref_image\":[\"https://img.example.com/p.webp\"],"
                        + "\"goods_title\":\"夏季短袖\""
                        + "},\"settings\":{\"resolution\":\"1080p\",\"aspect_ratio\":\"16:9\",\"duration\":30}}");

        adapter.validateCreate(request);
        adapter.normalizeCreate(request);

        assertThat(request.getInput().get("contents").isArray()).isTrue();
        assertThat(request.getInput().get("contents").get(0).get("type").asText()).isEqualTo("ref_image");

        KEffectsCreateTaskRequest bad = request("e-commerce-goods-studio",
                "{\"contents\":{\"goods_title\":\"夏季短袖\"},"
                        + "\"settings\":{\"duration\":20}}");
        assertThatThrownBy(() -> adapter.validateCreate(bad))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("ref_image");
    }

    @Test
    void goodsStudioAcceptsLegacyContentsArray() throws Exception {
        GoodsStudioTaskAdapter adapter = new GoodsStudioTaskAdapter(objectMapper);
        KEffectsCreateTaskRequest request = request("e-commerce-goods-studio",
                "{\"contents\":["
                        + "{\"type\":\"ref_image\",\"url\":\"https://img.example.com/p.webp\"},"
                        + "{\"type\":\"goods_title\",\"text\":\"夏季短袖\"}"
                        + "]}");

        adapter.validateCreate(request);
        adapter.normalizeCreate(request);

        assertThat(request.getTaskType()).isEqualTo("e-commerce-goods-studio");
        assertThat(request.getInput().get("contents").isArray()).isTrue();
        assertThat(request.getInput().get("contents").get(0).get("type").asText()).isEqualTo("ref_image");
    }

    @Test
    void clothingDupeKeepsExistingValidationRules() throws Exception {
        ClothingDupeTaskAdapter adapter = new ClothingDupeTaskAdapter(objectMapper);
        KEffectsCreateTaskRequest request = request("e-commerce-clothing-dupe",
                "{\"contents\":["
                        + "{\"type\":\"source_video\",\"url\":\"https://video.example.com/a.mp4\"},"
                        + "{\"type\":\"product_image\",\"url\":\"https://img.example.com/p.webp\"}"
                        + "],\"settings\":{\"resolution\":\"720p\",\"aspectRatio\":\"9:16\"}}");

        adapter.validateCreate(request);

        KEffectsCreateTaskRequest bad = request("e-commerce-clothing-dupe",
                "{\"contents\":[{\"type\":\"product_image\",\"url\":\"https://img.example.com/p.webp\"}]}");
        assertThatThrownBy(() -> adapter.validateCreate(bad))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("source_video");
    }

    @Test
    void adaptersExposeTheirTaskTypes() {
        assertThat(new GoodsScraperTaskAdapter().taskType()).isEqualTo("e-commerce-goods-scraper");
        assertThat(new GoodsStudioTaskAdapter(objectMapper).taskType())
                .isEqualTo("e-commerce-goods-studio");
        assertThat(new ClothingDupeTaskAdapter(objectMapper).taskType()).isEqualTo("e-commerce-clothing-dupe");
    }

    @Test
    void adaptersExposeUpstreamBaseUrls() {
        assertThat(new GoodsScraperTaskAdapter().upstreamBaseUrl())
                .isEqualTo(KlingAigcEngineUpstream.TEST_BASE_URL);
        assertThat(new GoodsStudioTaskAdapter(objectMapper).upstreamBaseUrl())
                .isEqualTo(KlingAigcEngineUpstream.TEST_BASE_URL);
        assertThat(new ClothingDupeTaskAdapter(objectMapper).upstreamBaseUrl())
                .isEqualTo(KlingAigcEngineUpstream.TEST_BASE_URL);
        assertThat(KlingAigcEngineUpstream.PROD_BASE_URL)
                .isEqualTo("http://kling-aigc-engine.internal");
    }

    private KEffectsCreateTaskRequest request(String taskType, String inputJson) throws Exception {
        KEffectsCreateTaskRequest request = new KEffectsCreateTaskRequest();
        request.setTaskType(taskType);
        JsonNode input = objectMapper.readTree(inputJson);
        request.setInput(input);
        return request;
    }
}
