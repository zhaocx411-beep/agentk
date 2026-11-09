package com.kuaishou.agent.operation.backend.keffects.task.adapter;

import java.util.Arrays;
import java.util.HashSet;
import java.util.Set;

import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.kuaishou.agent.operation.backend.keffects.dto.KEffectsCreateTaskRequest;
import com.kuaishou.agent.operation.backend.keffects.task.KEffectsTaskAdapter;
import com.kuaishou.agent.operation.backend.keffects.task.KEffectsTaskAssetService.AssetProfile;

/**
 * {@code e-commerce-goods-studio} 任务适配（kling-aigc-engine）。
 * <p>
 * 平台入参：{@code input.contents} 推荐对象（{@code ref_image} / {@code goods_title} / {@code goods_description}）。
 * 出站上游前在 {@link #normalizeCreate} 转为 {@code contents[]} 数组（上游要求 list）。
 * 落库由 facade 统一写 {@code aop_pipeline_exec} / {@code aop_asset}（{@code source_kind=api}）。
 */
@Component
public class GoodsStudioTaskAdapter implements KEffectsTaskAdapter {

    public static final String TASK_TYPE = "e-commerce-goods-studio";

    private static final Set<String> RESOLUTIONS = new HashSet<>(Arrays.asList("720p", "1080p"));
    private static final Set<String> ASPECT_RATIOS = new HashSet<>(Arrays.asList(
            "9:16", "16:9", "portrait", "vertical", "竖版", "landscape", "horizontal", "横版"));
    private static final Set<Integer> DURATIONS = new HashSet<>(Arrays.asList(15, 30, 60));
    private static final AssetProfile ASSET_PROFILE = AssetProfile.of(TASK_TYPE,
            "productVideo", "video", "image", "coverImage", "resultVideo");

    private final ObjectMapper objectMapper;

    public GoodsStudioTaskAdapter() {
        this(new ObjectMapper());
    }

    public GoodsStudioTaskAdapter(ObjectMapper objectMapper) {
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
    public void normalizeCreate(KEffectsCreateTaskRequest request) {
        if (request == null) {
            return;
        }
        request.setTaskType(TASK_TYPE);
        JsonNode input = request.getInput();
        if (input == null || !input.isObject()) {
            return;
        }
        ObjectNode inputObj = (ObjectNode) input;
        JsonNode contents = inputObj.get("contents");
        if (contents != null && contents.isObject()) {
            inputObj.set("contents", convertObjectToUpstreamArray((ObjectNode) contents));
        }
    }

    @Override
    public void validateCreate(KEffectsCreateTaskRequest request) {
        if (request == null) {
            throw new IllegalArgumentException("request body is required");
        }
        if (StringUtils.hasText(request.getTaskType()) && !TASK_TYPE.equals(request.getTaskType().trim())) {
            throw new IllegalArgumentException("taskType must be " + TASK_TYPE);
        }
        JsonNode input = request.getInput();
        if (input == null || !input.isObject()) {
            throw new IllegalArgumentException("input is required");
        }
        JsonNode contents = input.get("contents");
        if (contents == null || contents.isNull() || contents.isMissingNode()) {
            throw new IllegalArgumentException("input.contents is required");
        }
        if (contents.isObject()) {
            validateContentsObject((ObjectNode) contents);
        } else if (contents.isArray()) {
            validateContentsArray((ArrayNode) contents);
        } else {
            throw new IllegalArgumentException("input.contents must be object or array");
        }
        validateSettings(input.get("settings"));
    }

    private void validateContentsObject(ObjectNode contents) {
        int refImages = countRefImages(contents.get("ref_image"));
        if (refImages < 1 || refImages > 5) {
            throw new IllegalArgumentException("input.contents.ref_image count must be between 1 and 5, got " + refImages);
        }
        if (!StringUtils.hasText(text(contents, "goods_title"))) {
            throw new IllegalArgumentException("input.contents.goods_title is required");
        }
        String title = text(contents, "goods_title");
        if (title != null && title.length() > 500) {
            throw new IllegalArgumentException("input.contents.goods_title max length is 500");
        }
        String description = text(contents, "goods_description");
        if (description != null && description.length() > 2000) {
            throw new IllegalArgumentException("input.contents.goods_description max length is 2000");
        }
    }

    private void validateContentsArray(ArrayNode array) {
        int refImages = 0;
        boolean hasTitle = false;
        for (JsonNode item : array) {
            if (item == null || !item.isObject()) {
                throw new IllegalArgumentException("contents[] must be object");
            }
            String type = text(item, "type");
            if (!StringUtils.hasText(type)) {
                throw new IllegalArgumentException("contents[].type is required");
            }
            switch (type.trim()) {
                case "ref_image":
                    if (!StringUtils.hasText(text(item, "url"))) {
                        throw new IllegalArgumentException("ref_image.url is required");
                    }
                    refImages++;
                    break;
                case "goods_title":
                    if (!StringUtils.hasText(text(item, "text"))) {
                        throw new IllegalArgumentException("goods_title.text is required");
                    }
                    hasTitle = true;
                    break;
                case "goods_description":
                    break;
                case "product_url":
                    throw new IllegalArgumentException("product_url is not supported for e-commerce-goods-studio");
                default:
                    throw new IllegalArgumentException("unsupported contents[].type: " + type.trim());
            }
        }
        if (refImages < 1 || refImages > 5) {
            throw new IllegalArgumentException("ref_image count must be between 1 and 5, got " + refImages);
        }
        if (!hasTitle) {
            throw new IllegalArgumentException("goods_title is required in contents[]");
        }
    }

    private ArrayNode convertObjectToUpstreamArray(ObjectNode contents) {
        ArrayNode array = objectMapper.createArrayNode();
        JsonNode refImage = contents.get("ref_image");
        if (refImage != null && refImage.isTextual() && StringUtils.hasText(refImage.asText())) {
            array.add(contentsItem("ref_image", refImage.asText().trim(), null));
        } else if (refImage != null && refImage.isArray()) {
            for (JsonNode item : refImage) {
                if (item != null && item.isTextual() && StringUtils.hasText(item.asText())) {
                    array.add(contentsItem("ref_image", item.asText().trim(), null));
                }
            }
        }
        String title = text(contents, "goods_title");
        if (StringUtils.hasText(title)) {
            array.add(contentsItem("goods_title", null, title.trim()));
        }
        String description = text(contents, "goods_description");
        if (StringUtils.hasText(description)) {
            array.add(contentsItem("goods_description", null, description.trim()));
        }
        return array;
    }

    private ObjectNode contentsItem(String type, String url, String textValue) {
        ObjectNode item = objectMapper.createObjectNode();
        item.put("type", type);
        if (StringUtils.hasText(url)) {
            item.put("url", url);
        }
        if (StringUtils.hasText(textValue)) {
            item.put("text", textValue);
        }
        return item;
    }

    private static int countRefImages(JsonNode refImage) {
        if (refImage == null || refImage.isNull() || refImage.isMissingNode()) {
            return 0;
        }
        if (refImage.isTextual() && StringUtils.hasText(refImage.asText())) {
            return 1;
        }
        if (!refImage.isArray()) {
            return 0;
        }
        int n = 0;
        for (JsonNode item : refImage) {
            if (item != null && item.isTextual() && StringUtils.hasText(item.asText())) {
                n++;
            }
        }
        return n;
    }

    private void validateSettings(JsonNode settings) {
        if (settings == null || settings.isNull() || settings.isMissingNode()) {
            return;
        }
        if (!settings.isObject()) {
            throw new IllegalArgumentException("input.settings must be object");
        }
        String resolution = firstText(settings, "resolution");
        if (StringUtils.hasText(resolution) && !RESOLUTIONS.contains(resolution.trim())) {
            throw new IllegalArgumentException("settings.resolution must be one of " + RESOLUTIONS);
        }
        String aspectRatio = firstText(settings, "aspect_ratio", "aspectRatio");
        if (StringUtils.hasText(aspectRatio) && !ASPECT_RATIOS.contains(aspectRatio.trim())) {
            throw new IllegalArgumentException("settings.aspect_ratio must be one of " + ASPECT_RATIOS);
        }
        JsonNode duration = settings.get("duration");
        if (duration != null && !duration.isNull() && !duration.isMissingNode()) {
            if (!duration.isInt() || !DURATIONS.contains(duration.asInt())) {
                throw new IllegalArgumentException("settings.duration must be one of " + DURATIONS);
            }
        }
    }

    private static String text(JsonNode node, String field) {
        JsonNode value = node == null ? null : node.get(field);
        return value != null && value.isTextual() ? value.asText() : null;
    }

    private static String firstText(JsonNode node, String... fields) {
        for (String field : fields) {
            String value = text(node, field);
            if (StringUtils.hasText(value)) {
                return value;
            }
        }
        return null;
    }
}
