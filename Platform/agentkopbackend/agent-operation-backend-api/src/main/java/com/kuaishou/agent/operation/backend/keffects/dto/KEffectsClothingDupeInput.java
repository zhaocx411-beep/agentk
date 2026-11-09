package com.kuaishou.agent.operation.backend.keffects.dto;

import java.util.ArrayList;
import java.util.List;

import lombok.Data;

/**
 * {@code e-commerce-clothing-dupe} 任务 input。
 */
@Data
public class KEffectsClothingDupeInput {

    private List<Content> contents = new ArrayList<>();

    private Settings settings;

    @Data
    public static class Content {
        /** prompt / source_video / product_image / model_image / bgm / voice */
        private String type;
        private String text;
        private String url;
        private String voiceId;
    }

    @Data
    public static class Settings {
        /** 720p / 1080p */
        private String resolution;
        /** 9:16 / 2:3 / 3:4 / 1:1 / 4:3 / 3:2 / 16:9 / 21:9 */
        private String aspectRatio;
    }
}
