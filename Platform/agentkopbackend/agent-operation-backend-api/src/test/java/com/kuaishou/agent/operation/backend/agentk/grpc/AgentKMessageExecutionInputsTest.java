package com.kuaishou.agent.operation.backend.agentk.grpc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMediaAttachment;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKPostMessageRequest;
import com.kuaishou.klingai.agentk.api.v1.AgentMessageExecutionInput;
import com.kuaishou.klingai.agentk.api.v1.MediaAttachment;

class AgentKMessageExecutionInputsTest {

    @Test
    void shouldMapTextAndAttachments() {
        AgentKMediaAttachment image = new AgentKMediaAttachment();
        image.setType("IMAGE");
        image.setUri(" https://cdn/a.png ");
        image.setMimeType("image/png");
        AgentKMediaAttachment video = new AgentKMediaAttachment();
        video.setType("video");
        video.setUri("https://cdn/a.mp4");
        video.setMimeType("video/mp4");

        AgentKPostMessageRequest request = new AgentKPostMessageRequest();
        request.setText("看看这些素材");
        request.setAttachments(java.util.Arrays.asList(image, video));

        AgentMessageExecutionInput input = AgentKMessageExecutionInputs.from(request);
        assertThat(input.getText()).isEqualTo("看看这些素材");
        assertThat(input.getAttachmentsList()).hasSize(2);
        MediaAttachment first = input.getAttachments(0);
        assertThat(first.getType()).isEqualTo("image");
        assertThat(first.getUri()).isEqualTo("https://cdn/a.png");
        assertThat(first.getMimeType()).isEqualTo("image/png");
        assertThat(input.getAttachments(1).getType()).isEqualTo("video");
        assertThat(input.getAttachments(1).getMimeType()).isEqualTo("video/mp4");
    }

    @Test
    void shouldAllowTextOnly() {
        AgentKPostMessageRequest request = new AgentKPostMessageRequest();
        request.setText("hello");
        AgentMessageExecutionInput input = AgentKMessageExecutionInputs.from(request);
        assertThat(input.getText()).isEqualTo("hello");
        assertThat(input.getAttachmentsList()).isEmpty();
    }

    @Test
    void shouldRejectBlankUri() {
        AgentKMediaAttachment item = new AgentKMediaAttachment();
        item.setType("image");
        item.setUri("  ");
        AgentKPostMessageRequest request = new AgentKPostMessageRequest();
        request.setAttachments(Collections.singletonList(item));
        assertThatThrownBy(() -> AgentKMessageExecutionInputs.from(request))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("attachments.uri");
    }

    @Test
    void shouldRejectMoreThanTenAttachments() {
        List<AgentKMediaAttachment> items = new ArrayList<>();
        for (int i = 0; i < 11; i++) {
            AgentKMediaAttachment item = new AgentKMediaAttachment();
            item.setType("image");
            item.setUri("https://cdn/" + i + ".png");
            items.add(item);
        }
        AgentKPostMessageRequest request = new AgentKPostMessageRequest();
        request.setAttachments(items);
        assertThatThrownBy(() -> AgentKMessageExecutionInputs.from(request))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("at most 10");
    }
}
