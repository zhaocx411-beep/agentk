package com.kuaishou.agent.operation.backend.config;

import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

import com.kuaishou.security.accessproxy.v1.AccessProxyClient;
import com.kuaishou.security.accessproxy.v1.model.config.SdkConfig;

@Configuration
public class CasConfig {

    @Bean
    public AccessProxyClient accessProxyClient() {
        SdkConfig sdkConfig = SdkConfig.builder()
                .jwksUrl(
                        "https://ksap-metadata.corp.kuaishou.com/accessproxy_uri/identity_passthrough/.well-known/jwks.json")
                .ksapDomain("keling-operation-platform-test.staging.kuaishou.com")
                .verifyIss(false)
                .build();
        return new AccessProxyClient(sdkConfig);
    }
}
