package com.kuaishou.agent.operation.backend;

import java.util.Arrays;
import java.util.Optional;

import org.mybatis.spring.annotation.MapperScan;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.boot.ApplicationRunner;
import org.springframework.context.annotation.Bean;
import org.springframework.core.env.Environment;

import com.kuaishou.agent.operation.backend.common.RegionHelper;
import com.kuaishou.infra.boot.KsSpringApplication;
import com.kuaishou.infra.boot.autoconfigure.KsBootApplication;

/**
 * KsBoot 应用启动类，默认扫描当前 package
 *
 * Write the code. Change the world.
 *
 * @author trang
 */
@KsBootApplication(scanBasePackages = "com.kuaishou.agent.operation.backend")
@MapperScan("com.kuaishou.agent.operation.backend.platform.dao.mapper")
public class Application {

    private static final Logger LOGGER = LoggerFactory.getLogger(Application.class);

    public static void main(String[] args) {
        KsSpringApplication.justRun(args);
    }

    @Bean
    public ApplicationRunner agentOperationBackendEnvironmentLogger(Environment environment) {
        return args -> {
            String kwsRegion = Optional.ofNullable(System.getenv("KWS_SERVICE_REGION")).orElse("");
            String kwsStage = Optional.ofNullable(System.getenv("KWS_SERVICE_STAGE")).orElse("");
            LOGGER.info(
                    "AgentOperationBackendApi 启动环境: applicationName={}, activeProfiles={}, defaultProfiles={}, "
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