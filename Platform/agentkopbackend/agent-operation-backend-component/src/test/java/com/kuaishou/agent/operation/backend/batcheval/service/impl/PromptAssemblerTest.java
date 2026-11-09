package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.batcheval.support.LoopUserMessage;
import com.kuaishou.agent.operation.backend.batcheval.support.LoopUserMessage.Attachment;

class PromptAssemblerTest {

    private final PromptAssembler assembler = new PromptAssembler();

    @Test
    void shouldAppendFirstFrameAndOptionalAltImagesWhenModelHasNoImageInput() {
        Map<String, Object> extra = new LinkedHashMap<>();
        extra.put("商品标题", "YSL");
        String text = assembler.assemble(
                "手写要求",
                Arrays.asList("https://img/a.jpg", "https://img/b.jpg"),
                extra,
                Collections.singletonList("understand_media"));

        assertThat(text).contains("手写要求");
        assertThat(text).contains("kling_image_to_video 的 firstFrame");
        assertThat(text).contains("https://img/a.jpg");
        assertThat(text).contains("备选参考图");
        assertThat(text).contains("https://img/b.jpg");
    }

    @Test
    void shouldSkipAltImagesWithoutUnderstandMedia() {
        String text = assembler.assemble(
                "p",
                Arrays.asList("https://img/a.jpg", "https://img/b.jpg"),
                Collections.emptyMap(),
                Collections.singletonList("kling_image_to_video"));
        assertThat(text).contains("https://img/a.jpg");
        assertThat(text).doesNotContain("备选参考图");
        assertThat(text).doesNotContain("https://img/b.jpg");
    }

    @Test
    void imageCapableModelPutsReferImagesIntoAttachmentsNotPrompt() {
        LoopUserMessage message = assembler.assembleMessage(
                "看这张图",
                Arrays.asList("https://img/a.jpg", "https://img/b.png"),
                Collections.emptyMap(),
                Collections.singletonList("echo"),
                "qwen3.5-plus");

        assertThat(message.getText()).isEqualTo("看这张图");
        assertThat(message.getText()).doesNotContain("firstFrame");
        assertThat(message.getAttachments()).extracting(Attachment::getUri)
                .containsExactly("https://img/a.jpg", "https://img/b.png");
        assertThat(message.getAttachments()).extracting(Attachment::getType)
                .containsOnly("image");
        assertThat(message.getAttachments().get(0).getMimeType()).isEqualTo("image/jpeg");
        assertThat(message.getAttachments().get(1).getMimeType()).isEqualTo("image/png");
    }

    @Test
    void geminiPutsImageVideoAudioIntoAttachments() {
        Map<String, Object> extra = new LinkedHashMap<>();
        extra.put("audioUrl", "https://cdn/a.mp3");
        extra.put("videoUrl", "https://cdn/v.mp4");
        LoopUserMessage message = assembler.assembleMessage(
                "看这些附件",
                Collections.singletonList("https://img/a.jpg"),
                extra,
                Collections.emptyList(),
                "gemini-3.1-flash-lite");

        assertThat(message.getText()).isEqualTo("看这些附件");
        assertThat(message.getAttachments()).extracting(Attachment::getType)
                .containsExactly("image", "video", "audio");
        assertThat(message.getAttachments()).extracting(Attachment::getUri)
                .containsExactly("https://img/a.jpg", "https://cdn/v.mp4", "https://cdn/a.mp3");
        assertThat(message.getAttachments()).extracting(Attachment::getMimeType)
                .containsExactly("image/jpeg", "video/mp4", "audio/mpeg");
    }

    @Test
    void imageOnlyModelDoesNotAttachAudioOrVideo() {
        Map<String, Object> extra = new LinkedHashMap<>();
        extra.put("audioUrl", "https://cdn/a.mp3");
        extra.put("videoUrl", "https://cdn/v.mp4");
        LoopUserMessage message = assembler.assembleMessage(
                "p",
                Collections.singletonList("https://img/a.jpg"),
                extra,
                Collections.emptyList(),
                "gpt-5.4");

        assertThat(message.getAttachments()).extracting(Attachment::getType).containsExactly("image");
        assertThat(message.getAttachments()).extracting(Attachment::getUri)
                .containsExactly("https://img/a.jpg");
    }

    @Test
    void textOnlyModelLeavesAudioInExtraFields() {
        Map<String, Object> extra = new LinkedHashMap<>();
        extra.put("audioUrl", "https://cdn/a.mp3");
        LoopUserMessage message = assembler.assembleMessage(
                "转写这段音频",
                Collections.emptyList(),
                extra,
                Collections.singletonList("transcribe_audio"),
                "deepseek-v4-pro");

        assertThat(message.hasAttachments()).isFalse();
        assertThat(message.getText()).isEqualTo("转写这段音频");
    }
}
