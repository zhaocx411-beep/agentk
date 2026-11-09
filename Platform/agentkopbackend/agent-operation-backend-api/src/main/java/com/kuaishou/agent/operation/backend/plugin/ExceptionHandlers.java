package com.kuaishou.agent.operation.backend.plugin;

import static com.kuaishou.framework.util.ObjectMapperUtils.toJSON;
import static com.kuaishou.agent.operation.backend.model.ResultCode.SERVICE_BUSY;

import java.util.Map;

import javax.servlet.http.HttpServletRequest;

import org.apache.catalina.connector.ClientAbortException;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.converter.HttpMessageNotReadableException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

import com.google.common.collect.Maps;
import com.kuaishou.agent.operation.backend.auth.exception.AgentOperationAuthException;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.support.PlatformAccessException;
import com.kuaishou.agent.operation.backend.platform.support.PlatformBadRequestException;

/**
 * Spring 全局异常处理器
 *
 * Write the code. Change the world.
 *
 * @author trang
 */
@RestControllerAdvice
public class ExceptionHandlers {

    private static final Logger logger = LoggerFactory.getLogger(ExceptionHandlers.class);

    @ExceptionHandler(AgentOperationAuthException.class)
    public Message<?> handleAuth(AgentOperationAuthException e) {
        logger.warn("{}", e.getMessage());
        return Message.error(e.getResultCode(), e.getMessage());
    }

    @ExceptionHandler(PlatformAccessException.class)
    public Message<?> handlePlatformAccess(PlatformAccessException e) {
        logger.warn("{}", e.getMessage());
        return Message.error(ResultCode.FORBIDDEN, e.getMessage());
    }

    @ExceptionHandler(PlatformBadRequestException.class)
    public Message<?> handlePlatformBadRequest(PlatformBadRequestException e) {
        logger.warn("{}", e.getMessage());
        return Message.error(ResultCode.BAD_REQUEST, e.getMessage());
    }

    @ExceptionHandler(IllegalArgumentException.class)
    public Message<?> handleIllegalArgument(IllegalArgumentException e) {
        logger.warn("{}", e.getMessage());
        return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, e.getMessage());
    }

    @ExceptionHandler(HttpMessageNotReadableException.class)
    public Message<?> handleNotReadable(HttpMessageNotReadableException e) {
        String detail = e.getMostSpecificCause() != null
                ? e.getMostSpecificCause().getMessage() : e.getMessage();
        logger.warn("bad request body: {}", detail);
        return Message.error(ResultCode.BAD_REQUEST,
                "请求体 JSON 不完整或格式错误: " + detail);
    }

    /** 客户端主动断开（SSE/长连接常见），不按 5xx 处理。 */
    @ExceptionHandler(ClientAbortException.class)
    public void handleClientAbort(ClientAbortException e) {
        logger.warn("client aborted: {}", e.getMessage());
    }

    @ExceptionHandler(Throwable.class)
    public Message<?> handleThrowable(HttpServletRequest request, Throwable e) {
        if (isClientAbort(e)) {
            logger.warn("client aborted: {}", e.getMessage());
            return null;
        }
        printStackTrace(request, e);
        return Message.error(SERVICE_BUSY, e.getMessage());
    }

    private static boolean isClientAbort(Throwable e) {
        for (Throwable t = e; t != null; t = t.getCause()) {
            if (t instanceof ClientAbortException) {
                return true;
            }
            String msg = t.getMessage();
            if (msg != null && (msg.contains("Broken pipe") || msg.contains("Connection reset by peer"))) {
                return true;
            }
        }
        return false;
    }

    /**
     * 打印异常信息
     */
    private void printErrorMsg(Throwable e) {
        logger.error("handle:{}, msg:{}", e.getClass().getName(), e.getMessage());
    }

    /**
     * 打印异常信息和入参
     */
    private void printErrorMsg(HttpServletRequest request, Throwable e) {
        Map<String, String[]> params = request.getParameterMap();
        Map<String, Object> map = Maps.transformValues(
                params, arr -> (arr == null || arr.length == 0) ? null : arr.length == 1 ? arr[0] : arr
        );
        logger.error("handle:{}, msg:{}, params:{}", e.getClass().getName(), e.getMessage(), toJSON(map));
    }

    /**
     * 打印异常堆栈和入参
     */
    private void printStackTrace(HttpServletRequest request, Throwable e) {
        Map<String, String[]> params = request.getParameterMap();
        Map<String, Object> map = Maps.transformValues(
                params, arr -> (arr == null || arr.length == 0) ? null : arr.length == 1 ? arr[0] : arr
        );
        logger.error("handle:{},  msg: {}, params:{}", e.getClass().getName(), e.getMessage(), toJSON(map), e);
    }

}