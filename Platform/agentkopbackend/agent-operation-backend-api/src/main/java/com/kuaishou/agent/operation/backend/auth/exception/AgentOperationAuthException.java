package com.kuaishou.agent.operation.backend.auth.exception;

import com.kuaishou.agent.operation.backend.model.ResultCode;

/**
 * 后台 API 鉴权异常。
 */
public class AgentOperationAuthException extends RuntimeException {

    private static final long serialVersionUID = 1L;

    private final ResultCode resultCode;

    public AgentOperationAuthException(ResultCode resultCode, String message) {
        super(message);
        this.resultCode = resultCode;
    }

    public ResultCode getResultCode() {
        return resultCode;
    }
}
