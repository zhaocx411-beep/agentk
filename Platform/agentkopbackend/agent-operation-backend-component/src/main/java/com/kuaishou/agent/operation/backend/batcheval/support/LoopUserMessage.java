package com.kuaishou.agent.operation.backend.batcheval.support;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * 离线 Agent Loop 发给 agent-k 的 user 消息：{@code text} + {@code attachments}。
 */
public final class LoopUserMessage {

    private final String text;
    private final List<Attachment> attachments;

    public LoopUserMessage(String text, List<Attachment> attachments) {
        this.text = text == null ? "" : text;
        this.attachments = attachments == null || attachments.isEmpty()
                ? Collections.emptyList()
                : Collections.unmodifiableList(new ArrayList<>(attachments));
    }

    public String getText() {
        return text;
    }

    public List<Attachment> getAttachments() {
        return attachments;
    }

    public boolean hasAttachments() {
        return !attachments.isEmpty();
    }

    public static final class Attachment {
        private final String type;
        private final String uri;
        private final String mimeType;

        public Attachment(String type, String uri, String mimeType) {
            this.type = type;
            this.uri = uri;
            this.mimeType = mimeType;
        }

        public String getType() {
            return type;
        }

        public String getUri() {
            return uri;
        }

        public String getMimeType() {
            return mimeType;
        }
    }
}
