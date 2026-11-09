package com.kuaishou.agent.operation.backend.agentk.tools;

import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.AgentkCoreWorkflowIds;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkClient;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkClient.AgentkClientException;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkProperties;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.AgentKCapabilityCacheMapper;
import com.kuaishou.agent.operation.backend.platform.entity.AgentKCapabilityCache;

/**
 * 能力名单。
 * <ul>
 *   <li>{@link #catalogFromCapabilities()}：Lab / {@code GET /agentk/tools}，只查 ListCapabilities
 *       （实时或 {@code aop_agentk_capability} 缓存），不补 mock</li>
 *   <li>{@link #snapshot()}：离线评测编译 Profile，拉不到 capabilities 时仍可用本地 mock</li>
 * </ul>
 */
@Service
public class AgentkToolsService {

    private static final Logger logger = LoggerFactory.getLogger(AgentkToolsService.class);
    private static final ObjectMapper OBJECT_MAPPER = new ObjectMapper();
    private static final TypeReference<Map<String, Object>> SCHEMA_TYPE =
            new TypeReference<Map<String, Object>>() {
            };

    private final AgentkClient agentkClient;
    private final AgentkProperties properties;
    private final AgentKCapabilityCacheMapper capabilityCacheMapper;

    public AgentkToolsService(AgentkClient agentkClient, AgentkProperties properties,
            AgentKCapabilityCacheMapper capabilityCacheMapper) {
        this.agentkClient = agentkClient;
        this.properties = properties;
        this.capabilityCacheMapper = capabilityCacheMapper;
    }

    /**
     * Lab 名单：只来自 agent-k ListCapabilities，失败则读由其写入的 {@code aop_agentk_capability}。
     * 不合并本地 mock pipeline / 工具表。
     */
    public ToolsSnapshot catalogFromCapabilities() {
        JsonNode capabilities = tryLoadCapabilities();
        if (capabilities != null) {
            List<AgentkToolInfo> remoteTools = parseRemoteTools(capabilities);
            List<AgentkPipelineInfo> remotePipelines = parseRemotePipelines(capabilities);
            logger.info("工具名单使用 agent-k ListCapabilities（{} tools, {} pipelines）",
                    remoteTools.size(), remotePipelines.size());
            return ToolsSnapshot.of("agentk", remoteTools, remotePipelines);
        }
        List<AgentKCapabilityCache> cached = listEnabledCache();
        List<AgentkToolInfo> cacheTools = parseCacheTools(cached);
        List<AgentkPipelineInfo> cachePipelines = parseCachePipelines(cached);
        logger.info("工具名单使用 aop_agentk_capability（ListCapabilities 缓存，{} tools, {} pipelines）",
                cacheTools.size(), cachePipelines.size());
        return ToolsSnapshot.of("cache", cacheTools, cachePipelines);
    }

    public ToolsSnapshot snapshot() {
        JsonNode capabilities = tryLoadCapabilities();
        List<AgentkToolInfo> remoteTools = parseRemoteTools(capabilities);
        List<AgentkPipelineInfo> remotePipelines = parseRemotePipelines(capabilities);

        if (!remoteTools.isEmpty() || !remotePipelines.isEmpty()) {
            List<AgentkPipelineInfo> pipelines = preferRemotePipelines(remotePipelines);
            logger.info("工具名单使用 agent-k capabilities（{} tools, {} pipelines）",
                    remoteTools.size(), pipelines.size());
            return ToolsSnapshot.of("agentk", remoteTools, pipelines);
        }

        List<AgentKCapabilityCache> cached = listEnabledCache();
        List<AgentkToolInfo> cacheTools = parseCacheTools(cached);
        List<AgentkPipelineInfo> cachePipelines = parseCachePipelines(cached);
        if (!cacheTools.isEmpty() || !cachePipelines.isEmpty()) {
            List<AgentkPipelineInfo> pipelines = preferRemotePipelines(cachePipelines);
            logger.info("工具名单使用 aop_agentk_capability 缓存（{} tools, {} pipelines）",
                    cacheTools.size(), pipelines.size());
            return ToolsSnapshot.of("cache", cacheTools, pipelines);
        }

        List<AgentkToolInfo> mockTools = AgentkToolsCatalog.mergeConfigured(properties.getTools());
        List<AgentkPipelineInfo> mockPipelines = preferRemotePipelines(remotePipelines);
        logger.info("工具名单使用 mock 兜底（{} tools, {} pipelines）",
                mockTools.size(), mockPipelines.size());
        return ToolsSnapshot.of("mock", mockTools, mockPipelines);
    }

    private List<AgentKCapabilityCache> listEnabledCache() {
        if (capabilityCacheMapper == null) {
            return Collections.emptyList();
        }
        try {
            return capabilityCacheMapper.selectList(new LambdaQueryWrapper<AgentKCapabilityCache>()
                    .eq(AgentKCapabilityCache::getEnabled, true)
                    .orderByAsc(AgentKCapabilityCache::getId));
        } catch (RuntimeException e) {
            logger.warn("读取 aop_agentk_capability 失败，继续 mock 兜底: {}", e.getMessage());
            return Collections.emptyList();
        }
    }

    private List<AgentkToolInfo> parseCacheTools(List<AgentKCapabilityCache> rows) {
        if (rows == null || rows.isEmpty()) {
            return Collections.emptyList();
        }
        LinkedHashSet<String> seen = new LinkedHashSet<>();
        List<AgentkToolInfo> out = new ArrayList<>();
        for (AgentKCapabilityCache row : rows) {
            if (row == null) {
                continue;
            }
            String name = firstText(row.getToolName(), leafName(row.getName()), leafName(row.getAgentkId()));
            if (!StringUtils.hasText(name) || !seen.add(name)) {
                continue;
            }
            if (!StringUtils.hasText(row.getToolName()) && !StringUtils.hasText(row.getToolRef())
                    && StringUtils.hasText(row.getPipelineName())) {
                continue;
            }
            if ("workflow".equalsIgnoreCase(emptyToNull(row.getKind()))
                    && !StringUtils.hasText(row.getToolName()) && !StringUtils.hasText(row.getToolRef())) {
                continue;
            }
            AgentkToolInfo info = new AgentkToolInfo();
            info.setName(name);
            String capabilityId = emptyToNull(row.getAgentkId());
            String toolRef = emptyToNull(row.getToolRef());
            if (!StringUtils.hasText(toolRef) && StringUtils.hasText(capabilityId)) {
                toolRef = "tool:" + capabilityId;
            }
            info.setRef(toolRef);
            info.setCapabilityId(capabilityId);
            String description = firstText(row.getToolDescription(), row.getDescription());
            info.setDescription(description == null ? "" : description);
            info.setMode(emptyToNull(row.getToolMode()));
            info.setSource("cache");
            info.setKind(resolveKind(row.getKind(), capabilityId));
            out.add(info);
        }
        return out;
    }

    private List<AgentkPipelineInfo> parseCachePipelines(List<AgentKCapabilityCache> rows) {
        if (rows == null || rows.isEmpty()) {
            return Collections.emptyList();
        }
        LinkedHashSet<String> seen = new LinkedHashSet<>();
        List<AgentkPipelineInfo> out = new ArrayList<>();
        for (AgentKCapabilityCache row : rows) {
            if (row == null) {
                continue;
            }
            String name = firstText(row.getPipelineName(), leafName(row.getName()), leafName(row.getAgentkId()));
            if (!StringUtils.hasText(name) || !seen.add(name)) {
                continue;
            }
            if (!StringUtils.hasText(row.getPipelineName()) && !StringUtils.hasText(row.getPipelineRef())
                    && !"workflow".equalsIgnoreCase(emptyToNull(row.getKind()))) {
                continue;
            }
            String capabilityId = emptyToNull(row.getAgentkId());
            String ref = emptyToNull(row.getPipelineRef());
            if (!StringUtils.hasText(ref) && StringUtils.hasText(capabilityId)) {
                ref = "pipeline:" + capabilityId;
            }
            if (!StringUtils.hasText(ref)) {
                continue;
            }
            AgentkPipelineInfo info = new AgentkPipelineInfo();
            info.setName(name);
            info.setRef(ref);
            info.setCapabilityId(capabilityId);
            String description = firstText(row.getPipelineDescription(), row.getDescription());
            info.setDescription(description == null ? "" : description);
            info.setSource("cache");
            info.setInputSchema(toSchemaMap(readJson(row.getInputSchemaJson())));
            out.add(info);
        }
        return out;
    }

    private JsonNode readJson(String json) {
        if (!StringUtils.hasText(json)) {
            return null;
        }
        try {
            return OBJECT_MAPPER.readTree(json);
        } catch (Exception e) {
            return null;
        }
    }

    private static String resolveKind(String declaredKind, String capabilityId) {
        if (AgentkCoreWorkflowIds.isWorkflow(capabilityId)) {
            return "workflow";
        }
        return firstText(declaredKind, "activity");
    }

    private static String firstText(String... values) {
        if (values == null) {
            return null;
        }
        for (String value : values) {
            if (StringUtils.hasText(value)) {
                return value.trim();
            }
        }
        return null;
    }

    private static String leafName(String value) {
        if (!StringUtils.hasText(value)) {
            return null;
        }
        String trimmed = value.trim();
        int dot = trimmed.lastIndexOf('.');
        return dot >= 0 ? trimmed.substring(dot + 1) : trimmed;
    }

    private static String emptyToNull(String value) {
        return StringUtils.hasText(value) ? value.trim() : null;
    }

    /** capabilities 拉不到时返回 null，工具与 pipeline 各自退回 mock。 */
    private JsonNode tryLoadCapabilities() {
        try {
            JsonNode root = agentkClient.listCapabilities();
            JsonNode capabilities = root.path("capabilities");
            if (!capabilities.isArray() || capabilities.size() == 0) {
                logger.info("agent-k capabilities 为空");
                return null;
            }
            return capabilities;
        } catch (AgentkClientException e) {
            logger.warn("拉取 agent-k capabilities 失败: {}", e.getMessage());
            return null;
        } catch (RuntimeException e) {
            logger.warn("解析 agent-k capabilities 失败: {}", e.getMessage());
            return null;
        }
    }

    /**
     * pipeline：远端优先；远端为空时用 mock。
     * 远端非空时仍补上 mock 里远端没有的第一方 pipeline（当前仅 generate_brand_ad）。
     */
    private static List<AgentkPipelineInfo> preferRemotePipelines(List<AgentkPipelineInfo> remote) {
        Map<String, AgentkPipelineInfo> merged = new LinkedHashMap<>();
        if (remote != null) {
            for (AgentkPipelineInfo pipeline : remote) {
                merged.put(pipeline.getName(), pipeline);
            }
        }
        if (merged.isEmpty()) {
            return new ArrayList<>(AgentkPipelinesCatalog.firstPartyMock());
        }
        for (AgentkPipelineInfo mock : AgentkPipelinesCatalog.firstPartyMock()) {
            merged.putIfAbsent(mock.getName(), mock);
        }
        return new ArrayList<>(merged.values());
    }

    private List<AgentkPipelineInfo> parseRemotePipelines(JsonNode capabilities) {
        if (capabilities == null) {
            return Collections.emptyList();
        }
        try {
            LinkedHashSet<String> seen = new LinkedHashSet<>();
            List<AgentkPipelineInfo> out = new ArrayList<>();
            for (JsonNode item : capabilities) {
                String kind = item.path("kind").asText("").trim();
                JsonNode pipeline = item.path("pipeline");
                boolean workflow = "workflow".equalsIgnoreCase(kind);
                if ((pipeline.isMissingNode() || pipeline.isNull()) && !workflow) {
                    continue;
                }
                String name = pipeline.path("name").asText("").trim();
                String capabilityId = item.path("id").asText("").trim();
                if (!StringUtils.hasText(name)) {
                    name = leafName(capabilityId);
                }
                String seenKey = StringUtils.hasText(capabilityId) ? capabilityId : name;
                if (!StringUtils.hasText(name) || !seen.add(seenKey)) {
                    continue;
                }
                String ref = item.path("pipelineRef").asText("").trim();
                if (!StringUtils.hasText(ref) && StringUtils.hasText(capabilityId)) {
                    ref = "pipeline:" + capabilityId;
                }
                if (!StringUtils.hasText(ref)) {
                    // 没有 ref 就没法写进 Profile.pipelines，直接跳过而不是产出半条。
                    continue;
                }
                AgentkPipelineInfo info = new AgentkPipelineInfo();
                info.setName(name);
                info.setRef(ref);
                info.setCapabilityId(StringUtils.hasText(capabilityId) ? capabilityId : null);
                String description = pipeline.path("description").asText("");
                if (!StringUtils.hasText(description)) {
                    description = item.path("description").asText("");
                }
                info.setDescription(description);
                info.setSource("agentk");
                info.setInputSchema(toSchemaMap(item.path("inputSchema")));
                out.add(info);
            }
            return out;
        } catch (RuntimeException e) {
            logger.warn("解析 agent-k pipeline 声明失败，使用 mock 兜底: {}", e.getMessage());
            return Collections.emptyList();
        }
    }

    private static Map<String, Object> toSchemaMap(JsonNode schema) {
        if (schema == null || !schema.isObject()) {
            return new LinkedHashMap<>();
        }
        return OBJECT_MAPPER.convertValue(schema, SCHEMA_TYPE);
    }

    private List<AgentkToolInfo> parseRemoteTools(JsonNode capabilities) {
        if (capabilities == null) {
            return Collections.emptyList();
        }
        try {
            LinkedHashSet<String> seen = new LinkedHashSet<>();
            List<AgentkToolInfo> out = new ArrayList<>();
            for (JsonNode item : capabilities) {
                String kind = item.path("kind").asText("").trim();
                JsonNode tool = item.path("tool");
                if (tool.isMissingNode() || tool.isNull()) {
                    continue;
                }
                // workflow 双通道也会带 tool，Lab 名单按 kind 归到 pipeline，这里仍留给评测勾选。
                String name = tool.path("name").asText("").trim();
                if (!StringUtils.hasText(name) || !seen.add(name)) {
                    continue;
                }
                AgentkToolInfo info = new AgentkToolInfo();
                info.setName(name);
                String capabilityId = item.path("id").asText("").trim();
                String toolRef = item.path("toolRef").asText("").trim();
                if (!StringUtils.hasText(toolRef) && StringUtils.hasText(capabilityId)) {
                    toolRef = "tool:" + capabilityId;
                }
                info.setRef(StringUtils.hasText(toolRef) ? toolRef : null);
                info.setCapabilityId(StringUtils.hasText(capabilityId) ? capabilityId : null);
                String description = tool.path("description").asText("");
                if (!StringUtils.hasText(description)) {
                    description = item.path("description").asText("");
                }
                info.setDescription(description);
                info.setMode(tool.path("mode").asText(null));
                info.setSource("agentk");
                info.setKind(resolveKind(kind, capabilityId));
                out.add(info);
            }
            if (out.isEmpty()) {
                logger.info("agent-k capabilities 无 tool 声明，工具名单使用 mock 兜底");
            }
            return out;
        } catch (RuntimeException e) {
            logger.warn("解析 agent-k capabilities 失败，工具名单使用 mock 兜底: {}", e.getMessage());
            return Collections.emptyList();
        }
    }

    public static final class ToolsSnapshot {
        private final String source;
        private final List<AgentkToolInfo> tools;
        private final List<AgentkPipelineInfo> pipelines;

        private ToolsSnapshot(String source, List<AgentkToolInfo> tools,
                List<AgentkPipelineInfo> pipelines) {
            this.source = source;
            this.tools = tools;
            this.pipelines = pipelines;
        }

        public static ToolsSnapshot of(String source, List<AgentkToolInfo> tools) {
            return new ToolsSnapshot(source, tools, Collections.emptyList());
        }

        public static ToolsSnapshot of(String source, List<AgentkToolInfo> tools,
                List<AgentkPipelineInfo> pipelines) {
            return new ToolsSnapshot(source, tools,
                    pipelines == null ? Collections.emptyList() : pipelines);
        }

        /** agentk = 在线 capabilities；cache = aop_agentk_capability；mock = 本地兜底。 */
        public String getSource() {
            return source;
        }

        public List<AgentkToolInfo> getTools() {
            return tools;
        }

        public List<AgentkPipelineInfo> getPipelines() {
            return pipelines;
        }

        /**
         * Lab / 中后台名单：activity 与 workflow 都返回。
         * 同一 capability 若同时有 tool / pipeline，两条都保留（按 kind 区分）。
         * 离线评测编译 Profile 仍用 {@link #getTools()} / {@link #getPipelines()}，不要混用。
         */
        public List<AgentkToolInfo> catalogItems() {
            List<AgentkToolInfo> out = new ArrayList<>();
            LinkedHashSet<String> seen = new LinkedHashSet<>();
            if (tools != null) {
                for (AgentkToolInfo tool : tools) {
                    if (tool == null) {
                        continue;
                    }
                    if (AgentkCoreWorkflowIds.isWorkflow(tool.getCapabilityId())) {
                        tool.setKind("workflow");
                    } else if (!StringUtils.hasText(tool.getKind())) {
                        tool.setKind("activity");
                    }
                    String key = catalogKey(tool.getKind(), tool.getCapabilityId(), tool.getName());
                    if (StringUtils.hasText(key) && !seen.add(key)) {
                        continue;
                    }
                    out.add(tool);
                }
            }
            if (pipelines != null) {
                for (AgentkPipelineInfo pipeline : pipelines) {
                    if (pipeline == null) {
                        continue;
                    }
                    String key = catalogKey("workflow", pipeline.getCapabilityId(), pipeline.getName());
                    if (StringUtils.hasText(key) && !seen.add(key)) {
                        continue;
                    }
                    AgentkToolInfo item = new AgentkToolInfo();
                    item.setKind("workflow");
                    item.setName(pipeline.getName());
                    item.setRef(pipeline.getRef());
                    item.setCapabilityId(pipeline.getCapabilityId());
                    item.setDescription(pipeline.getDescription());
                    item.setSource(pipeline.getSource());
                    out.add(item);
                }
            }
            return out;
        }

        private static String catalogKey(String kind, String capabilityId, String name) {
            String identity = StringUtils.hasText(capabilityId) ? capabilityId.trim()
                    : (StringUtils.hasText(name) ? name.trim() : null);
            if (!StringUtils.hasText(identity)) {
                return null;
            }
            String prefix = StringUtils.hasText(kind) ? kind.trim() : "activity";
            return prefix + ":" + identity;
        }
    }
}
