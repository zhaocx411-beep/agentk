package com.kuaishou.agent.operation.backend.model;

import java.time.LocalDateTime;

import javax.annotation.Nullable;

import com.kuaishou.framework.util.HostInfo;
import com.kuaishou.infra.boot.util.WebServerUtils;

/**
 * API 接口返回值，请根据实际情况修改，公司范围内暂无相关规范
 *
 * @author liushilong <liushilong@kuaishou.com>
 * Created on 2022-11-29
 */
public class Message<T> {
    /** 业务状态码，与 {@link ResultCode} 一致 */
    private int code;
    // 状态描述
    private String message;
    // 实际返回的数据
    private T data;
    // host
    private String host = HostInfo.getHostName();
    // port
    private int port = WebServerUtils.getPort();
    // 当前时间
    private LocalDateTime timestamp = LocalDateTime.now();

    public Message(ResultCode result, @Nullable String message, @Nullable T data) {
        this(result.getCode(), message == null ? result.getMessage() : message, data);
    }


    public Message(int code, String message, T data) {
        this.code = code;
        this.message = message;
        this.data = data;
    }

    public static Message ok() {
        return new Message(ResultCode.SUCCESS, null, null);
    }

    public static <T> Message<T> ok(T data) {
        return new Message(ResultCode.SUCCESS, null, data);
    }

    public static Message error(ResultCode result) {
        return new Message(result, null, null);
    }

    public static Message error(ResultCode result, String message) {
        return new Message(result, message, null);
    }

    public static <T> Message error(ResultCode result, T data) {
        return new Message(result, null, data);
    }

    public static <T> Message error(ResultCode result, String message, T data) {
        return new Message(result, message, data);
    }

    public int getCode() {
        return code;
    }

    public void setCode(int code) {
        this.code = code;
    }

    public String getMessage() {
        return message;
    }

    public void setMessage(String message) {
        this.message = message;
    }

    public T getData() {
        return data;
    }

    public void setData(T data) {
        this.data = data;
    }

    public String getHost() {
        return host;
    }

    public void setHost(String host) {
        this.host = host;
    }

    public int getPort() {
        return port;
    }

    public void setPort(int port) {
        this.port = port;
    }

    public LocalDateTime getTimestamp() {
        return timestamp;
    }

    public void setTimestamp(LocalDateTime timestamp) {
        this.timestamp = timestamp;
    }
}
