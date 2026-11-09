package com.kuaishou.agent.operation.backend.agentk.grpc;

import java.lang.reflect.Method;
import java.util.function.Function;

import javax.annotation.PreDestroy;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.kuaishou.klingai.agentk.client.AgentKClient;
import com.kuaishou.klingai.agentk.client.ClientOptions;
import com.kuaishou.klingai.agentk.client.grpc.AgentKGrpcClient;

/**
 * 进程级 agent-k gRPC SDK 客户端（0.3.6，无 API Key）。
 * <p>
 * 天上已注入 {@code KWS_SERVICE_NAME} 时原样使用；本地没有时补天琴服务名
 * {@code ai-platform-king-agent-operation-backend-api}，再创建 SDK。
 */
@Component
public class AgentKSdkHolder {

    private static final Logger logger = LoggerFactory.getLogger(AgentKSdkHolder.class);
    private static final String KWS_SERVICE_NAME = "KWS_SERVICE_NAME";
    private static final String LOCAL_KWS_SERVICE_NAME = "ai-platform-king-agent-operation-backend-api";

    private final AgentKClient sdkClient;

    public AgentKSdkHolder() {
        applyLocalKwsServiceName();
        this.sdkClient = createClient();
    }

    public AgentKClient client() {
        return sdkClient;
    }

    @PreDestroy
    public void close() {
        sdkClient.close();
    }

    static void applyLocalKwsServiceName() {
        if (StringUtils.hasText(System.getenv(KWS_SERVICE_NAME))) {
            logger.info("KWS_SERVICE_NAME={}", System.getenv(KWS_SERVICE_NAME));
            return;
        }
        if (KwsProcessEnv.put(KWS_SERVICE_NAME, LOCAL_KWS_SERVICE_NAME)) {
            logger.info("local: KWS_SERVICE_NAME={}", LOCAL_KWS_SERVICE_NAME);
        } else {
            logger.warn("local: failed to write process env KWS_SERVICE_NAME={}", LOCAL_KWS_SERVICE_NAME);
        }
    }

    /**
     * 0.3.6 公开 API 是 {@code create(new ClientOptions())}，内部读 {@code System.getenv}。
     * 本地再显式传入同一份服务名，避免 getenv 没写上时身份是空的。
     */
    @SuppressWarnings("unchecked")
    private static AgentKClient createClient() {
        ClientOptions options = new ClientOptions();
        try {
            Class<?> identityClass = Class.forName(
                    "com.kuaishou.klingai.agentk.client.grpc.KessIdentity");
            Method from = identityClass.getDeclaredMethod("from", Function.class);
            from.setAccessible(true);
            Function<String, String> getenv = key -> {
                if (KWS_SERVICE_NAME.equals(key)) {
                    String value = System.getenv(key);
                    return StringUtils.hasText(value) ? value : LOCAL_KWS_SERVICE_NAME;
                }
                return System.getenv(key);
            };
            Object identity = from.invoke(null, getenv);
            Method create = AgentKGrpcClient.class.getDeclaredMethod(
                    "create", ClientOptions.class, identityClass);
            create.setAccessible(true);
            return (AgentKClient) create.invoke(null, options, identity);
        } catch (ReflectiveOperationException e) {
            logger.warn("fallback AgentKGrpcClient.create(ClientOptions): {}", e.toString());
            return AgentKGrpcClient.create(options);
        }
    }
}
