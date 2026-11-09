package com.kuaishou.agent.operation.backend.platform.agent100;

import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Agent100 模块定义（对齐 Demo {@code Agent100Definition}）。
 * <p>
 * 第一阶段为代码注册表；第二阶段可迁移到 DB 分组表。
 */
public final class Agent100Definition {

    private final String id;
    private final String name;
    private final String icon;
    private final String accent;
    private final String description;
    private final String defaultModel;
    private final String defaultTitle;
    private final List<Agent100PipelineDef> pipelines;
    private final Map<String, String> modelDefaults;
    private final Map<String, List<String>> modelOptions;
    /** 页面音色配置，如 {@code qwen -> [{id, description}]}。 */
    private final Map<String, List<Map<String, String>>> voices;
    /**
     * 带文案的字段枚举，如 {@code aspect -> [{id, label}]}。
     * 供管理平台下拉；与 inputSchema.enum 同源。
     */
    private final Map<String, List<Map<String, String>>> fieldOptions;

    public Agent100Definition(String id, String name, String icon, String accent, String description,
            String defaultModel, String defaultTitle, List<Agent100PipelineDef> pipelines,
            Map<String, String> modelDefaults, Map<String, List<String>> modelOptions,
            Map<String, List<Map<String, String>>> voices,
            Map<String, List<Map<String, String>>> fieldOptions) {
        this.id = id;
        this.name = name;
        this.icon = icon;
        this.accent = accent;
        this.description = description;
        this.defaultModel = defaultModel;
        this.defaultTitle = defaultTitle;
        this.pipelines = Collections.unmodifiableList(new ArrayList<>(pipelines));
        this.modelDefaults = Collections.unmodifiableMap(new LinkedHashMap<>(modelDefaults));
        Map<String, List<String>> options = new LinkedHashMap<>();
        for (Map.Entry<String, List<String>> entry : modelOptions.entrySet()) {
            options.put(entry.getKey(), Collections.unmodifiableList(new ArrayList<>(entry.getValue())));
        }
        this.modelOptions = Collections.unmodifiableMap(options);
        this.voices = copyLabeledMap(voices);
        this.fieldOptions = copyLabeledMap(fieldOptions);
    }

    private static Map<String, List<Map<String, String>>> copyLabeledMap(
            Map<String, List<Map<String, String>>> source) {
        Map<String, List<Map<String, String>>> copy = new LinkedHashMap<>();
        if (source == null) {
            return Collections.unmodifiableMap(copy);
        }
        for (Map.Entry<String, List<Map<String, String>>> entry : source.entrySet()) {
            copy.put(entry.getKey(), Collections.unmodifiableList(new ArrayList<>(entry.getValue())));
        }
        return Collections.unmodifiableMap(copy);
    }

    public String getId() {
        return id;
    }

    public String getName() {
        return name;
    }

    public String getIcon() {
        return icon;
    }

    public String getAccent() {
        return accent;
    }

    public String getDescription() {
        return description;
    }

    public String getDefaultModel() {
        return defaultModel;
    }

    public String getDefaultTitle() {
        return defaultTitle;
    }

    public List<Agent100PipelineDef> getPipelines() {
        return pipelines;
    }

    public Map<String, String> getModelDefaults() {
        return modelDefaults;
    }

    public Map<String, List<String>> getModelOptions() {
        return modelOptions;
    }

    public Map<String, List<Map<String, String>>> getVoices() {
        return voices;
    }

    public Map<String, List<Map<String, String>>> getFieldOptions() {
        return fieldOptions;
    }

    public Agent100PipelineDef findPipeline(String pipelineName) {
        if (pipelineName == null) {
            return null;
        }
        String key = pipelineName.trim();
        for (Agent100PipelineDef pipeline : pipelines) {
            if (key.equals(pipeline.getName())) {
                return pipeline;
            }
        }
        return null;
    }

    public List<String> pipelineNames() {
        List<String> names = new ArrayList<>(pipelines.size());
        for (Agent100PipelineDef pipeline : pipelines) {
            names.add(pipeline.getName());
        }
        return names;
    }

    public List<String> capabilityIds() {
        List<String> ids = new ArrayList<>(pipelines.size());
        for (Agent100PipelineDef pipeline : pipelines) {
            ids.add(pipeline.getCapability());
        }
        return ids;
    }
}
