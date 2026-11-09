package com.kuaishou.agent.operation.backend.platform.support;

/**
 * 对应接口层 {@code ResultCode.BAD_REQUEST}，与参数校验失败区分。
 */
public class PlatformBadRequestException extends RuntimeException {

    public PlatformBadRequestException(String message) {
        super(message);
    }
}
