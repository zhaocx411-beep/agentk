package com.kuaishou.klingai.agentk.storyboard.apiserver;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.klingai.agentk.api.v1.Profile;

import java.util.concurrent.CountDownLatch;

public final class Main {
    private Main() {
    }

    public static void main(String[] args) throws Exception {
        AppConfig config = AppConfig.load(System.getenv());
        Profile profile = ProfileLoader.load(config.profileFile());
        ObjectMapper mapper = JsonSupport.createMapper();

        try (SdkStoryboardAgentKClient agentK =
                     SdkStoryboardAgentKClient.create(mapper);
             JobHttpServer server = JobHttpServer.create(config, agentK, profile)) {
            Runtime.getRuntime().addShutdownHook(new Thread(() -> {
                server.close();
                agentK.close();
            }, "shutdown"));
            server.start();
            System.out.printf(
                    "storyboard Java example API listening on %s:%d%n",
                    config.host(), server.port());
            new CountDownLatch(1).await();
        }
    }
}
