package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.regex.Pattern;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkModelsKconf;
import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkModelsParser.ParsedCatalog;
import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkModelsService;
import com.kuaishou.agent.operation.backend.agentk.tools.AgentkPipelineInfo;
import com.kuaishou.agent.operation.backend.agentk.tools.AgentkToolInfo;
import com.kuaishou.agent.operation.backend.agentk.tools.AgentkToolsService;
import com.kuaishou.agent.operation.backend.agentk.tools.AgentkToolsService.ToolsSnapshot;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkProperties;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.AgentConfigView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.AgentPipelineView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.AgentToolView;

/**
 * 将 UI 的聊天模型 / systemPrompt / tools / 工具模型 编译为 agent-k 会话 Profile JSON。
 * <p>
 * 模型名单优先来自 KConf {@code klingai.agentk.models}（与 agent-k 同源）；
 * kconf 空/不可达时退回 {@link AgentkProperties} 本地名单。
 * 工具名单优先拉 agent-k {@code GET /v1/capabilities}，失败/空则用比照 agent-k 的 mock，
 * 并并入本地 {@code batch-eval.tools}。
 * <p>
 * 产出文档对齐当前 agent-k Profile 契约：
 * <ul>
 *   <li>{@code defaultModel}：聊天模型</li>
 *   <li>{@code tools}: 本地机制工具 {@code {"name":"..."}}；Catalog 工具必须
 *       {@code {"ref":"tool:agentk.*","name":"..."}}</li>
 *   <li>{@code pipelines}: {@code [{"ref":"pipeline:agentk.*","name":"..."}]}</li>
 *   <li>{@code toolDefaults.<tool>.model}：工具默认模型（取代已删除的 {@code toolModels}）</li>
 * </ul>
 * profile version 对齐 agent-k：对 canonical JSON 做 SHA-256 取前 12 位 hex。
 */
@Component
public class ProfileCompiler {

    private static final Logger logger = LoggerFactory.getLogger(ProfileCompiler.class);
    private static final Pattern SAFE_NAME = Pattern.compile("[A-Za-z0-9._-]{1,128}");
    private static final Pattern TOOL_NAME = Pattern.compile("^[a-z][a-z0-9_]{0,127}$");

    private final ObjectMapper objectMapper;
    private final AgentkProperties properties;
    private final AgentkModelsService modelsService;
    private final AgentkToolsService toolsService;

    public ProfileCompiler(ObjectMapper objectMapper, AgentkProperties properties,
            AgentkModelsService modelsService, AgentkToolsService toolsService) {
        this.objectMapper = objectMapper;
        this.properties = properties;
        this.modelsService = modelsService;
        this.toolsService = toolsService;
    }

    public CompiledProfile compile(String chatModel, String systemPrompt, List<String> tools,
            Map<String, String> toolModelDefaults) {
        return compile(chatModel, systemPrompt, tools, toolModelDefaults, null);
    }

    /**
     * @param pipelineName 可选；非空时写入 {@code profile.pipelines}，并把同名工具从 tools
     *                     中剔除——同一能力若两条通道各挂一次，一个 task 会生成两份产物。
     */
    public CompiledProfile compile(String chatModel, String systemPrompt, List<String> tools,
            Map<String, String> toolModelDefaults, String pipelineName) {
        Catalog catalog = requireCatalog();
        validateName("model", chatModel);
        if (!catalog.chatModels.contains(chatModel)) {
            throw new IllegalArgumentException("聊天模型不在模型名单中: " + chatModel
                    + "（来源：KConf klingai.agentk.models，或 batch-eval.chat-models 兜底）");
        }
        if (!StringUtils.hasText(systemPrompt)) {
            throw new IllegalArgumentException("systemPrompt 不能为空");
        }
        AgentkPipelineInfo pipeline = resolvePipeline(pipelineName, catalog.pipelineByName);
        List<String> normalizedTools = new ArrayList<>(normalizeTools(tools, catalog.tools));
        if (pipeline != null && normalizedTools.remove(pipeline.getName())) {
            logger.info("pipeline {} 已从 tools 中剔除：同一能力只走 pipeline-runs 一条通道",
                    pipeline.getName());
        }
        Map<String, Map<String, Object>> toolDefaults = buildToolDefaults(
                normalizedTools, toolModelDefaults, catalog.toolModels);

        // 字段顺序对齐 agent-k profile.Document 的常见序列化，保证 version 哈希稳定。
        Map<String, Object> document = new LinkedHashMap<>();
        document.put("defaultModel", chatModel);
        document.put("system", systemPrompt.trim());
        document.put("tools", toToolEntries(normalizedTools, catalog.toolByName));
        if (!toolDefaults.isEmpty()) {
            document.put("toolDefaults", toolDefaults);
        }
        if (pipeline != null) {
            document.put("pipelines", Collections.singletonList(toPipelineEntry(pipeline)));
        }

        String profileJson = writeProfileJson(document);
        return new CompiledProfile(sha256Prefix(profileJson), profileJson, normalizedTools, toolDefaults,
                pipeline == null ? null : pipeline.getName(),
                pipeline == null ? null : pipeline.getRef());
    }

    public AgentConfigView toAgentConfig() {
        // 离线页 agent-config 已改为可选 agent 列表；本方法仅保留给单测/内部诊断编译名单。
        throw new UnsupportedOperationException(
                "agent-config 已改为可选 agent 列表，请使用 OfflineJobService.getAgentConfig");
    }

    /** 供离线执行器按 pipeline 的 inputSchema 组装 args。 */
    public AgentkPipelineInfo findPipeline(String pipelineName) {
        return resolvePipeline(pipelineName, requireCatalog().pipelineByName);
    }

    private static AgentkPipelineInfo resolvePipeline(String pipelineName,
            Map<String, AgentkPipelineInfo> pipelineByName) {
        if (!StringUtils.hasText(pipelineName)) {
            return null;
        }
        String name = pipelineName.trim();
        validateToolName(name);
        AgentkPipelineInfo pipeline = pipelineByName.get(name);
        if (pipeline == null) {
            throw new IllegalArgumentException("pipeline 不在可选名单中: " + name
                    + "（来源：agent-k /v1/capabilities 或中后台 mock；请刷新 agent-config）");
        }
        return pipeline;
    }

    private static Map<String, String> toPipelineEntry(AgentkPipelineInfo pipeline) {
        if (!StringUtils.hasText(pipeline.getRef())) {
            throw new IllegalStateException("pipeline 缺少 ref，无法写入 Profile: " + pipeline.getName());
        }
        Map<String, String> entry = new LinkedHashMap<>();
        entry.put("ref", pipeline.getRef().trim());
        entry.put("name", pipeline.getName());
        return entry;
    }

    private static List<AgentPipelineView> toPipelineViews(Collection<AgentkPipelineInfo> pipelines) {
        List<AgentPipelineView> views = new ArrayList<>(pipelines.size());
        for (AgentkPipelineInfo pipeline : pipelines) {
            AgentPipelineView view = new AgentPipelineView();
            view.setName(pipeline.getName());
            view.setRef(pipeline.getRef());
            view.setDescription(pipeline.getDescription());
            view.setCapabilityId(pipeline.getCapabilityId());
            view.setSource(pipeline.getSource());
            view.setInputSchema(pipeline.getInputSchema() == null
                    ? new LinkedHashMap<>() : new LinkedHashMap<>(pipeline.getInputSchema()));
            views.add(view);
        }
        return views;
    }

    private Catalog requireCatalog() {
        ParsedCatalog kconfCatalog = modelsService.snapshot();
        List<String> chatModels = kconfCatalog.chatModelNames();
        Map<String, List<String>> toolModels = copyToolModels(kconfCatalog.toolModels());
        String sourceVersion = StringUtils.hasText(kconfCatalog.getVersion())
                ? "kconf:" + kconfCatalog.getVersion()
                : null;

        if (chatModels.isEmpty()) {
            chatModels = normalizeConfiguredNames(properties.getChatModels(), "chat-models");
            sourceVersion = "local-batch-eval";
        }
        if (chatModels.isEmpty()) {
            throw new IllegalStateException(
                    "模型名单为空：请配置 KConf klingai.agentk.models，或退回填写 "
                            + "agent.operation.backend.agentk.batch-eval.chat-models");
        }
        if (toolModels.isEmpty()) {
            toolModels = loadLocalToolModels();
            if (sourceVersion == null) {
                sourceVersion = "local-batch-eval";
            }
        }
        if (sourceVersion == null) {
            sourceVersion = "kconf:" + AgentkModelsKconf.KEY;
        }

        String defaultChat = properties.getDefaultChatModel();
        if (!StringUtils.hasText(defaultChat)) {
            defaultChat = chatModels.get(0);
        } else {
            validateName("default-chat-model", defaultChat);
            if (!chatModels.contains(defaultChat)) {
                throw new IllegalStateException("default-chat-model 不在模型名单中: " + defaultChat
                        + "（当前来源 " + sourceVersion + "）");
            }
        }
        ToolsSnapshot toolsSnapshot = toolsService.snapshot();
        List<String> tools = new ArrayList<>();
        Map<String, AgentkToolInfo> toolByName = new LinkedHashMap<>();
        for (AgentkToolInfo tool : toolsSnapshot.getTools()) {
            tools.add(tool.getName());
            toolByName.put(tool.getName(), tool);
        }
        Map<String, AgentkPipelineInfo> pipelineByName = new LinkedHashMap<>();
        for (AgentkPipelineInfo pipeline : toolsSnapshot.getPipelines()) {
            pipelineByName.put(pipeline.getName(), pipeline);
        }
        return new Catalog(sourceVersion, chatModels, defaultChat, tools,
                toolsSnapshot.getSource(), toolByName, toolModels, pipelineByName);
    }

    private Map<String, List<String>> loadLocalToolModels() {
        Map<String, List<String>> toolModels = new LinkedHashMap<>();
        if (properties.getToolModels() == null) {
            return toolModels;
        }
        for (Map.Entry<String, List<String>> entry : properties.getToolModels().entrySet()) {
            String tool = entry.getKey();
            if (!StringUtils.hasText(tool)) {
                continue;
            }
            validateToolName(tool);
            List<String> models = normalizeConfiguredNames(entry.getValue(), "tool-models." + tool);
            if (!models.isEmpty()) {
                toolModels.put(tool, models);
            }
        }
        return toolModels;
    }

    private static List<String> normalizeConfiguredNames(List<String> raw, String field) {
        if (raw == null || raw.isEmpty()) {
            return Collections.emptyList();
        }
        LinkedHashSet<String> out = new LinkedHashSet<>();
        for (String item : raw) {
            if (!StringUtils.hasText(item)) {
                continue;
            }
            String name = item.trim();
            validateName(field, name);
            out.add(name);
        }
        return new ArrayList<>(out);
    }

    private static List<String> normalizeTools(List<String> tools, List<String> allowed) {
        if (tools == null) {
            return Collections.emptyList();
        }
        Set<String> allowedSet = new LinkedHashSet<>(allowed);
        LinkedHashSet<String> normalized = new LinkedHashSet<>();
        for (String tool : tools) {
            if (!StringUtils.hasText(tool)) {
                continue;
            }
            String name = tool.trim();
            validateToolName(name);
            if (!allowedSet.contains(name)) {
                throw new IllegalArgumentException("工具不在可选名单中: " + name
                        + "（来源：agent-k /v1/capabilities 或中后台 mock；请刷新 agent-config）");
            }
            normalized.add(name);
        }
        return new ArrayList<>(normalized);
    }

    private static Map<String, Map<String, Object>> buildToolDefaults(List<String> tools,
            Map<String, String> requested, Map<String, List<String>> catalogToolModels) {
        Map<String, String> req = requested == null ? Collections.emptyMap() : requested;
        Map<String, Map<String, Object>> out = new LinkedHashMap<>();
        for (String tool : tools) {
            List<String> options = catalogToolModels.get(tool);
            if (options == null || options.isEmpty()) {
                if (req.containsKey(tool) && StringUtils.hasText(req.get(tool))) {
                    throw new IllegalArgumentException("工具 " + tool + " 未配置可选模型，不能指定 toolDefaults.model");
                }
                continue;
            }
            String model = req.get(tool);
            if (!StringUtils.hasText(model)) {
                model = options.get(0);
            } else {
                model = model.trim();
                validateName("toolDefaults." + tool + ".model", model);
                if (!options.contains(model)) {
                    throw new IllegalArgumentException("工具模型不在本地配置名单中: " + tool + " -> " + model);
                }
            }
            Map<String, Object> defaults = new LinkedHashMap<>();
            defaults.put("model", model);
            out.put(tool, defaults);
        }
        for (String tool : req.keySet()) {
            if (!tools.contains(tool) && StringUtils.hasText(req.get(tool))) {
                throw new IllegalArgumentException("toolDefaults 指定了未启用的工具: " + tool);
            }
        }
        return out;
    }

    private static List<Map<String, String>> toToolEntries(List<String> tools,
            Map<String, AgentkToolInfo> toolByName) {
        List<Map<String, String>> entries = new ArrayList<>(tools.size());
        for (String tool : tools) {
            Map<String, String> entry = new LinkedHashMap<>();
            AgentkToolInfo info = toolByName == null ? null : toolByName.get(tool);
            String ref = resolveToolRef(info);
            if (StringUtils.hasText(ref)) {
                entry.put("ref", ref);
            }
            entry.put("name", tool);
            entries.add(entry);
        }
        return entries;
    }

    /**
     * Catalog 工具优先用显式 ref；仅有 capabilityId 时合成 {@code tool:&lt;id&gt;}。
     * 本地机制工具两者皆空，返回 null。
     */
    private static String resolveToolRef(AgentkToolInfo info) {
        if (info == null) {
            return null;
        }
        if (StringUtils.hasText(info.getRef())) {
            return info.getRef().trim();
        }
        if (StringUtils.hasText(info.getCapabilityId())) {
            return "tool:" + info.getCapabilityId().trim();
        }
        return null;
    }

    private static List<AgentToolView> toToolViews(Map<String, AgentkToolInfo> toolByName,
            List<String> tools) {
        List<AgentToolView> views = new ArrayList<>(tools.size());
        for (String name : tools) {
            AgentkToolInfo info = toolByName.get(name);
            AgentToolView view = new AgentToolView();
            view.setName(name);
            if (info != null) {
                view.setRef(info.getRef());
                view.setDescription(info.getDescription());
                view.setMode(info.getMode());
                view.setCapabilityId(info.getCapabilityId());
                view.setSource(info.getSource());
            }
            views.add(view);
        }
        return views;
    }

    private static Map<String, List<String>> copyToolModels(Map<String, List<String>> source) {
        Map<String, List<String>> copy = new LinkedHashMap<>();
        for (Map.Entry<String, List<String>> entry : source.entrySet()) {
            copy.put(entry.getKey(), new ArrayList<>(entry.getValue()));
        }
        return copy;
    }

    private static void validateName(String field, String value) {
        if (value == null || !SAFE_NAME.matcher(value).matches()) {
            throw new IllegalArgumentException(field + " 名称非法: " + value);
        }
    }

    private static void validateToolName(String value) {
        if (value == null || !TOOL_NAME.matcher(value).matches()) {
            throw new IllegalArgumentException("tool 名称非法: " + value);
        }
    }

    private String writeProfileJson(Map<String, Object> document) {
        try {
            return objectMapper.writeValueAsString(document);
        } catch (JsonProcessingException e) {
            throw new IllegalStateException("序列化 profile JSON 失败", e);
        }
    }

    private static String sha256Prefix(String content) {
        try {
            byte[] hash = MessageDigest.getInstance("SHA-256")
                    .digest(content.getBytes(StandardCharsets.UTF_8));
            StringBuilder hex = new StringBuilder();
            for (byte b : hash) {
                hex.append(String.format("%02x", b));
            }
            return hex.substring(0, 12);
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException("JDK 缺少 SHA-256", e);
        }
    }

    private static final class Catalog {
        private final String sourceVersion;
        private final List<String> chatModels;
        private final String defaultChatModel;
        private final List<String> tools;
        private final String toolsSource;
        private final Map<String, AgentkToolInfo> toolByName;
        private final Map<String, List<String>> toolModels;
        private final Map<String, AgentkPipelineInfo> pipelineByName;

        private Catalog(String sourceVersion, List<String> chatModels, String defaultChatModel,
                List<String> tools, String toolsSource, Map<String, AgentkToolInfo> toolByName,
                Map<String, List<String>> toolModels, Map<String, AgentkPipelineInfo> pipelineByName) {
            this.sourceVersion = sourceVersion;
            this.chatModels = chatModels;
            this.defaultChatModel = defaultChatModel;
            this.tools = tools;
            this.toolsSource = toolsSource;
            this.toolByName = toolByName;
            this.toolModels = toolModels;
            this.pipelineByName = pipelineByName;
        }
    }

    public static final class CompiledProfile {
        private final String version;
        private final String profileJson;
        private final List<String> tools;
        private final Map<String, Map<String, Object>> toolDefaults;
        private final String pipelineName;
        private final String pipelineRef;

        private CompiledProfile(String version, String profileJson, List<String> tools,
                Map<String, Map<String, Object>> toolDefaults, String pipelineName, String pipelineRef) {
            this.version = version;
            this.profileJson = profileJson;
            this.tools = tools;
            this.toolDefaults = toolDefaults;
            this.pipelineName = pipelineName;
            this.pipelineRef = pipelineRef;
        }

        /** 为空表示本次只跑 Agent Loop。 */
        public String getPipelineName() {
            return pipelineName;
        }

        public String getPipelineRef() {
            return pipelineRef;
        }

        public String getVersion() {
            return version;
        }

        /** agent-k 会话 Profile JSON（历史字段名 yaml 保留兼容 DB 列名）。 */
        public String getYaml() {
            return profileJson;
        }

        public List<String> getTools() {
            return tools;
        }

        public Map<String, Map<String, Object>> getToolDefaults() {
            return toolDefaults;
        }
    }
}
