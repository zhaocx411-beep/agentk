package com.kuaishou.agent.operation.backend.agentk.tools;

import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * 比照 agent-k CoreManifest 的 pipeline 兜底名单。
 * <p>
 * {@code GET /v1/capabilities} 不可达或未返回 pipeline 声明时用它，保证离线跑数页
 * 至少能选到第一方 pipeline。schema 与 agent-k {@code internal/capability/brandad}
 * 保持一致；agent-k 侧改了入参，这里的兜底也要跟着改。
 */
final class AgentkPipelinesCatalog {

    private AgentkPipelinesCatalog() {
    }

    static List<AgentkPipelineInfo> firstPartyMock() {
        AgentkPipelineInfo brandAd = new AgentkPipelineInfo();
        brandAd.setName("generate_brand_ad");
        brandAd.setCapabilityId("agentk.generate_brand_ad");
        brandAd.setRef("pipeline:agentk.generate_brand_ad");
        brandAd.setDescription("根据一组商品图片和视频提示词生成品牌广告视频。");
        brandAd.setSource("mock");
        brandAd.setInputSchema(brandAdInputSchema());
        return Collections.singletonList(brandAd);
    }

    private static Map<String, Object> brandAdInputSchema() {
        Map<String, Object> schema = new LinkedHashMap<>();
        schema.put("type", "object");
        schema.put("additionalProperties", false);
        schema.put("required", Arrays.asList("title", "images", "video_prompt"));

        Map<String, Object> title = new LinkedHashMap<>();
        title.put("type", "string");
        title.put("minLength", 1);
        title.put("description", "广告标题或业务类型，例如“电商”");

        Map<String, Object> images = new LinkedHashMap<>();
        images.put("type", "array");
        images.put("minItems", 1);
        Map<String, Object> imageItem = new LinkedHashMap<>();
        imageItem.put("type", "string");
        imageItem.put("minLength", 1);
        images.put("items", imageItem);
        images.put("description", "商品图片 URL 列表");

        Map<String, Object> videoPrompt = new LinkedHashMap<>();
        videoPrompt.put("type", "string");
        videoPrompt.put("minLength", 1);
        videoPrompt.put("description", "视频内容、风格和营销诉求");

        Map<String, Object> properties = new LinkedHashMap<>();
        properties.put("title", title);
        properties.put("images", images);
        properties.put("video_prompt", videoPrompt);
        schema.put("properties", properties);
        return schema;
    }
}
