package com.kuaishou.agent.operation.backend.agentk;

/**
 * agent-k 出站失败。{@code status} 为上游状态码，调用未走通（连接/超时等）时为 0。
 */
public class AgentkOutboundException extends RuntimeException {

    private final int status;

    public AgentkOutboundException(int status, String message) {
        super(message);
        this.status = status;
    }

    public AgentkOutboundException(int status, String message, Throwable cause) {
        super(message, cause);
        this.status = status;
    }

    public int getStatus() {
        return status;
    }

    /** 传输失败、429、5xx 可重试；其余 4xx 是契约问题，重试只会重复失败。 */
    public boolean isRetryable() {
        return status == 0 || status == 429 || status >= 500;
    }
}
