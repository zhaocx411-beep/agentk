package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collection;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.agentk.tools.AgentkPipelineInfo;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;

/**
 * 把一条 dataset_task 转成某个 pipeline 的 args。
 * <p>
 * 取值优先级：
 * <ol>
 *   <li>{@code extraFields.pipelineArgs.<pipelineName>}：整体作为 args，多 pipeline 各存一份，
 *       与展示用的商品字段互不干扰</li>
 *   <li>按 inputSchema 的属性名做通用兜底映射（images/video_prompt/title 等常见键）</li>
 * </ol>
 * 组装完按 schema 的 {@code required} 做本地必填校验：缺字段直接给出可读提示，
 * 而不是等 agent-k 返回 400。schema 未知时只走第 1 步，不猜字段。
 */
@Component
public class PipelineArgsResolver {

    /** task.extraFields 里承载 pipeline 入参的约定键。 */
    public static final String PIPELINE_ARGS_KEY = "pipelineArgs";

    /** 图片数组类字段的候选名；值取自 task.referImages。 */
    private static final List<String> IMAGE_KEYS = Arrays.asList("images", "referImages", "imageUrls");
    /** 提示词类字段的候选名；值取自 task.prompt。 */
    private static final List<String> PROMPT_KEYS = Arrays.asList("video_prompt", "prompt", "text");
    /** 标题类字段的候选名；值取自 extraFields。 */
    private static final List<String> TITLE_KEYS = Arrays.asList("title", "name");
    /** extraFields 里可当标题用的键。 */
    private static final List<String> TITLE_SOURCE_FIELDS = Arrays.asList("商品标题", "title", "标题");
    /** 链接类字段的候选名；值取自 task.linkUrl。 */
    private static final List<String> LINK_KEYS = Arrays.asList("url", "link", "linkUrl", "product_url");

    public Map<String, Object> resolve(DatasetTask task, AgentkPipelineInfo pipeline) {
        if (pipeline == null) {
            throw new IllegalArgumentException("pipeline 不能为空");
        }
        if (task == null) {
            throw new IllegalArgumentException("数据集用例不存在，无法组装 pipeline 入参");
        }
        Map<String, Object> schema = pipeline.getInputSchema();
        Map<String, Object> args = explicitArgs(task, pipeline.getName());
        if (args == null) {
            args = mapByConvention(task, schema);
        }
        validateRequired(args, schema, pipeline.getName());
        return args;
    }

    /** 返回 null 表示 task 没有为该 pipeline 显式配参。 */
    @SuppressWarnings("unchecked")
    private static Map<String, Object> explicitArgs(DatasetTask task, String pipelineName) {
        Map<String, Object> extraFields = task.getExtraFields();
        if (extraFields == null || extraFields.isEmpty()) {
            return null;
        }
        Object bucket = extraFields.get(PIPELINE_ARGS_KEY);
        if (!(bucket instanceof Map)) {
            return null;
        }
        Object args = ((Map<String, Object>) bucket).get(pipelineName);
        if (!(args instanceof Map)) {
            return null;
        }
        return new LinkedHashMap<>((Map<String, Object>) args);
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> mapByConvention(DatasetTask task, Map<String, Object> schema) {
        Map<String, Object> args = new LinkedHashMap<>();
        Object rawProperties = schema == null ? null : schema.get("properties");
        if (!(rawProperties instanceof Map)) {
            // schema 未知时不猜字段：没有 pipelineArgs 就交给必填校验报错。
            return args;
        }
        Map<String, Object> properties = (Map<String, Object>) rawProperties;
        List<String> images = task.getReferImages() == null
                ? new ArrayList<>() : new ArrayList<>(task.getReferImages());
        Map<String, Object> extraFields = task.getExtraFields() == null
                ? new LinkedHashMap<>() : task.getExtraFields();

        if (!images.isEmpty()) {
            putFirstMatch(args, properties, IMAGE_KEYS, images);
        }
        if (StringUtils.hasText(task.getPrompt())) {
            putFirstMatch(args, properties, PROMPT_KEYS, task.getPrompt().trim());
        }
        String title = firstNonBlankField(extraFields, TITLE_SOURCE_FIELDS);
        if (StringUtils.hasText(title)) {
            putFirstMatch(args, properties, TITLE_KEYS, title);
        }
        if (StringUtils.hasText(task.getLinkUrl())) {
            putFirstMatch(args, properties, LINK_KEYS, task.getLinkUrl().trim());
        }
        // extraFields 里与 schema 同名的标量字段直接透传（多 pipeline 自定义入参的轻量兜底）。
        for (Map.Entry<String, Object> entry : extraFields.entrySet()) {
            String key = entry.getKey();
            if (PIPELINE_ARGS_KEY.equals(key) || args.containsKey(key) || !properties.containsKey(key)) {
                continue;
            }
            if (entry.getValue() != null) {
                args.put(key, entry.getValue());
            }
        }
        return args;
    }

    private static void putFirstMatch(Map<String, Object> args, Map<String, Object> properties,
            List<String> candidates, Object value) {
        for (String key : candidates) {
            if (properties.containsKey(key)) {
                args.put(key, value);
                return;
            }
        }
    }

    private static String firstNonBlankField(Map<String, Object> fields, Collection<String> keys) {
        for (String key : keys) {
            Object value = fields.get(key);
            if (value != null && StringUtils.hasText(String.valueOf(value))) {
                return String.valueOf(value).trim();
            }
        }
        return null;
    }

    @SuppressWarnings("unchecked")
    private static void validateRequired(Map<String, Object> args, Map<String, Object> schema,
            String pipelineName) {
        Object required = schema == null ? null : schema.get("required");
        if (!(required instanceof Collection)) {
            return;
        }
        List<String> missing = new ArrayList<>();
        for (Object item : (Collection<Object>) required) {
            String key = String.valueOf(item);
            Object value = args.get(key);
            if (isBlank(value)) {
                missing.add(key);
            }
        }
        if (!missing.isEmpty()) {
            throw new IllegalArgumentException("pipeline " + pipelineName + " 缺少必填入参: "
                    + String.join(", ", missing)
                    + "；可在用例 extraFields." + PIPELINE_ARGS_KEY + "." + pipelineName + " 中补齐");
        }
    }

    private static boolean isBlank(Object value) {
        if (value == null) {
            return true;
        }
        if (value instanceof CharSequence) {
            return !StringUtils.hasText(value.toString());
        }
        if (value instanceof Collection) {
            return ((Collection<?>) value).isEmpty();
        }
        return false;
    }
}
