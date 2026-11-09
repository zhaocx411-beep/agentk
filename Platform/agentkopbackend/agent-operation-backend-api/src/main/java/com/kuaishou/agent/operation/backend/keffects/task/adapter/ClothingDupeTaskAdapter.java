package com.kuaishou.agent.operation.backend.keffects.task.adapter;

import java.util.Arrays;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;

import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.keffects.dto.KEffectsClothingDupeInput;
import com.kuaishou.agent.operation.backend.keffects.dto.KEffectsClothingDupeInput.Content;
import com.kuaishou.agent.operation.backend.keffects.dto.KEffectsClothingDupeInput.Settings;
import com.kuaishou.agent.operation.backend.keffects.dto.KEffectsCreateTaskRequest;
import com.kuaishou.agent.operation.backend.keffects.task.KEffectsTaskAdapter;
import com.kuaishou.agent.operation.backend.keffects.task.KEffectsTaskAssetService.AssetProfile;

/**
 * {@code e-commerce-clothing-dupe} 任务适配（kling-aigc-engine）。
 * <p>
 * 落库由 {@link com.kuaishou.agent.operation.backend.keffects.task.KEffectsTaskFacade} 统一调用
 * {@link com.kuaishou.agent.operation.backend.keffects.task.KEffectsTaskAssetService}。
 */
@Component
public class ClothingDupeTaskAdapter implements KEffectsTaskAdapter {

    public static final String TASK_TYPE = "e-commerce-clothing-dupe";

    private static final Set<String> RESOLUTIONS = new HashSet<>(Arrays.asList("720p", "1080p"));
    private static final Set<String> ASPECT_RATIOS = new HashSet<>(Arrays.asList(
            "9:16", "2:3", "3:4", "1:1", "4:3", "3:2", "16:9", "21:9"));
    private static final AssetProfile ASSET_PROFILE = AssetProfile.of(TASK_TYPE,
            "productVideo", "videoWithBgmTts", "stitchedVideo",
            "videoWithBgm", "videoWithTts", "videoRaw");

    private final ObjectMapper objectMapper;

    public ClothingDupeTaskAdapter(ObjectMapper objectMapper) {
        this.objectMapper = objectMapper;
    }

    @Override
    public String taskType() {
        return TASK_TYPE;
    }

    @Override
    public String upstreamBaseUrl() {
        return KlingAigcEngineUpstream.baseUrl();
    }

    @Override
    public AssetProfile assetProfile() {
        return ASSET_PROFILE;
    }

    @Override
    public void validateCreate(KEffectsCreateTaskRequest request) {
        if (request == null) {
            throw new IllegalArgumentException("request body is required");
        }
        if (StringUtils.hasText(request.getTaskType()) && !TASK_TYPE.equals(request.getTaskType().trim())) {
            throw new IllegalArgumentException("taskType must be " + TASK_TYPE);
        }
        KEffectsClothingDupeInput input = convertInput(request.getInput());
        if (input == null || input.getContents() == null || input.getContents().isEmpty()) {
            throw new IllegalArgumentException("input.contents is required");
        }
        Map<String, Integer> counts = new HashMap<>();
        for (Content content : input.getContents()) {
            if (content == null || !StringUtils.hasText(content.getType())) {
                throw new IllegalArgumentException("contents[].type is required");
            }
            String type = content.getType().trim();
            counts.merge(type, 1, Integer::sum);
            switch (type) {
                case "prompt":
                    if (!StringUtils.hasText(content.getText())) {
                        throw new IllegalArgumentException("prompt.text is required");
                    }
                    break;
                case "source_video":
                case "product_image":
                case "model_image":
                case "bgm":
                    if (!StringUtils.hasText(content.getUrl())) {
                        throw new IllegalArgumentException(type + ".url is required");
                    }
                    break;
                case "voice":
                    if (!StringUtils.hasText(content.getVoiceId())) {
                        throw new IllegalArgumentException("voice.voiceId is required");
                    }
                    break;
                default:
                    throw new IllegalArgumentException("unsupported contents[].type: " + type);
            }
        }
        requireCount(counts, "source_video", 1, 1);
        requireCount(counts, "product_image", 1, 5);
        requireCount(counts, "model_image", 0, 5);
        requireCount(counts, "prompt", 0, 1);
        requireCount(counts, "bgm", 0, 1);
        requireCount(counts, "voice", 0, 1);

        Settings settings = input.getSettings();
        if (settings != null) {
            if (StringUtils.hasText(settings.getResolution())
                    && !RESOLUTIONS.contains(settings.getResolution().trim())) {
                throw new IllegalArgumentException("settings.resolution must be one of " + RESOLUTIONS);
            }
            if (StringUtils.hasText(settings.getAspectRatio())
                    && !ASPECT_RATIOS.contains(settings.getAspectRatio().trim())) {
                throw new IllegalArgumentException("settings.aspectRatio must be one of " + ASPECT_RATIOS);
            }
        }
    }

    private KEffectsClothingDupeInput convertInput(JsonNode input) {
        if (input == null || input.isNull() || input.isMissingNode()) {
            return null;
        }
        return objectMapper.convertValue(input, KEffectsClothingDupeInput.class);
    }

    private static void requireCount(Map<String, Integer> counts, String type, int min, int max) {
        int n = counts.getOrDefault(type, 0);
        if (n < min || n > max) {
            throw new IllegalArgumentException(type + " count must be between " + min + " and " + max
                    + ", got " + n);
        }
    }
}
