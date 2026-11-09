package com.kuaishou.agent.operation.backend.config;

import java.io.IOException;
import java.util.Arrays;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.env.EnvironmentPostProcessor;
import org.springframework.boot.env.YamlPropertySourceLoader;
import org.springframework.core.env.ConfigurableEnvironment;
import org.springframework.core.env.MapPropertySource;
import org.springframework.core.env.PropertySource;
import org.springframework.core.io.ClassPathResource;
import org.springframework.core.io.Resource;

import com.kuaishou.agent.operation.backend.common.RegionHelper;

/**
 * 按当前 IDC/环境加载区域配置：config/application-{region}-{env}.yml。
 */
public class RegionEnvironmentPostProcessor implements EnvironmentPostProcessor {

    private static final Logger LOGGER = LoggerFactory.getLogger(RegionEnvironmentPostProcessor.class);
    private static final String REGION_CONFIG_META = "agentOperationRegionConfigMeta";
    private final YamlPropertySourceLoader loader = new YamlPropertySourceLoader();

    @Override
    public void postProcessEnvironment(ConfigurableEnvironment environment, SpringApplication application) {
        String region = RegionHelper.getCurrentRegion();
        String env = RegionHelper.getCurrentEnv();
        String configFileName = RegionHelper.getCurrentConfigFileName();

        Map<String, Object> meta = new HashMap<>();
        meta.put("agent.operation.backend.region", region);
        meta.put("agent.operation.backend.region.env", env);
        meta.put("agent.operation.backend.region.config-file", configFileName);
        meta.put("agent.operation.backend.region.config-loaded", false);
        environment.getPropertySources().addFirst(new MapPropertySource(REGION_CONFIG_META, meta));

        try {
            Resource resource = new ClassPathResource(configFileName);
            LOGGER.info("[AgentOperationRegionConfig] Prepare to load region config, activeProfiles={}, "
                            + "region={}, env={}, configFile={}, resource={}",
                    Arrays.toString(environment.getActiveProfiles()), region, env, configFileName,
                    resource.getDescription());
            if (!resource.exists()) {
                meta.put("agent.operation.backend.region.config-exists", false);
                LOGGER.warn("[AgentOperationRegionConfig] Config file not found: {}", configFileName);
                return;
            }
            meta.put("agent.operation.backend.region.config-exists", true);
            List<PropertySource<?>> propertySources = loader.load("agentOperationRegionConfig", resource);
            for (PropertySource<?> propertySource : propertySources) {
                environment.getPropertySources().addFirst(propertySource);
            }
            meta.put("agent.operation.backend.region.config-loaded", true);
            meta.put("agent.operation.backend.region.config-property-source-count", propertySources.size());
            LOGGER.info("[AgentOperationRegionConfig] Loaded region config successfully, configFile={}, "
                    + "propertySourceCount={}", configFileName, propertySources.size());
        } catch (IOException e) {
            meta.put("agent.operation.backend.region.config-load-error", e.getMessage());
            LOGGER.error("[AgentOperationRegionConfig] Failed to load config: {}", configFileName, e);
        }
    }
}
