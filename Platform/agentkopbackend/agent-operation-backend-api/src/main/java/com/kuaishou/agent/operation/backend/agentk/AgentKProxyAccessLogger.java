package com.kuaishou.agent.operation.backend.agentk;

import java.nio.charset.StandardCharsets;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKAppendResultDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKErrorDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListSessionsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMeDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKPostMessageRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionDto;

/**
 * agent-k 透传访问日志：解析请求/响应摘要做结构化 INFO，完整 body 截断打 DEBUG。
 * 不参与业务逻辑，不改变透传字节。
 */
@Component
public class AgentKProxyAccessLogger {

    private static final Logger log = LoggerFactory.getLogger(AgentKProxyAccessLogger.class);
    private static final int MAX_DEBUG_BODY_CHARS = 2048;

    private final ObjectMapper objectMapper;

    public AgentKProxyAccessLogger(ObjectMapper objectMapper) {
        this.objectMapper = objectMapper;
    }

    public void logJsonRequest(String method, String upstreamPath, String query, AgentKOutboundIdentity identity,
            byte[] requestBody) {
        String requestSummary = summarizeRequest(upstreamPath, requestBody);
        log.info("[AgentK] >>> {} {}{} userId={} summary={}",
                method,
                upstreamPath,
                formatQuery(query),
                safeUserId(identity),
                requestSummary);
        if (log.isDebugEnabled() && requestBody != null && requestBody.length > 0) {
            log.debug("[AgentK] >>> body={}", truncateUtf8(requestBody));
        }
    }

    public void logJsonResponse(String method, String upstreamPath, int status, Object responseBody, long elapsedMs) {
        String responseSummary = summarizeResponse(upstreamPath, status, responseBody);
        if (status >= 400) {
            log.warn("[AgentK] <<< {} {} wrappedHttp={} grpcCode={} elapsedMs={} summary={}",
                    method, upstreamPath, status, grpcCodeOf(responseBody), elapsedMs, responseSummary);
        } else {
            log.info("[AgentK] <<< {} {} status={} elapsedMs={} summary={}",
                    method, upstreamPath, status, elapsedMs, responseSummary);
        }
        if (log.isDebugEnabled()) {
            String debugBody = debugBody(responseBody);
            if (debugBody != null) {
                log.debug("[AgentK] <<< body={}", debugBody);
            }
        }
    }

    public void logJsonProxyFailed(String method, String upstreamPath, long elapsedMs, String error) {
        log.warn("[AgentK] <<< {} {} failed elapsedMs={} error={}", method, upstreamPath, elapsedMs, error);
    }

    public void logSseOpen(String upstreamPath, String query, AgentKOutboundIdentity identity, String lastEventId) {
        log.info("[AgentK] >>> GET {} {} userId={} lastEventId={}",
                upstreamPath,
                formatQuery(query),
                safeUserId(identity),
                blankToDash(lastEventId));
    }

    public void logSseComplete(String upstreamPath, int status, long elapsedMs, long bytesTransferred) {
        if (status >= 400) {
            log.warn("[AgentK] <<< SSE {} status={} elapsedMs={} bytes={}",
                    upstreamPath, status, elapsedMs, bytesTransferred);
        } else {
            log.info("[AgentK] <<< SSE {} status={} elapsedMs={} bytes={}",
                    upstreamPath, status, elapsedMs, bytesTransferred);
        }
    }

    public void logSseFailed(String upstreamPath, long elapsedMs, String error) {
        log.warn("[AgentK] <<< SSE {} failed elapsedMs={} error={}", upstreamPath, elapsedMs, error);
    }

    private String summarizeRequest(String upstreamPath, byte[] requestBody) {
        if (requestBody == null || requestBody.length == 0) {
            return "-";
        }
        try {
            if ("/sessions".equals(upstreamPath)) {
                // 出站 body 可能是 Map{title,profile}，不一定是 CreateSessionRequest
                com.fasterxml.jackson.databind.JsonNode node = objectMapper.readTree(requestBody);
                return String.format("createSession title=%s hasProfile=%s",
                        blankToDash(node.path("title").asText(null)),
                        !node.path("profile").isMissingNode() && !node.path("profile").isNull());
            }
            if (upstreamPath.endsWith("/messages")) {
                AgentKPostMessageRequest req = objectMapper.readValue(requestBody, AgentKPostMessageRequest.class);
                String text = req.getText();
                int attachmentCount = req.getAttachments() == null ? 0 : req.getAttachments().size();
                return "postMessage textLen=" + (text == null ? 0 : text.length())
                        + " attachments=" + attachmentCount
                        + " preview=" + previewText(text);
            }
        } catch (Exception e) {
            return "parseFailed";
        }
        return "bodyLen=" + requestBody.length;
    }

    private String summarizeResponse(String upstreamPath, int status, Object responseBody) {
        if (responseBody == null) {
            return "empty";
        }
        if (responseBody instanceof AgentKErrorDto) {
            AgentKErrorDto err = (AgentKErrorDto) responseBody;
            return "grpcCode=" + blankToDash(err.getGrpcCode())
                    + " grpcMessage=" + blankToDash(firstNonBlank(err.getGrpcMessage(), err.getError()))
                    + " wrappedHttp=" + (err.getWrappedHttp() == null ? status : err.getWrappedHttp());
        }
        byte[] bytes = asBytes(responseBody);
        if (bytes == null || bytes.length == 0) {
            return "empty";
        }
        try {
            if (status >= 400) {
                AgentKErrorDto err = objectMapper.readValue(bytes, AgentKErrorDto.class);
                return "grpcCode=" + blankToDash(err.getGrpcCode())
                        + " error=" + blankToDash(err.getError());
            }
            if ("/sessions".equals(upstreamPath)) {
                if (status == 201) {
                    AgentKSessionDto sess = objectMapper.readValue(bytes, AgentKSessionDto.class);
                    return "sessionId=" + blankToDash(sess.getId()) + " tenantId=" + blankToDash(sess.getTenantId());
                }
                AgentKListSessionsResponse page = objectMapper.readValue(bytes, AgentKListSessionsResponse.class);
                int count = page.getSessions() == null ? 0 : page.getSessions().size();
                return "sessionCount=" + count + " hasMore=" + page.getHasMore();
            }
            if (upstreamPath.endsWith("/messages") || upstreamPath.endsWith("/answers")) {
                AgentKAppendResultDto append = objectMapper.readValue(bytes, AgentKAppendResultDto.class);
                int count = append.getEvents() == null ? 0 : append.getEvents().size();
                return "sessionId=" + blankToDash(append.getSessionId())
                        + " executionId=" + blankToDash(append.getExecutionId())
                        + " status=" + blankToDash(append.getStatus())
                        + " eventCount=" + count;
            }
            if (upstreamPath.endsWith("/events")) {
                AgentKListEventsResponse page = objectMapper.readValue(bytes, AgentKListEventsResponse.class);
                int count = page.getEvents() == null ? 0 : page.getEvents().size();
                return "eventCount=" + count + " hasMore=" + page.getHasMore();
            }
            if ("/me".equals(upstreamPath)) {
                AgentKMeDto me = objectMapper.readValue(bytes, AgentKMeDto.class);
                return "display=" + blankToDash(me.getDisplay()) + " hasAvatar=" + StringUtils.hasText(me.getAvatar());
            }
        } catch (Exception e) {
            return "parseFailed bodyLen=" + bytes.length;
        }
        return "bodyLen=" + bytes.length;
    }

    private static String grpcCodeOf(Object responseBody) {
        if (responseBody instanceof AgentKErrorDto) {
            return blankToDash(((AgentKErrorDto) responseBody).getGrpcCode());
        }
        return "-";
    }

    private String debugBody(Object responseBody) {
        if (responseBody == null) {
            return null;
        }
        try {
            if (responseBody instanceof byte[]) {
                byte[] bytes = (byte[]) responseBody;
                return bytes.length == 0 ? null : truncateUtf8(bytes);
            }
            if (responseBody instanceof String) {
                String raw = (String) responseBody;
                return raw.isEmpty() ? null : truncateUtf8(raw.getBytes(StandardCharsets.UTF_8));
            }
            return truncateUtf8(objectMapper.writeValueAsBytes(responseBody));
        } catch (Exception e) {
            return String.valueOf(responseBody);
        }
    }

    private byte[] asBytes(Object responseBody) {
        if (responseBody instanceof byte[]) {
            return (byte[]) responseBody;
        }
        if (responseBody instanceof String) {
            return ((String) responseBody).getBytes(StandardCharsets.UTF_8);
        }
        try {
            return objectMapper.writeValueAsBytes(responseBody);
        } catch (Exception e) {
            return responseBody.toString().getBytes(StandardCharsets.UTF_8);
        }
    }

    private static String firstNonBlank(String first, String second) {
        if (StringUtils.hasText(first)) {
            return first;
        }
        return second;
    }

    private static String safeUserId(AgentKOutboundIdentity identity) {
        if (identity == null || !StringUtils.hasText(identity.getUserId())) {
            return "-";
        }
        return identity.getUserId();
    }

    private static String formatQuery(String query) {
        if (!StringUtils.hasText(query)) {
            return "";
        }
        return "?" + query;
    }

    private static String previewText(String text) {
        if (!StringUtils.hasText(text)) {
            return "-";
        }
        String normalized = text.replace('\n', ' ').trim();
        if (normalized.length() <= 40) {
            return normalized;
        }
        return normalized.substring(0, 40) + "...";
    }

    private static String blankToDash(String value) {
        return StringUtils.hasText(value) ? value : "-";
    }

    private static String truncateUtf8(byte[] bytes) {
        String raw = new String(bytes, StandardCharsets.UTF_8);
        if (raw.length() <= MAX_DEBUG_BODY_CHARS) {
            return raw;
        }
        return raw.substring(0, MAX_DEBUG_BODY_CHARS) + "...(truncated)";
    }
}
