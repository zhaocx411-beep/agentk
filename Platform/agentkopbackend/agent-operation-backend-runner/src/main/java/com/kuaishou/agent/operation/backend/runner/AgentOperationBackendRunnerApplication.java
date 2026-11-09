package com.kuaishou.agent.operation.backend.runner;

import java.util.Arrays;
import java.util.Optional;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.boot.ApplicationRunner;
import org.springframework.context.annotation.Bean;
import org.springframework.core.env.Environment;

import com.kuaishou.agent.operation.backend.common.RegionHelper;
import com.kuaishou.infra.boot.KsSpringApplicationBuilder;
import com.kuaishou.infra.boot.autoconfigure.KsBootApplication;

/**
 * Agent 运维后台 Runner 启动类。
 */
@KsBootApplication(scanBasePackages = "com.kuaishou.agent.operation.backend")
public class AgentOperationBackendRunnerApplication {

    private static final Logger LOGGER = LoggerFactory.getLogger(AgentOperationBackendRunnerApplication.class);

    public static void main(String[] args) {
        KsSpringApplicationBuilder.just()
                // 后续新增 kbus / task / rocketmq runner 时，在这里继续 .service(...)
                .run(args);
    }

    @Bean
    public ApplicationRunner agentOperationBackendRunnerEnvironmentLogger(Environment environment) {
        return args -> {
            String kwsRegion = Optional.ofNullable(System.getenv("KWS_SERVICE_REGION")).orElse("");
            String kwsStage = Optional.ofNullable(System.getenv("KWS_SERVICE_STAGE")).orElse("");
            LOGGER.info(
                    "AgentOperationBackendRunner 启动环境: applicationName={}, activeProfiles={}, defaultProfiles={}, "
                            + "kwsServiceRegion(raw)={}, kwsServiceStage(raw)={}, "
                            + "regionHelper.region={}, regionHelper.env={}, regionHelper.configFile={}, "
                            + "regionHelper.isOversea={}, regionHelper.isProd={}, "
                            + "regionConfig.loaded={}",
                    environment.getProperty("spring.application.name"),
                    Arrays.toString(environment.getActiveProfiles()),
                    Arrays.toString(environment.getDefaultProfiles()),
                    kwsRegion,
                    kwsStage,
                    environment.getProperty("agent.operation.backend.region", RegionHelper.getCurrentRegion()),
                    environment.getProperty("agent.operation.backend.region.env", RegionHelper.getCurrentEnv()),
                    environment.getProperty("agent.operation.backend.region.config-file",
                            RegionHelper.getCurrentConfigFileName()),
                    RegionHelper.isOversea(),
                    RegionHelper.isProd(),
                    environment.getProperty("agent.operation.backend.region.config-loaded"));
        };
    }
}
