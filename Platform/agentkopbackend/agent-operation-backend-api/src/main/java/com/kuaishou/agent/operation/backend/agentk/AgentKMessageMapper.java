package com.kuaishou.agent.operation.backend.agentk;

import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKErrorDto;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;

/**
 * 将 agent-k 上游 {@link ResponseEntity} 转为平台 {@link Message}。
 */
@Component
public class AgentKMessageMapper {

    public <T> Message<T> toMessage(ResponseEntity<?> upstream, Class<T> successType) {
        Object body = upstream.getBody();
        if (upstream.getStatusCode().is2xxSuccessful()) {
            if (body == null) {
                return Message.ok(null);
            }
            if (successType.isInstance(body)) {
                return Message.ok(successType.cast(body));
            }
        }
        return new Message<>(mapUpstreamStatus(upstream.getStatusCodeValue()).getCode(),
                extractAgentKErrorMessage(body), null);
    }

    /**
     * 上游已经映射过的错误原样转给调用方，不再一律盖成 600。
     */
    public static <T> Message<T> keepMappedError(Message<?> upstream, String fallback) {
        if (upstream == null) {
            return Message.error(ResultCode.RELY_SERVICE_BUSY, fallback);
        }
        String message = StringUtils.hasText(upstream.getMessage()) ? upstream.getMessage() : fallback;
        return new Message<>(upstream.getCode(), message, null);
    }

    private static String extractAgentKErrorMessage(Object body) {
        if (body instanceof AgentKErrorDto) {
            AgentKErrorDto dto = (AgentKErrorDto) body;
            if (StringUtils.hasText(dto.getError())) {
                return dto.getError();
            }
            if (StringUtils.hasText(dto.getGrpcMessage())) {
                String code = StringUtils.hasText(dto.getGrpcCode()) ? dto.getGrpcCode() + ": " : "";
                return code + dto.getGrpcMessage();
            }
        }
        return ResultCode.RELY_SERVICE_BUSY.getMessage();
    }

    private static ResultCode mapUpstreamStatus(int status) {
        if (status == HttpStatus.NOT_FOUND.value()) {
            return ResultCode.NOT_FOUND;
        }
        if (status == HttpStatus.BAD_REQUEST.value()) {
            return ResultCode.PARAM_VALIDATION_FAILURE;
        }
        if (status == HttpStatus.UNAUTHORIZED.value()) {
            return ResultCode.UNAUTHORIZED;
        }
        if (status == HttpStatus.FORBIDDEN.value()) {
            return ResultCode.FORBIDDEN;
        }
        if (status == HttpStatus.CONFLICT.value()) {
            return ResultCode.OPERATION_UNSUPPORTED;
        }
        if (status == HttpStatus.BAD_GATEWAY.value() || status >= 500) {
            return ResultCode.RELY_SERVICE_BUSY;
        }
        return ResultCode.SERVICE_BUSY;
    }
}
