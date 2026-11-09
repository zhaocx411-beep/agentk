package com.kuaishou.agent.operation.backend.common.config;

import java.util.ArrayList;
import java.util.List;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.context.annotation.Configuration;
import org.springframework.web.servlet.config.annotation.InterceptorRegistration;
import org.springframework.web.servlet.config.annotation.InterceptorRegistry;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;

import com.kuaishou.agent.operation.backend.auth.AgentOperationAuthProperties;
import com.kuaishou.agent.operation.backend.common.interceptor.LoginInterceptor;

/**
 * 后台 API 拦截器配置。
 */
@Configuration
public class InterceptorConfig implements WebMvcConfigurer {

    private static final String API_PATH = "/agent/operation/backend/**";

    @Autowired
    private LoginInterceptor loginInterceptor;

    @Autowired
    private AgentOperationAuthProperties authProperties;

    @Override
    public void addInterceptors(InterceptorRegistry registry) {
        // configured 列表由 Spring 绑定，理论上默认非 null，但需防御配置异常导致 null。
        List<String> excludes = new ArrayList<>();
        List<String> configured = authProperties.getInterceptorExcludePathPatterns();
        if (configured != null && !configured.isEmpty()) {
            excludes.addAll(configured);
        }

        InterceptorRegistration reg = registry.addInterceptor(loginInterceptor).addPathPatterns(API_PATH);
        reg.excludePathPatterns(excludes.toArray(new String[0]));
    }
}
