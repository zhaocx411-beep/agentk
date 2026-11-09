package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.agentk.tools.AgentkPipelineInfo;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;

class PipelineArgsResolverTest {

    private final PipelineArgsResolver resolver = new PipelineArgsResolver();

    @Test
    void shouldPreferExplicitPipelineArgsBucket() {
        DatasetTask task = task();
        task.setPrompt("ignored");
        task.setReferImages(Collections.singletonList("https://img/ignored.jpg"));
        Map<String, Object> explicit = new LinkedHashMap<>();
        explicit.put("title", "显式标题");
        explicit.put("images", Collections.singletonList("https://img/explicit.jpg"));
        explicit.put("video_prompt", "显式提示");
        Map<String, Object> bucket = new LinkedHashMap<>();
        bucket.put("generate_brand_ad", explicit);
        Map<String, Object> extra = new LinkedHashMap<>();
        extra.put("pipelineArgs", bucket);
        extra.put("商品标题", "不应使用");
        task.setExtraFields(extra);

        Map<String, Object> args = resolver.resolve(task, brandAdPipeline());
        assertThat(args).containsEntry("title", "显式标题")
                .containsEntry("video_prompt", "显式提示");
        assertThat(args.get("images")).isEqualTo(Collections.singletonList("https://img/explicit.jpg"));
    }

    @Test
    void shouldMapByConventionWhenPipelineArgsMissing() {
        DatasetTask task = task();
        task.setPrompt("做一个广告");
        task.setReferImages(Arrays.asList("https://img/a.jpg", "https://img/b.jpg"));
        Map<String, Object> extra = new LinkedHashMap<>();
        extra.put("商品标题", "YSL");
        task.setExtraFields(extra);

        Map<String, Object> args = resolver.resolve(task, brandAdPipeline());
        assertThat(args).containsEntry("title", "YSL")
                .containsEntry("video_prompt", "做一个广告");
        assertThat(args.get("images")).isEqualTo(Arrays.asList("https://img/a.jpg", "https://img/b.jpg"));
    }

    @Test
    void shouldRejectMissingRequiredFields() {
        DatasetTask task = task();
        task.setPrompt("只有提示");
        task.setExtraFields(new LinkedHashMap<>());

        assertThatThrownBy(() -> resolver.resolve(task, brandAdPipeline()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("缺少必填入参")
                .hasMessageContaining("title")
                .hasMessageContaining("images");
    }

    @Test
    void shouldNotGuessWhenSchemaUnknownAndNoExplicitArgs() {
        AgentkPipelineInfo pipeline = new AgentkPipelineInfo();
        pipeline.setName("custom_pipe");
        pipeline.setInputSchema(Collections.singletonMap("required",
                Collections.singletonList("foo")));

        DatasetTask task = task();
        task.setPrompt("x");
        task.setReferImages(Collections.singletonList("https://img/a.jpg"));

        assertThatThrownBy(() -> resolver.resolve(task, pipeline))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("foo");
    }

    private static DatasetTask task() {
        DatasetTask task = new DatasetTask();
        task.setTaskKey("t1");
        return task;
    }

    private static AgentkPipelineInfo brandAdPipeline() {
        AgentkPipelineInfo info = new AgentkPipelineInfo();
        info.setName("generate_brand_ad");
        Map<String, Object> schema = new LinkedHashMap<>();
        schema.put("type", "object");
        schema.put("required", Arrays.asList("title", "images", "video_prompt"));
        Map<String, Object> properties = new LinkedHashMap<>();
        properties.put("title", Collections.singletonMap("type", "string"));
        properties.put("images", Collections.singletonMap("type", "array"));
        properties.put("video_prompt", Collections.singletonMap("type", "string"));
        schema.put("properties", properties);
        info.setInputSchema(schema);
        return info;
    }
}
