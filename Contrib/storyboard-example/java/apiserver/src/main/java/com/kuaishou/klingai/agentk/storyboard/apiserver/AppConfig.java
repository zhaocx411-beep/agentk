package com.kuaishou.klingai.agentk.storyboard.apiserver;

import java.nio.file.Path;
import java.util.Map;

record AppConfig(
        Path profileFile,
        String host,
        int port,
        Models models) {

    static AppConfig load(Map<String, String> environment) {
        int port;
        try {
            port = Integer.parseInt(value(environment, "API_PORT", "8080"));
        } catch (NumberFormatException error) {
            throw new IllegalArgumentException(
                    "API_PORT must be an integer between 1 and 65535", error);
        }
        if (port < 1 || port > 65535) {
            throw new IllegalArgumentException("API_PORT must be an integer between 1 and 65535");
        }

        return new AppConfig(
                Path.of(value(environment, "PROFILE_FILE", "profile.json")),
                value(environment, "API_HOST", "127.0.0.1"),
                port,
                new Models(
                        value(environment, "STORYBOARD_CHAT_MODEL", "deepseek-v4-pro"),
                        value(environment, "STORYBOARD_IMAGE_MODEL", "z-image-turbo"),
                        value(environment, "STORYBOARD_VIDEO_MODEL", "kling-3.0")));
    }

    private static String value(Map<String, String> environment, String name, String fallback) {
        String value = environment.get(name);
        return value == null || value.trim().isEmpty() ? fallback : value.trim();
    }
}
