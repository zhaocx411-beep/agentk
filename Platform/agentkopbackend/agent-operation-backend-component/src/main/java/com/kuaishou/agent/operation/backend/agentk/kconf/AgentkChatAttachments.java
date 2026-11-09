package com.kuaishou.agent.operation.backend.agentk.kconf;

import lombok.Data;

/**
 * 主对话 {@code attachments} 能力，对齐 agent-k {@code modelcontract}
 * 的 UserImages / UserVideos / UserAudios。
 * <p>
 * {@code mode} 给前端直接分支：
 * <ul>
 *   <li>{@code none}：不能带 attachments</li>
 *   <li>{@code image}：只能带图片</li>
 *   <li>{@code all}：图片、视频、音频都可用</li>
 * </ul>
 */
@Data
public class AgentkChatAttachments {

    public static final String MODE_NONE = "none";
    public static final String MODE_IMAGE = "image";
    public static final String MODE_ALL = "all";

    /** none / image / all */
    private String mode;

    private boolean image;
    private boolean video;
    private boolean audio;

    public static AgentkChatAttachments none() {
        return of(MODE_NONE, false, false, false);
    }

    public static AgentkChatAttachments imageOnly() {
        return of(MODE_IMAGE, true, false, false);
    }

    public static AgentkChatAttachments all() {
        return of(MODE_ALL, true, true, true);
    }

    private static AgentkChatAttachments of(String mode, boolean image, boolean video, boolean audio) {
        AgentkChatAttachments attachments = new AgentkChatAttachments();
        attachments.setMode(mode);
        attachments.setImage(image);
        attachments.setVideo(video);
        attachments.setAudio(audio);
        return attachments;
    }
}
