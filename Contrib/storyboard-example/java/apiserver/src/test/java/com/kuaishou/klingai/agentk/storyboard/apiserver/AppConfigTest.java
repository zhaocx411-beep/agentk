package com.kuaishou.klingai.agentk.storyboard.apiserver;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

import java.util.HashMap;
import java.util.Map;

import org.junit.jupiter.api.Test;

class AppConfigTest {
    @Test
    void loadsDefaultsAndOverrides() {
        Map<String, String> environment = new HashMap<>();
        environment.put("API_PORT", "9090");
        environment.put("STORYBOARD_VIDEO_MODEL", "video-custom");
        AppConfig config = AppConfig.load(environment);
        assertEquals("127.0.0.1", config.host());
        assertEquals(9090, config.port());
        assertEquals("deepseek-v4-pro", config.models().chat());
        assertEquals("video-custom", config.models().video());
    }

    @Test
    void rejectsInvalidPort() {
        assertThrows(IllegalArgumentException.class, () -> AppConfig.load(Map.of(
                "API_PORT", "0")));
        assertThrows(IllegalArgumentException.class, () -> AppConfig.load(Map.of(
                "API_PORT", "not-a-number")));
    }
}
