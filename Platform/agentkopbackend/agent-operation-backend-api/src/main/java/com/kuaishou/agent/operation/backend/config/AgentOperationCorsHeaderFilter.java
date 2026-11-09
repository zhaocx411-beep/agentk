package com.kuaishou.agent.operation.backend.config;

import java.io.IOException;
import java.util.LinkedHashSet;
import java.util.Locale;
import java.util.Set;

import javax.servlet.Filter;
import javax.servlet.FilterChain;
import javax.servlet.ServletException;
import javax.servlet.ServletRequest;
import javax.servlet.ServletResponse;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

/**
 * Servlet 最早层兜底 CORS：对白名单 Origin 写 ACAO，OPTIONS 预检直接结束。
 */
class AgentOperationCorsHeaderFilter implements Filter {

    private static final String HDR_ORIGIN = "Origin";
    private static final String HDR_AC_ORIGIN = "Access-Control-Allow-Origin";
    private static final String HDR_AC_CREDENTIALS = "Access-Control-Allow-Credentials";
    private static final String HDR_AC_METHODS = "Access-Control-Allow-Methods";
    private static final String HDR_AC_HEADERS = "Access-Control-Allow-Headers";
    private static final String HDR_AC_EXPOSE = "Access-Control-Expose-Headers";
    private static final String HDR_AC_MAX_AGE = "Access-Control-Max-Age";
    private static final String HDR_ACRH = "Access-Control-Request-Headers";
    private static final String VARY = "Vary";

    private static final String[] PREFLIGHT_BASE_HEADERS_LOWER = {
            "authorization", "content-type", "x-requested-with", "x-request-id",
            "accept", "origin", "x-ksap-authn-token", "x-referer-chain"
    };

    static String mergeAccessControlAllowHeaders(String accessControlRequestHeaders) {
        Set<String> set = new LinkedHashSet<>();
        for (String base : PREFLIGHT_BASE_HEADERS_LOWER) {
            set.add(base);
        }
        if (accessControlRequestHeaders != null) {
            for (String raw : accessControlRequestHeaders.split(",")) {
                String trimmed = raw.trim();
                if (!trimmed.isEmpty()) {
                    set.add(trimmed.toLowerCase(Locale.ROOT));
                }
            }
        }
        return String.join(", ", set);
    }

    @Override
    public void doFilter(ServletRequest request, ServletResponse response, FilterChain chain)
            throws IOException, ServletException {
        if (!(request instanceof HttpServletRequest) || !(response instanceof HttpServletResponse)) {
            chain.doFilter(request, response);
            return;
        }

        HttpServletRequest req = (HttpServletRequest) request;
        HttpServletResponse resp = (HttpServletResponse) response;
        String rawOrigin = req.getHeader(HDR_ORIGIN);
        boolean allow = rawOrigin != null && AgentOperationCorsAllowedOrigins.isAllowed(rawOrigin);
        String origin = allow ? rawOrigin.trim() : null;

        if (allow && origin != null) {
            resp.setHeader(HDR_AC_ORIGIN, origin);
            resp.setHeader(HDR_AC_CREDENTIALS, "true");
            resp.setHeader(HDR_AC_EXPOSE, "Content-Disposition, File-Name, Content-Type");
            resp.addHeader(VARY, HDR_ORIGIN);
        }

        if ("OPTIONS".equalsIgnoreCase(req.getMethod()) && allow) {
            resp.setHeader(HDR_AC_METHODS, "GET,POST,PUT,PATCH,DELETE,OPTIONS");
            resp.setHeader(HDR_AC_HEADERS, mergeAccessControlAllowHeaders(req.getHeader(HDR_ACRH)));
            resp.setHeader(HDR_AC_MAX_AGE, "3600");
            resp.setStatus(HttpServletResponse.SC_OK);
            resp.flushBuffer();
            return;
        }

        chain.doFilter(request, response);
    }
}
