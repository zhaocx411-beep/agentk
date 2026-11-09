package com.kuaishou.agent.operation.backend.common.web;

import java.io.UnsupportedEncodingException;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;

/**
 * 下载响应头。
 * <p>
 * HTTP 头按 ISO-8859-1 传输，中文一旦以原始 UTF-8 字节写进去，客户端会按 Latin-1
 * 解出「è¯­éŸ³」这类乱码。因此头里只出现百分号编码后的 ASCII：
 * {@code filename=} 与 {@code filename*=} 都用 UTF-8 percent-encoding。
 * <p>
 * 另配合 {@code File-Name} 头，前端走 blob 下载时可直接 {@code decodeURIComponent} 取名。
 */
public final class ContentDispositions {

    public static final String HEADER_FILE_NAME = "File-Name";

    private ContentDispositions() {
    }

    /** {@code attachment; filename="<编码>"; filename*=UTF-8''<编码>} */
    public static String attachment(String filename, String fallback) {
        String encoded = encode(filename, fallback);
        return "attachment; filename=\"" + encoded + "\"; filename*=UTF-8''" + encoded;
    }

    /** {@code File-Name} 头的值，与 Content-Disposition 内的编码一致。 */
    public static String fileNameHeader(String filename, String fallback) {
        return encode(filename, fallback);
    }

    private static String encode(String filename, String fallback) {
        String safeFallback = fallback == null || fallback.trim().isEmpty() ? "download.bin" : fallback.trim();
        return urlEncode(sanitize(filename, safeFallback));
    }

    private static String sanitize(String filename, String fallback) {
        if (filename == null || filename.trim().isEmpty()) {
            return fallback;
        }
        String cleaned = filename.trim()
                .replace('\\', '_')
                .replace('/', '_')
                .replace('"', '_')
                .replace("\r", "")
                .replace("\n", "");
        return cleaned.isEmpty() ? fallback : cleaned;
    }

    /** URLEncoder 会把空格编成 {@code +}，文件名场景需还原为 {@code %20}。 */
    private static String urlEncode(String value) {
        try {
            return URLEncoder.encode(value, StandardCharsets.UTF_8.name()).replace("+", "%20");
        } catch (UnsupportedEncodingException e) {
            throw new IllegalStateException("UTF-8 not supported", e);
        }
    }
}
