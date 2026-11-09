package com.kuaishou.agent.operation.backend.platform.support;

import java.util.regex.Pattern;

import org.springframework.util.StringUtils;

/**
 * 把 BlobStore IDC 内网 host（{@code *.internal}）换成办公网 host，供浏览器访问。
 * 公有读只免鉴权，不改变「.internal 只能在机房访问」。
 */
public final class BlobStoreOfficeUrls {

    private static final Pattern IDC_HOST = Pattern.compile(
            "(?i)https?://bs3-[^/\"'\\s]+\\.internal(?::\\d+)?");

    private BlobStoreOfficeUrls() {
    }

    public static String toOfficeUrl(String url, String officeHttpBase) {
        if (!StringUtils.hasText(url) || !StringUtils.hasText(officeHttpBase)) {
            return url;
        }
        String base = officeHttpBase.trim();
        while (base.endsWith("/")) {
            base = base.substring(0, base.length() - 1);
        }
        return IDC_HOST.matcher(url.trim()).replaceAll(java.util.regex.Matcher.quoteReplacement(base));
    }

    public static boolean looksLikeIdcUrl(String url) {
        return StringUtils.hasText(url) && IDC_HOST.matcher(url).find();
    }
}
