package com.kuaishou.agent.operation.backend.agentk.tryon;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.Arrays;
import java.util.Collections;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMediaAttachment;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKPostMessageRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKTtsHint;
import com.kuaishou.agent.operation.backend.agentk.dto.VirtualTryOnUserInput;
import com.kuaishou.agent.operation.backend.agentk.tts.AgentKTtsMessageComposer;

class VirtualTryOnUserInputComposerTest {

    @Test
    void withoutImageFieldsKeepsOriginalRequest() {
        AgentKPostMessageRequest request = new AgentKPostMessageRequest();
        request.setText("随便聊一下");
        assertThat(VirtualTryOnUserInputComposer.hasStructuredInput(request)).isFalse();
        assertThat(VirtualTryOnUserInputComposer.apply(request)).isSameAs(request);
    }

    @Test
    void composePrefersNestedUserInput() {
        AgentKPostMessageRequest request = new AgentKPostMessageRequest();
        request.setText("顶层 text 应被 notes 覆盖");
        request.setProductImageUrls(Collections.singletonList("https://cdn/ignore.png"));
        VirtualTryOnUserInput input = new VirtualTryOnUserInput();
        input.setProductImageUrls(Arrays.asList(" https://cdn/product.png ", ""));
        input.setModelImageUrl("https://cdn/model.png");
        input.setNotes("年轻女性，棚拍，全身照，自然站姿，9:16");
        input.setOutputType("image");
        request.setUserInput(input);

        String text = VirtualTryOnUserInputComposer.compose(request);
        assertThat(text).contains("请执行虚拟试穿。");
        assertThat(text).contains("商品图 productImageUrls:\n1. https://cdn/product.png");
        assertThat(text).doesNotContain("https://cdn/ignore.png");
        assertThat(text).contains("模特图 modelImageUrl:\nhttps://cdn/model.png");
        assertThat(text).contains("输出类型 outputType:\nimage");
        assertThat(text).contains("用户补充要求:\n年轻女性，棚拍，全身照，自然站姿，9:16");
        assertThat(text).contains("如果没有模特图，则先生成一个自然模特图");
    }

    @Test
    void composeWithoutModelUsesPlaceholder() {
        AgentKPostMessageRequest request = new AgentKPostMessageRequest();
        request.setProductImageUrls(Collections.singletonList("https://cdn/product.png"));

        String text = VirtualTryOnUserInputComposer.compose(request);
        assertThat(text).contains("模特图 modelImageUrl:\n（无）");
        assertThat(text).contains("用户补充要求:\n（无）");
    }

    @Test
    void ttsPrefixIsAppliedAfterStructuredWrap() {
        AgentKPostMessageRequest request = new AgentKPostMessageRequest();
        request.setProductImageUrls(Collections.singletonList("https://cdn/product.png"));
        request.setText("做成展示视频");
        AgentKTtsHint hint = new AgentKTtsHint();
        hint.setEngine("qwen");
        hint.setVoiceId("Cherry");
        request.setTts(hint);

        String outbound = AgentKTtsMessageComposer.compose(VirtualTryOnUserInputComposer.apply(request));
        assertThat(outbound).startsWith("【TTS】工具=generate_speech_qwen");
        assertThat(outbound).contains("请执行虚拟试穿。");
        assertThat(outbound).contains("https://cdn/product.png");
    }

    @Test
    void applyKeepsAttachmentsWhenWrappingTryOn() {
        AgentKPostMessageRequest request = new AgentKPostMessageRequest();
        request.setProductImageUrls(Collections.singletonList("https://cdn/product.png"));
        AgentKMediaAttachment attachment = new AgentKMediaAttachment();
        attachment.setType("image");
        attachment.setUri("https://cdn/product.png");
        request.setAttachments(Collections.singletonList(attachment));

        AgentKPostMessageRequest outbound = VirtualTryOnUserInputComposer.apply(request);
        assertThat(outbound.getAttachments()).hasSize(1);
        assertThat(outbound.getAttachments().get(0).getUri()).isEqualTo("https://cdn/product.png");
    }
}
