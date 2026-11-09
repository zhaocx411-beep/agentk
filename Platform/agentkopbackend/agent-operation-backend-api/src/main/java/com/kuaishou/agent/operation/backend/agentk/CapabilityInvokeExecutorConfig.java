package com.kuaishou.agent.operation.backend.agentk;

import java.util.concurrent.Executor;
import java.util.concurrent.ThreadPoolExecutor;

import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.scheduling.concurrent.ThreadPoolTaskExecutor;

/**
 * Capability invoke 后台等待 AgentK 的线程池。HTTP 立即返回，gRPC 在此池中阻塞到结束。
 */
@Configuration
public class CapabilityInvokeExecutorConfig {

    @Bean(name = "capabilityInvokeExecutor")
    public Executor capabilityInvokeExecutor() {
        ThreadPoolTaskExecutor executor = new ThreadPoolTaskExecutor();
        executor.setCorePoolSize(4);
        executor.setMaxPoolSize(32);
        executor.setQueueCapacity(200);
        executor.setKeepAliveSeconds(120);
        executor.setAllowCoreThreadTimeOut(true);
        executor.setThreadNamePrefix("capability-invoke-");
        executor.setRejectedExecutionHandler(new ThreadPoolExecutor.AbortPolicy());
        executor.initialize();
        return executor;
    }
}
