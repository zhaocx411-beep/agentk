package com.kuaishou.agent.operation.backend.platform.config;

import javax.annotation.PreDestroy;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.util.StringUtils;

import com.amazonaws.auth.AWSStaticCredentialsProvider;
import com.amazonaws.auth.BasicAWSCredentials;
import com.kuaishou.blobstore.s3.BS3Client;
import com.kuaishou.blobstore.s3.BS3ClientConfig;
import com.kuaishou.blobstore.s3.regions.HB1RegionProvider;

/**
 * BlobStore {@link BS3Client} 单例：媒体上传签发 URL，以及 KEffects 产物 bucket+key → blobUrl。
 * <p>
 * 开启：{@code agent.operation.backend.media.enabled=true}，并配置 access-key / secret-key。
 */
@Configuration
@ConditionalOnProperty(prefix = "agent.operation.backend.media", name = "enabled", havingValue = "true")
public class BlobStoreConfig {

    private static final Logger LOGGER = LoggerFactory.getLogger(BlobStoreConfig.class);

    private BS3Client bs3Client;

    @Bean
    public BS3Client bs3Client(
            @Value("${agent.operation.backend.media.access-key:}") String accessKey,
            @Value("${agent.operation.backend.media.secret-key:}") String secretKey) {
        if (!StringUtils.hasText(accessKey) || !StringUtils.hasText(secretKey)) {
            throw new IllegalStateException(
                    "agent.operation.backend.media.enabled=true 时必须配置 access-key / secret-key");
        }
        LOGGER.info("初始化 BS3Client（媒体上传）");
        try {
            BS3ClientConfig clientConfig = BS3ClientConfig.builder()
                    .regionProvider(new HB1RegionProvider())
                    .credentialsProvider(new AWSStaticCredentialsProvider(
                            new BasicAWSCredentials(accessKey, secretKey)))
                    .build();
            bs3Client = BS3Client.create(clientConfig);
            bs3Client.tryWarmup();
            LOGGER.info("BS3Client 初始化完成");
            return bs3Client;
        } catch (Exception e) {
            LOGGER.error("BS3Client 初始化失败", e);
            throw new IllegalStateException("BS3Client 初始化失败: " + e.getMessage(), e);
        }
    }

    @PreDestroy
    public void destroy() {
        if (bs3Client != null) {
            LOGGER.info("关闭 BS3Client");
            bs3Client.shutdown();
        }
    }
}
