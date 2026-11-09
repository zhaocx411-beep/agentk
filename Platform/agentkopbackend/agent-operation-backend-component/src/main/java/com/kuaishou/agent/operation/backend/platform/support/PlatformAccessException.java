package com.kuaishou.agent.operation.backend.platform.support;

/**
 * 平台资产归属/权限异常（无权限读他人 session 等）。
 */
public class PlatformAccessException extends RuntimeException {

    public PlatformAccessException(String message) {
        super(message);
    }
}
