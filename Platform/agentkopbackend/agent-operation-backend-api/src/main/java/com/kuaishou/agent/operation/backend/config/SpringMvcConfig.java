package com.kuaishou.agent.operation.backend.config;

import javax.annotation.Nonnull;
import javax.servlet.Filter;

import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.boot.web.servlet.FilterRegistrationBean;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.core.Ordered;
import org.springframework.format.FormatterRegistry;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;

import com.kuaishou.agent.operation.backend.agentk.AgentKProperties;
import com.kuaishou.agent.operation.backend.agentk.AgentKRegressionProperties;
import com.kuaishou.agent.operation.backend.auth.AgentOperationAuthProperties;
import com.kuaishou.agent.operation.backend.keffects.KEffectsProperties;
import com.kuaishou.agent.operation.backend.plugin.ApplicationWebServerCustomizer;
import com.kuaishou.infra.boot.converter.HasIntValueConverter;

/**
 * Spring MVC 配置
 *
 * Write the code. Change the world.
 *
 * @author trang
 */
@Configuration
@EnableConfigurationProperties({AgentOperationAuthProperties.class, AgentKProperties.class,
        AgentKRegressionProperties.class, KEffectsProperties.class})
class SpringMvcConfig {

    @Bean
    FilterRegistrationBean<Filter> agentOperationCorsHeaderFilterRegistration() {
        FilterRegistrationBean<Filter> registration =
                new FilterRegistrationBean<>(new AgentOperationCorsHeaderFilter());
        registration.setOrder(Ordered.HIGHEST_PRECEDENCE);
        registration.addUrlPatterns("/*");
        return registration;
    }

    @Bean
    ApplicationWebServerCustomizer applicationWebServerCustomizer() {
        return new ApplicationWebServerCustomizer();
    }

    @Bean
    WebMvcConfigurer webMvcConfigurer() {
        return new WebMvcConfigurer() {

            @Override
            public void addFormatters(@Nonnull FormatterRegistry registry) {
                // 用于 String <==> HasIntValue 之间的转换
                registry.addConverter(new HasIntValueConverter());
            }

        };
    }

}