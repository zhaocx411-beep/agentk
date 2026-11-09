package com.kuaishou.agent.operation.backend.common.interceptor;

import static com.kuaishou.framework.util.ObjectMapperUtils.toJSON;

import java.io.IOException;
import java.io.PrintWriter;

import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.web.servlet.HandlerInterceptor;

import com.kuaishou.agent.operation.backend.auth.AgentOperationAuthProperties;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.security.accessproxy.v1.AccessProxyClient;
import com.kuaishou.security.accessproxy.v1.model.ApPrincipal;

/**
 * 静态 ThreadLocal 存放 ApPrincipal，
 * AccessProxy 验票后写入，请求结束清理。
 */
@Component
public class LoginInterceptor implements HandlerInterceptor {

    private static final Logger log = LoggerFactory.getLogger(LoginInterceptor.class);

    private static final ThreadLocal<ApPrincipal> threadLocal = new ThreadLocal<>();

    private final AgentOperationAuthProperties authProperties;

    private final AccessProxyClient accessProxyClient;

    public LoginInterceptor(AgentOperationAuthProperties authProperties, AccessProxyClient accessProxyClient) {
        this.authProperties = authProperties;
        this.accessProxyClient = accessProxyClient;
    }

    public static ThreadLocal<ApPrincipal> getThreadLocal() {
        return threadLocal;
    }

    @Override
    public boolean preHandle(HttpServletRequest request, HttpServletResponse response, Object handler)
            throws Exception {
        if (!authProperties.isEnabled()) {
            return true;
        }
        // CORS 预检通常不带票据；此处若写 JSON，响应缺少 Access-Control-*，浏览器会判定 preflight 失败
        if ("OPTIONS".equalsIgnoreCase(request.getMethod())) {
            return true;
        }
        if (request == null || response == null) {
            log.warn("LoginInterceptor.preHandle: request or response is null");
            if (response != null) {
                writeUnauthorized(response);
            }
            return false;
        }
        log.info("LoginInterceptor request x-ksap-authn-token={}", request.getHeader("x-ksap-authn-token"));
        ApPrincipal principal = accessProxyClient.verifyToken(request);
        String username = principal == null ? null : principal.getUsername();
        log.info("获取登录人信息：登录人信息 {}", username);
        if (principal == null || isBlank(username)) {
            writeUnauthorized(response);
            return false;
        }
        threadLocal.set(principal);
        return true;
    }

    @Override
    public void afterCompletion(HttpServletRequest request, HttpServletResponse response, Object handler,
            Exception ex) {
        threadLocal.remove();
    }

    private static boolean isBlank(String s) {
        return s == null || s.trim().isEmpty();
    }

    private static void writeUnauthorized(HttpServletResponse response) throws IOException {
        if (response == null) {
            return;
        }
        response.setCharacterEncoding("UTF-8");
        response.setContentType("application/json;charset=UTF-8");
        response.setStatus(HttpServletResponse.SC_OK);
        PrintWriter writer = response.getWriter();
        writer.print(toJSON(Message.error(ResultCode.LOGIN_REQUIRED, ResultCode.LOGIN_REQUIRED.getMessage())));
        writer.flush();
        response.flushBuffer();
    }
}
