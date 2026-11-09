package com.kuaishou.agent.operation.backend.agentk.grpc;

import org.springframework.http.HttpStatus;

import com.connectrpc.ConnectException;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKErrorDto;

import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.StatusRuntimeException;

/**
 * Connect/gRPC 异常 → 原始 gRPC 码 + 包装用 HTTP 状态。
 * 平台可以继续用 400/502/600 包装，但 {@link Failure#grpcCode} 必须是上游原文。
 */
final class AgentKConnectErrors {

    private AgentKConnectErrors() {
    }

    static HttpStatus toHttpStatus(Throwable error) {
        return describe(error).httpStatus;
    }

    static AgentKErrorDto toErrorDto(Throwable error) {
        return describe(error).toDto();
    }

    static Failure describe(Throwable error) {
        if (error == null) {
            return Failure.unknown("agent-k unavailable", null);
        }
        ConnectException connect = unwrapConnect(error);
        if (connect != null) {
            String code = connect.getCode() == null ? "UNKNOWN" : connect.getCode().name();
            String message = firstNonBlank(connect.getMessage(), code);
            return new Failure(normalizeCode(code), message, httpStatusForName(code), error);
        }
        Status grpc = grpcStatus(error);
        if (grpc != null) {
            String code = grpc.getCode().name();
            String message = firstNonBlank(grpc.getDescription(), error.getMessage(), code);
            return new Failure(code, message, httpStatusForGrpc(grpc.getCode()), error);
        }
        return Failure.unknown(
                firstNonBlank(error.getMessage(), "agent-k unavailable"), error);
    }

    private static Status grpcStatus(Throwable error) {
        for (Throwable cursor = error; cursor != null; cursor = cursor.getCause()) {
            if (cursor instanceof StatusRuntimeException) {
                return ((StatusRuntimeException) cursor).getStatus();
            }
            if (cursor instanceof StatusException) {
                return ((StatusException) cursor).getStatus();
            }
        }
        return null;
    }

    private static ConnectException unwrapConnect(Throwable error) {
        for (Throwable cursor = error; cursor != null; cursor = cursor.getCause()) {
            if (cursor instanceof ConnectException) {
                return (ConnectException) cursor;
            }
        }
        return null;
    }

    private static HttpStatus httpStatusForName(String code) {
        if (code == null) {
            return HttpStatus.BAD_GATEWAY;
        }
        try {
            return httpStatusForGrpc(Status.Code.valueOf(normalizeCode(code)));
        } catch (IllegalArgumentException e) {
            if ("INTERNAL_ERROR".equalsIgnoreCase(code)) {
                return HttpStatus.INTERNAL_SERVER_ERROR;
            }
            return HttpStatus.BAD_GATEWAY;
        }
    }

    private static HttpStatus httpStatusForGrpc(Status.Code code) {
        switch (code) {
            case CANCELLED:
            case INVALID_ARGUMENT:
            case FAILED_PRECONDITION:
            case OUT_OF_RANGE:
                return HttpStatus.BAD_REQUEST;
            case NOT_FOUND:
                return HttpStatus.NOT_FOUND;
            case ALREADY_EXISTS:
            case ABORTED:
                return HttpStatus.CONFLICT;
            case PERMISSION_DENIED:
                return HttpStatus.FORBIDDEN;
            case UNAUTHENTICATED:
                return HttpStatus.UNAUTHORIZED;
            case UNAVAILABLE:
            case DEADLINE_EXCEEDED:
                return HttpStatus.BAD_GATEWAY;
            default:
                return HttpStatus.INTERNAL_SERVER_ERROR;
        }
    }

    private static String normalizeCode(String code) {
        if (code == null || code.isBlank()) {
            return "UNKNOWN";
        }
        return code.trim().toUpperCase().replace('-', '_');
    }

    private static String firstNonBlank(String... values) {
        if (values == null) {
            return null;
        }
        for (String value : values) {
            if (value != null && !value.isBlank()) {
                return value;
            }
        }
        return null;
    }

    static final class Failure {
        final String grpcCode;
        final String grpcMessage;
        final HttpStatus httpStatus;
        final Throwable cause;

        Failure(String grpcCode, String grpcMessage, HttpStatus httpStatus, Throwable cause) {
            this.grpcCode = grpcCode == null ? "UNKNOWN" : grpcCode;
            this.grpcMessage = grpcMessage == null ? this.grpcCode : grpcMessage;
            this.httpStatus = httpStatus == null ? HttpStatus.BAD_GATEWAY : httpStatus;
            this.cause = cause;
        }

        static Failure unknown(String message, Throwable cause) {
            return new Failure("UNKNOWN", message, HttpStatus.BAD_GATEWAY, cause);
        }

        AgentKErrorDto toDto() {
            AgentKErrorDto dto = new AgentKErrorDto();
            dto.setGrpcCode(grpcCode);
            dto.setGrpcMessage(grpcMessage);
            dto.setError(grpcMessage.startsWith(grpcCode)
                    ? grpcMessage
                    : grpcCode + ": " + grpcMessage);
            dto.setWrappedHttp(httpStatus.value());
            return dto;
        }
    }
}
