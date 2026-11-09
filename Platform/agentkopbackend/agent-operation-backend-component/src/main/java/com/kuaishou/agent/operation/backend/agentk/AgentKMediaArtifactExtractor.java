package com.kuaishou.agent.operation.backend.agentk;

import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMediaArtifactDto;

/**
 * 从工具、Job 或 pipeline 输出中投影出展示用媒体 URL。
 */
public final class AgentKMediaArtifactExtractor {

    private static final String IMAGE = "image";
    private static final String VIDEO = "video";
    /**
     * 从 markdown / 长文本里抠 URL。只允许 URL 合法字符，
     * 避免把 {@code ...png和提示词} 整段写进 asset_url。
     */
    private static final Pattern HTTP_URL = Pattern.compile(
            "https?://[\\w\\-._~:/?#\\[\\]@!$&'()*+,;=%]+");

    private AgentKMediaArtifactExtractor() {
    }

    public static List<AgentKMediaArtifactDto> extract(JsonNode root) {
        return filterPublishable(extractAll(root));
    }

    /** 提取全部候选 URL（含输入图等），供内部诊断；作品集同步请用 {@link #extract(JsonNode)}。 */
    public static List<AgentKMediaArtifactDto> extractAll(JsonNode root) {
        if (root == null || root.isNull() || root.isMissingNode()) {
            return Collections.emptyList();
        }
        Map<String, AgentKMediaArtifactDto> artifacts = new LinkedHashMap<>();
        collectStandardArtifacts(root, artifacts);
        collect(root, "", null, artifacts);
        return new ArrayList<>(artifacts.values());
    }

    public static boolean isPublishableType(String type) {
        if (!StringUtils.hasText(type)) {
            return false;
        }
        String normalized = type.trim().toLowerCase(Locale.ROOT);
        return IMAGE.equals(normalized) || VIDEO.equals(normalized);
    }

    public static List<AgentKMediaArtifactDto> filterPublishable(List<AgentKMediaArtifactDto> artifacts) {
        if (artifacts == null || artifacts.isEmpty()) {
            return Collections.emptyList();
        }
        List<AgentKMediaArtifactDto> filtered = new ArrayList<>();
        for (AgentKMediaArtifactDto artifact : artifacts) {
            if (artifact != null && isPublishableType(artifact.getType())
                    && StringUtils.hasText(artifact.getUrl())) {
                filtered.add(artifact);
            }
        }
        return filtered;
    }

    /** agent-k 标准 {@code artifacts[{type,uri}]} 优先投影。 */
    private static void collectStandardArtifacts(JsonNode root,
            Map<String, AgentKMediaArtifactDto> artifacts) {
        if (root == null || !root.isObject()) {
            return;
        }
        JsonNode items = root.get("artifacts");
        if (items == null || !items.isArray()) {
            return;
        }
        for (JsonNode item : items) {
            if (item == null || !item.isObject()) {
                continue;
            }
            String type = text(item, "type");
            String uri = text(item, "uri");
            if (!StringUtils.hasText(uri)) {
                uri = text(item, "url");
            }
            addIfMediaUrl(uri, type, "artifacts", artifacts);
        }
    }

    public static List<String> urlsOfType(List<AgentKMediaArtifactDto> artifacts, String type) {
        if (artifacts == null || artifacts.isEmpty() || !StringUtils.hasText(type)) {
            return Collections.emptyList();
        }
        List<String> urls = new ArrayList<>();
        for (AgentKMediaArtifactDto artifact : artifacts) {
            if (artifact != null && type.equals(artifact.getType())
                    && StringUtils.hasText(artifact.getUrl())) {
                urls.add(artifact.getUrl());
            }
        }
        return urls;
    }

    private static void collect(JsonNode node, String fieldName, String inheritedType,
            Map<String, AgentKMediaArtifactDto> artifacts) {
        if (node == null || node.isNull() || node.isMissingNode()) {
            return;
        }
        String fieldType = mediaTypeFromKey(fieldName);
        String currentType = StringUtils.hasText(fieldType) ? fieldType : inheritedType;
        if (node.isTextual()) {
            collectTextUrls(node.asText(), currentType, fieldName, artifacts);
            return;
        }
        if (node.isArray()) {
            for (JsonNode item : node) {
                collect(item, fieldName, currentType, artifacts);
            }
            return;
        }
        if (!node.isObject()) {
            return;
        }

        String objectType = mediaTypeFromObject(node);
        if (StringUtils.hasText(objectType)) {
            currentType = objectType;
        }
        final String objectMediaType = currentType;
        node.fields().forEachRemaining(entry -> {
            String childType = objectMediaType;
            String keyType = mediaTypeFromKey(entry.getKey());
            if (StringUtils.hasText(keyType)) {
                childType = keyType;
            }
            collect(entry.getValue(), entry.getKey(), childType, artifacts);
        });
    }

    private static void addIfMediaUrl(String rawUrl, String type, String source,
            Map<String, AgentKMediaArtifactDto> artifacts) {
        if (!StringUtils.hasText(rawUrl)) {
            return;
        }
        String url = sanitizeUrl(rawUrl.trim());
        if (!StringUtils.hasText(url) || !isHttpUrl(url)) {
            return;
        }
        String mediaType = StringUtils.hasText(type) ? type : mediaTypeFromUrl(url);
        if (!StringUtils.hasText(mediaType)) {
            return;
        }
        String key = mediaType + ":" + url;
        if (artifacts.containsKey(key)) {
            return;
        }
        AgentKMediaArtifactDto artifact = new AgentKMediaArtifactDto();
        artifact.setType(mediaType);
        artifact.setUrl(url);
        artifact.setSource(source);
        artifacts.put(key, artifact);
    }

    private static void collectTextUrls(String text, String type, String source,
            Map<String, AgentKMediaArtifactDto> artifacts) {
        if (!StringUtils.hasText(text)) {
            return;
        }
        // 禁止把整段 assistant.text（URL + 中文说明）当成一条 URL。
        Matcher matcher = HTTP_URL.matcher(text);
        while (matcher.find()) {
            addIfMediaUrl(matcher.group(), type, source, artifacts);
        }
    }

    /**
     * 去掉粘在 URL 后的中文/markdown/标点尾巴。
     * 例如 {@code .png和提示词}、{@code .png`}、{@code .png）}。
     * 有明确媒体后缀时，截断到后缀结束（可保留紧随的 ?query / #fragment）。
     */
    static String sanitizeUrl(String raw) {
        if (!StringUtils.hasText(raw)) {
            return "";
        }
        String url = raw.trim();
        while (url.startsWith("`") || url.startsWith("'") || url.startsWith("\"")
                || url.startsWith("<") || url.startsWith("(") || url.startsWith("[")) {
            url = url.substring(1).trim();
        }
        int cut = url.length();
        for (int i = 0; i < url.length(); i++) {
            char c = url.charAt(i);
            if (Character.UnicodeScript.of(c) == Character.UnicodeScript.HAN
                    || c == '`'
                    || c == '，' || c == '。' || c == '、' || c == '；' || c == '：'
                    || c == '！' || c == '？' || c == '（' || c == '）'
                    || c == '【' || c == '】' || c == '《' || c == '》'
                    || c == ')' || c == ']' || c == '>' || c == '"' || c == '\'') {
                cut = i;
                break;
            }
        }
        url = url.substring(0, cut);
        url = clipToMediaSuffix(url);
        while (url.length() > 0) {
            char last = url.charAt(url.length() - 1);
            if (last == '.' || last == ',' || last == ';' || last == ':' || last == '!'
                    || last == '?' || last == '(' || last == '[' || last == '{'
                    || last == '`' || last == '\'' || last == '"') {
                url = url.substring(0, url.length() - 1);
            } else {
                break;
            }
        }
        return url;
    }

    /** 有 .png/.mp4 等后缀时，丢掉后缀之后的所有垃圾字符。 */
    private static String clipToMediaSuffix(String url) {
        if (!StringUtils.hasText(url)) {
            return "";
        }
        String lower = url.toLowerCase(Locale.ROOT);
        String[] exts = {".jpeg", ".jpg", ".png", ".webp", ".gif", ".mp4", ".mov", ".webm", ".m3u8"};
        int bestEnd = -1;
        for (String ext : exts) {
            int idx = lower.lastIndexOf(ext);
            if (idx < 0) {
                continue;
            }
            int end = idx + ext.length();
            if (end < url.length()) {
                char next = url.charAt(end);
                if (next == '?' || next == '#') {
                    int j = end + 1;
                    while (j < url.length() && isUrlQueryChar(url.charAt(j))) {
                        j++;
                    }
                    end = j;
                }
            }
            if (end > bestEnd) {
                bestEnd = end;
            }
        }
        return bestEnd > 0 ? url.substring(0, bestEnd) : url;
    }

    private static boolean isUrlQueryChar(char c) {
        return Character.isLetterOrDigit(c)
                || c == '-' || c == '_' || c == '.' || c == '~'
                || c == '/' || c == '?' || c == '#' || c == '[' || c == ']'
                || c == '@' || c == '!' || c == '$' || c == '&' || c == '\''
                || c == '(' || c == ')' || c == '*' || c == '+' || c == ','
                || c == ';' || c == '=' || c == '%' || c == ':';
    }

    private static boolean isHttpUrl(String value) {
        String lower = value.toLowerCase(Locale.ROOT);
        return lower.startsWith("http://") || lower.startsWith("https://");
    }

    private static String mediaTypeFromKey(String fieldName) {
        if (!StringUtils.hasText(fieldName)) {
            return null;
        }
        String key = fieldName.toLowerCase(Locale.ROOT).replace("_", "");
        if (key.contains("image") || key.contains("img") || key.contains("picture")
                || key.contains("cover")) {
            return IMAGE;
        }
        if (key.contains("video") || key.contains("mp4")) {
            return VIDEO;
        }
        return null;
    }

    private static String mediaTypeFromObject(JsonNode node) {
        String type = text(node, "type");
        if (!StringUtils.hasText(type)) {
            type = text(node, "mediaType");
        }
        if (!StringUtils.hasText(type)) {
            type = text(node, "mimeType");
        }
        String lower = type.toLowerCase(Locale.ROOT);
        if (lower.contains(IMAGE)) {
            return IMAGE;
        }
        if (lower.contains(VIDEO)) {
            return VIDEO;
        }
        return null;
    }

    private static String mediaTypeFromUrl(String url) {
        String lower = url.toLowerCase(Locale.ROOT);
        if (lower.contains(".png") || lower.contains(".jpg") || lower.contains(".jpeg")
                || lower.contains(".webp") || lower.contains(".gif")) {
            return IMAGE;
        }
        if (lower.contains(".mp4") || lower.contains(".mov") || lower.contains(".webm")
                || lower.contains(".m3u8")) {
            return VIDEO;
        }
        return null;
    }

    private static String text(JsonNode node, String field) {
        JsonNode value = node.get(field);
        return value != null && value.isTextual() ? value.asText() : "";
    }
}
