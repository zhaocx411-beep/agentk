package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import java.util.ArrayList;
import java.util.Collection;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;

import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkChatAttachments;
import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkChatModelCatalog;
import com.kuaishou.agent.operation.backend.batcheval.support.LoopUserMessage;
import com.kuaishou.agent.operation.backend.batcheval.support.LoopUserMessage.Attachment;

/**
 * 按主模型能力组装发给 agent-k 的 user 消息。
 * <p>
 * 主模型能收的媒体进 {@code attachments}，不再把 URL 拼进 prompt。
 * 主模型不能收图时，仍把首帧 URL 写入文本，供 {@code kling_image_to_video} 等工具转填。
 */
@Component
public class PromptAssembler {

    private static final String UNDERSTAND_MEDIA = "understand_media";
    /** 对齐 agent-k 单轮附件上限。 */
    static final int MAX_ATTACHMENTS = 10;
    static final int MAX_VIDEOS = 1;

    private static final String[] VIDEO_KEYS = {
            "videoUrl", "videoUrls", "referVideo", "referVideos"};
    private static final String[] AUDIO_KEYS = {
            "audioUrl", "audioUrls", "referAudio", "referAudios"};

    public String assemble(String prompt, List<String> referImages, Map<String, Object> extraFields,
            Collection<String> enabledTools) {
        return assembleMessage(prompt, referImages, extraFields, enabledTools, null).getText();
    }

    public LoopUserMessage assembleMessage(String prompt, List<String> referImages,
            Map<String, Object> extraFields, Collection<String> enabledTools, String defaultModel) {
        AgentkChatAttachments caps = AgentkChatModelCatalog.attachmentsOf(defaultModel);
        String base = StringUtils.hasText(prompt) ? prompt.trim() : buildFromExtraFields(extraFields);
        List<String> images = referImages == null ? new ArrayList<>() : referImages;

        List<Attachment> attachments = new ArrayList<>();
        LinkedHashSet<String> seen = new LinkedHashSet<>();
        if (caps.isImage()) {
            for (String url : images) {
                addAttachment(attachments, seen, "image", url);
            }
        }
        if (caps.isVideo()) {
            for (String url : extraFieldUrls(extraFields, VIDEO_KEYS)) {
                addAttachment(attachments, seen, "video", url);
            }
        }
        if (caps.isAudio()) {
            for (String url : extraFieldUrls(extraFields, AUDIO_KEYS)) {
                addAttachment(attachments, seen, "audio", url);
            }
        }

        String text = caps.isImage() ? nullToEmpty(base) : appendImageUrls(base, images, enabledTools);
        return new LoopUserMessage(text, attachments);
    }

    private static String appendImageUrls(String base, List<String> images, Collection<String> enabledTools) {
        if (images.isEmpty()) {
            return nullToEmpty(base);
        }
        StringBuilder sb = new StringBuilder();
        if (StringUtils.hasText(base)) {
            sb.append(base.trim());
        }
        String first = images.get(0);
        if (StringUtils.hasText(first)) {
            if (sb.length() > 0) {
                sb.append("\n\n");
            }
            sb.append("首帧图(必须将此 URL 原样作为 kling_image_to_video 的 firstFrame 参数;")
                    .append("视频画幅比例由该图决定):\n")
                    .append(first.trim());
        }

        boolean includeAlts = enabledTools != null && enabledTools.contains(UNDERSTAND_MEDIA);
        if (includeAlts && images.size() > 1) {
            sb.append("\n\n备选参考图(可用 understand_media 查看以理解商品外观):");
            for (int i = 1; i < images.size(); i++) {
                String url = images.get(i);
                if (StringUtils.hasText(url)) {
                    sb.append("\n- ").append(url.trim());
                }
            }
        }
        return sb.toString().trim();
    }

    private static void addAttachment(List<Attachment> attachments, LinkedHashSet<String> seen,
            String type, String url) {
        if (!StringUtils.hasText(url) || attachments.size() >= MAX_ATTACHMENTS) {
            return;
        }
        String uri = url.trim();
        if (!seen.add(uri)) {
            return;
        }
        if ("video".equals(type) && videoCount(attachments) >= MAX_VIDEOS) {
            return;
        }
        String mime = guessMime(uri, type);
        if (("video".equals(type) || "audio".equals(type)) && !StringUtils.hasText(mime)) {
            return;
        }
        attachments.add(new Attachment(type, uri, mime));
    }

    private static int videoCount(List<Attachment> attachments) {
        int count = 0;
        for (Attachment attachment : attachments) {
            if ("video".equals(attachment.getType())) {
                count++;
            }
        }
        return count;
    }

    static List<String> extraFieldUrls(Map<String, Object> extraFields, String[] keys) {
        List<String> urls = new ArrayList<>();
        if (extraFields == null || extraFields.isEmpty()) {
            return urls;
        }
        LinkedHashSet<String> seen = new LinkedHashSet<>();
        for (String key : keys) {
            collectUrls(extraFields.get(key), urls, seen);
        }
        return urls;
    }

    private static void collectUrls(Object value, List<String> urls, LinkedHashSet<String> seen) {
        if (value == null) {
            return;
        }
        if (value instanceof Collection) {
            for (Object item : (Collection<?>) value) {
                collectUrls(item, urls, seen);
            }
            return;
        }
        String text = String.valueOf(value).trim();
        if (text.isEmpty() || "null".equals(text) || !seen.add(text)) {
            return;
        }
        urls.add(text);
    }

    static String guessMime(String uri, String type) {
        String ext = extension(uri);
        if ("image".equals(type)) {
            if ("jpg".equals(ext) || "jpeg".equals(ext)) {
                return "image/jpeg";
            }
            if ("png".equals(ext)) {
                return "image/png";
            }
            if ("webp".equals(ext)) {
                return "image/webp";
            }
            return null;
        }
        if ("video".equals(type)) {
            if ("mp4".equals(ext) || "m4v".equals(ext)) {
                return "video/mp4";
            }
            if ("webm".equals(ext)) {
                return "video/webm";
            }
            if ("mov".equals(ext)) {
                return "video/quicktime";
            }
            return null;
        }
        if ("audio".equals(type)) {
            if ("mp3".equals(ext) || "mpeg".equals(ext)) {
                return "audio/mpeg";
            }
            if ("wav".equals(ext)) {
                return "audio/wav";
            }
            if ("m4a".equals(ext)) {
                return "audio/m4a";
            }
            if ("aac".equals(ext)) {
                return "audio/aac";
            }
            if ("ogg".equals(ext) || "oga".equals(ext)) {
                return "audio/ogg";
            }
            if ("flac".equals(ext)) {
                return "audio/flac";
            }
            if ("mp4".equals(ext)) {
                return "audio/m4a";
            }
            return null;
        }
        return null;
    }

    private static String extension(String uri) {
        if (!StringUtils.hasText(uri)) {
            return "";
        }
        String path = uri.trim();
        int query = path.indexOf('?');
        if (query >= 0) {
            path = path.substring(0, query);
        }
        int hash = path.indexOf('#');
        if (hash >= 0) {
            path = path.substring(0, hash);
        }
        int slash = path.lastIndexOf('/');
        String name = slash >= 0 ? path.substring(slash + 1) : path;
        int dot = name.lastIndexOf('.');
        if (dot < 0 || dot == name.length() - 1) {
            return "";
        }
        return name.substring(dot + 1).toLowerCase(Locale.ROOT);
    }

    private static String buildFromExtraFields(Map<String, Object> extraFields) {
        if (extraFields == null || extraFields.isEmpty()) {
            return "";
        }
        Map<String, Object> fields = new LinkedHashMap<>(extraFields);
        StringBuilder sb = new StringBuilder();
        append(sb, "商品标题", fields.get("商品标题"));
        append(sb, "商品价格", fields.get("商品价格"));
        append(sb, "商品属性", fields.get("商品属性信息"));
        if (fields.get("商品属性") != null && fields.get("商品属性信息") == null) {
            append(sb, "商品属性", fields.get("商品属性"));
        }
        return sb.toString().trim();
    }

    private static void append(StringBuilder sb, String label, Object value) {
        if (value == null) {
            return;
        }
        String text = String.valueOf(value).trim();
        if (text.isEmpty()) {
            return;
        }
        sb.append(label).append(':').append(text).append('\n');
    }

    private static String nullToEmpty(String value) {
        return value == null ? "" : value;
    }
}
