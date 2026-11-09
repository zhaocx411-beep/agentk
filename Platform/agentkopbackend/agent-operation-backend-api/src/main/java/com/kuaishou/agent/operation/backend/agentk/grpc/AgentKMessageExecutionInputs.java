package com.kuaishou.agent.operation.backend.agentk.grpc;

import java.util.Locale;

import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMediaAttachment;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKPostMessageRequest;
import com.kuaishou.klingai.agentk.api.v1.AgentMessageExecutionInput;
import com.kuaishou.klingai.agentk.api.v1.MediaAttachment;

/**
 * 把平台发消息请求转成 gRPC {@link AgentMessageExecutionInput}。
 */
public final class AgentKMessageExecutionInputs {

    private static final int MAX_ATTACHMENTS = 10;

    private AgentKMessageExecutionInputs() {
    }

    public static AgentMessageExecutionInput from(AgentKPostMessageRequest message) {
        AgentMessageExecutionInput.Builder builder = AgentMessageExecutionInput.newBuilder();
        builder.setText(message == null || message.getText() == null ? "" : message.getText());
        if (message == null || message.getAttachments() == null || message.getAttachments().isEmpty()) {
            return builder.build();
        }
        int count = 0;
        for (AgentKMediaAttachment item : message.getAttachments()) {
            if (item == null) {
                continue;
            }
            if (!StringUtils.hasText(item.getUri())) {
                throw new IllegalArgumentException("attachments.uri is required");
            }
            count++;
            if (count > MAX_ATTACHMENTS) {
                throw new IllegalArgumentException("attachments may contain at most " + MAX_ATTACHMENTS + " media items");
            }
            MediaAttachment.Builder attachment = MediaAttachment.newBuilder()
                    .setUri(item.getUri().trim());
            if (StringUtils.hasText(item.getType())) {
                attachment.setType(item.getType().trim().toLowerCase(Locale.ROOT));
            }
            if (StringUtils.hasText(item.getMimeType())) {
                attachment.setMimeType(item.getMimeType().trim());
            }
            builder.addAttachments(attachment);
        }
        return builder.build();
    }
}
