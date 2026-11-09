package com.kuaishou.agent.operation.backend.agentk;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.time.Duration;
import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.stream.Collectors;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto.AgentKCapabilityItemDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityCacheMetaDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityListViewDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityRefreshResultDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityViewDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilityViewDto.FieldView;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKPipelineBindingDto;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.AgentKCapabilityCacheMapper;
import com.kuaishou.agent.operation.backend.platform.entity.AgentKCapabilityCache;

@Service
public class AgentKCapabilityCacheService implements AgentKPipelineBindingLookup, AgentKToolCapabilityLookup {

    private static final Logger LOGGER = LoggerFactory.getLogger(AgentKCapabilityCacheService.class);
    private static final long CACHE_TTL_SECONDS = 300L;

    private final AgentKClient agentKClient;
    private final AgentKCapabilityCacheMapper capabilityMapper;
    private final ObjectMapper objectMapper;
    private final AtomicBoolean refreshing = new AtomicBoolean(false);
    /** 整份 capabilities 响应 hash，进程内缓存，重启后丢失。 */
    private final AtomicReference<String> snapshotHashRef = new AtomicReference<>("");
    /** 最近一次成功刷新时间（含 hash 未变），避免无 DB 写入时 TTL 失效。 */
    private final AtomicReference<LocalDateTime> lastRefreshAtRef = new AtomicReference<>();
    private final ExecutorService executor;

    public AgentKCapabilityCacheService(AgentKClient agentKClient,
            AgentKCapabilityCacheMapper capabilityMapper,
            ObjectMapper objectMapper) {
        this.agentKClient = agentKClient;
        this.capabilityMapper = capabilityMapper;
        this.objectMapper = objectMapper;
        this.executor = Executors.newSingleThreadExecutor(new ThreadFactory() {
            private final AtomicInteger seq = new AtomicInteger();

            @Override
            public Thread newThread(Runnable r) {
                Thread t = new Thread(r, "agentk-capability-refresh-" + seq.incrementAndGet());
                t.setDaemon(true);
                return t;
            }
        });
    }

    /**
     * 实时拉取 agent-k gRPC {@code ListCapabilities}，并异步回写 DB 缓存。
     */
    public AgentKCapabilitiesDto listRawCapabilities(AgentKOutboundIdentity identity) {
        AgentKCapabilitiesDto live = fetchLiveCapabilities(identity);
        persistSnapshotAsync(live);
        return live;
    }

    /**
     * 前端能力目录：每次实时拉 agent-k，不读库。
     * <p>
     * 成功后异步刷新 {@code aop_agentk_capability}，供建会话补 capability / pipeline 名称使用。
     */
    public AgentKCapabilityListViewDto listFrontendCapabilities(String kind, String workflowKey,
            AgentKOutboundIdentity identity) {
        AgentKCapabilitiesDto live = fetchLiveCapabilities(identity);
        int liveCount = itemsOf(live).size();
        LOGGER.info("agentk capabilities live fetched kind={} workflowKey={} liveCount={} userId={}",
                kind, workflowKey, liveCount, identity == null ? null : identity.getUserId());
        persistSnapshotAsync(live);
        return toFrontendView(live, kind, workflowKey);
    }

    /**
     * 官方工具广场：ListCapabilities 全量（不按 kind 过滤），失败或条数少于库表时回读
     * {@code aop_agentk_capability} 全部 enabled 行，由前端用 {@code kind} 区分 activity / workflow。
     */
    public AgentKCapabilityListViewDto listPlazaCapabilities(AgentKOutboundIdentity identity) {
        AgentKCapabilityListViewDto dbView = plazaFromDb();
        int dbSize = plazaSize(dbView);
        try {
            AgentKCapabilitiesDto live = fetchLiveCapabilities(identity);
            AgentKCapabilityListViewDto liveView = toFrontendView(live, null, null);
            int liveSize = plazaSize(liveView);
            LOGGER.info("official plaza liveCount={} dbCount={} userId={}",
                    liveSize, dbSize, identity == null ? null : identity.getUserId());
            if (liveSize >= dbSize && liveSize > 0) {
                persistSnapshotAsync(live);
                return liveView;
            }
            if (dbSize > 0) {
                LOGGER.warn("official plaza prefer aop_agentk_capability over incomplete live live={} db={}",
                        liveSize, dbSize);
                return dbView;
            }
            persistSnapshotAsync(live);
            return liveView;
        } catch (IllegalStateException e) {
            LOGGER.warn("official plaza live failed, fallback aop_agentk_capability: {}", e.getMessage());
            return dbView;
        }
    }

    private static int plazaSize(AgentKCapabilityListViewDto view) {
        return view == null || view.getCapabilities() == null ? 0 : view.getCapabilities().size();
    }

    private AgentKCapabilityListViewDto plazaFromDb() {
        AgentKCapabilityListViewDto view = new AgentKCapabilityListViewDto();
        List<AgentKCapabilityViewDto> capabilities = new ArrayList<>();
        for (AgentKCapabilityCache row : listEnabled(null)) {
            if (row != null) {
                capabilities.add(toView(row));
            }
        }
        LOGGER.info("official plaza db capabilities count={}", capabilities.size());
        view.setCapabilities(capabilities);
        view.setCache(liveMeta());
        return view;
    }

    /**
     * 实时 ListCapabilities；失败抛 {@link IllegalStateException}（由 Controller 转成 600）。
     */
    public AgentKCapabilitiesDto fetchLiveCapabilities(AgentKOutboundIdentity identity) {
        try {
            ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/capabilities", null, null,
                    AgentKCapabilitiesDto.class, identity);
            if (!upstream.getStatusCode().is2xxSuccessful()
                    || !(upstream.getBody() instanceof AgentKCapabilitiesDto)) {
                throw new IllegalStateException(
                        "agent-k capabilities fetch failed: " + upstream.getStatusCodeValue());
            }
            return (AgentKCapabilitiesDto) upstream.getBody();
        } catch (IllegalStateException e) {
            throw e;
        } catch (Exception e) {
            throw new IllegalStateException("agent-k capabilities fetch failed: " + e.getMessage(), e);
        }
    }

    AgentKCapabilityListViewDto toFrontendView(AgentKCapabilitiesDto live, String kind, String workflowKey) {
        AgentKCapabilityListViewDto view = new AgentKCapabilityListViewDto();
        List<AgentKCapabilityViewDto> capabilities = new ArrayList<>();
        if (StringUtils.hasText(workflowKey)) {
            String key = workflowKey.trim();
            String normalizedKey = stripPipelinePrefix(key);
            String keyLeaf = leafOf(normalizedKey);
            LOGGER.info("agentk capabilities filter by workflowKey raw={} normalized={} leaf={} kind={}",
                    key, normalizedKey, keyLeaf, kind);
            for (AgentKCapabilityItemDto item : itemsOf(live)) {
                AgentKCapabilityViewDto converted = toView(item);
                if (converted == null) {
                    continue;
                }
                boolean matched = matchesWorkflowKey(converted, item, workflowKey);
                if (LOGGER.isDebugEnabled()) {
                    LOGGER.debug(
                            "agentk capabilities match candidate key={} matched={} id={} kind={} name={} "
                                    + "pipelineRef={} pipelineName={}",
                            key,
                            matched,
                            converted.getId(),
                            converted.getKind(),
                            converted.getName(),
                            converted.getPipelineRef(),
                            item.getPipeline() == null ? null : item.getPipeline().getName());
                }
                if (matched && !alreadyHasId(capabilities, converted.getId())) {
                    capabilities.add(converted);
                }
            }
            if (capabilities.isEmpty()) {
                LOGGER.warn(
                        "capability workflowKey miss key={} normalized={} leaf={} liveCount={} kind={} catalog={}",
                        key, normalizedKey, keyLeaf, itemsOf(live).size(), kind,
                        summarizeLiveCatalog(live));
            } else {
                LOGGER.info("agentk capabilities workflowKey hit key={} matchedIds={}",
                        key, capabilities.stream()
                                .map(AgentKCapabilityViewDto::getId)
                                .collect(Collectors.toList()));
            }
        } else {
            for (AgentKCapabilityItemDto item : itemsOf(live)) {
                AgentKCapabilityViewDto converted = toView(item);
                if (converted != null && matchesKind(converted.getKind(), kind)) {
                    capabilities.add(converted);
                }
            }
            LOGGER.info("agentk capabilities list kind={} size={}", kind, capabilities.size());
        }
        view.setCapabilities(capabilities);
        view.setCache(liveMeta());
        return view;
    }

    private static String summarizeLiveCatalog(AgentKCapabilitiesDto live) {
        List<String> parts = new ArrayList<>();
        for (AgentKCapabilityItemDto item : itemsOf(live)) {
            if (item == null) {
                continue;
            }
            String pipelineName = item.getPipeline() == null ? "" : empty(item.getPipeline().getName());
            parts.add(empty(item.getId()) + "|k=" + empty(item.getKind())
                    + "|ref=" + empty(item.getPipelineRef())
                    + "|pn=" + pipelineName
                    + "|tool=" + (item.getTool() == null ? "" : empty(item.getTool().getName())));
        }
        return parts.toString();
    }

    /**
     * 官方 default 用：TTL 过期时尝试拉 agent-k /capabilities；上游失败或空列表则不写 DB，直接读库。
     */
    public Map<String, AgentKPipelineBindingDto> pipelineBindingsStaleRefresh(Collection<String> agentkIds,
            AgentKOutboundIdentity identity) {
        if (isExpired()) {
            refreshSyncIfIdle(identity, false);
        }
        return pipelineBindings(agentkIds);
    }

    /**
     * 按 agentk_id 批量查 capability（单次 IN 查询，走 uniq_agentk_capability_id）。
     */
    public Map<String, AgentKCapabilityCache> mapEnabledByAgentkIds(Collection<String> agentkIds) {
        if (agentkIds == null || agentkIds.isEmpty()) {
            return Collections.emptyMap();
        }
        List<String> ids = agentkIds.stream()
                .filter(StringUtils::hasText)
                .map(String::trim)
                .distinct()
                .collect(Collectors.toList());
        if (ids.isEmpty()) {
            return Collections.emptyMap();
        }
        List<AgentKCapabilityCache> rows = capabilityMapper.selectList(new LambdaQueryWrapper<AgentKCapabilityCache>()
                .eq(AgentKCapabilityCache::getEnabled, true)
                .in(AgentKCapabilityCache::getAgentkId, ids));
        Map<String, AgentKCapabilityCache> result = new LinkedHashMap<>();
        for (AgentKCapabilityCache row : rows) {
            result.put(row.getAgentkId(), row);
        }
        return result;
    }

    @Override
    public String findCapabilityIdByToolName(String toolName) {
        if (!StringUtils.hasText(toolName)) {
            return null;
        }
        String key = toolName.trim();
        AgentKCapabilityCache preferred = null;
        for (AgentKCapabilityCache row : listEnabled(null)) {
            if (!matchesToolName(row, key)) {
                continue;
            }
            if ("activity".equalsIgnoreCase(empty(row.getKind()))) {
                return row.getAgentkId();
            }
            if (preferred == null) {
                preferred = row;
            }
        }
        return preferred == null ? null : preferred.getAgentkId();
    }

    @Override
    public boolean hasCapabilityId(String capabilityId) {
        if (!StringUtils.hasText(capabilityId)) {
            return false;
        }
        return findEnabled(capabilityId.trim()) != null;
    }

    @Override
    public List<ActivityTool> listActivityTools() {
        List<ActivityTool> tools = new ArrayList<>();
        Set<String> seen = new HashSet<>();
        for (AgentKCapabilityCache row : listEnabled("activity")) {
            String name = resolveToolName(row);
            if (!StringUtils.hasText(name) || !StringUtils.hasText(row.getAgentkId()) || !seen.add(name)) {
                continue;
            }
            String instructions = StringUtils.hasText(row.getToolDescription())
                    ? row.getToolDescription()
                    : empty(row.getDescription());
            tools.add(new ActivityTool(name, row.getAgentkId().trim(), empty(row.getDescription()), instructions));
        }
        return tools;
    }

    private static boolean matchesToolName(AgentKCapabilityCache row, String key) {
        if (key.equals(empty(row.getToolName())) || key.equals(empty(row.getName()))) {
            return true;
        }
        String agentkId = empty(row.getAgentkId());
        if (!StringUtils.hasText(agentkId)) {
            return false;
        }
        int dot = agentkId.lastIndexOf('.');
        String leaf = dot >= 0 ? agentkId.substring(dot + 1) : agentkId;
        return key.equals(leaf);
    }

    private static String resolveToolName(AgentKCapabilityCache row) {
        if (StringUtils.hasText(row.getToolName())) {
            return row.getToolName().trim();
        }
        if (StringUtils.hasText(row.getName())) {
            String name = row.getName().trim();
            // name 有时是完整 agentkId
            if (name.contains(".")) {
                int dot = name.lastIndexOf('.');
                return name.substring(dot + 1);
            }
            return name;
        }
        String agentkId = empty(row.getAgentkId());
        if (!StringUtils.hasText(agentkId)) {
            return "";
        }
        int dot = agentkId.lastIndexOf('.');
        return dot >= 0 ? agentkId.substring(dot + 1) : agentkId;
    }

    /**
     * 批量解析建 session 用的 pipeline ref/name（单次 IN 查询）。
     */
    @Override
    public Map<String, AgentKPipelineBindingDto> pipelineBindings(Collection<String> agentkIds) {
        Map<String, AgentKPipelineBindingDto> result = new LinkedHashMap<>();
        for (Map.Entry<String, AgentKCapabilityCache> entry : mapEnabledByAgentkIds(agentkIds).entrySet()) {
            AgentKPipelineBindingDto binding = toPipelineBinding(entry.getValue());
            if (binding != null) {
                result.put(entry.getKey(), binding);
            }
        }
        return result;
    }

    /**
     * 用 capability 缓存里的 pipeline_ref / pipeline_name 覆盖 workflow definition_json。
     * 建会话 / 官方目录当前不走此覆盖，直接用 {@code aop_workflow_version.definition_json}。
     */
    public String resolveWorkflowDefinitionJson(String agentkId, String definitionJson) {
        if (!StringUtils.hasText(agentkId)) {
            return definitionJson;
        }
        AgentKCapabilityCache cap = findEnabled(agentkId.trim());
        if (cap == null) {
            return definitionJson;
        }
        AgentKPipelineBindingDto binding = toPipelineBinding(cap);
        if (binding == null) {
            return definitionJson;
        }
        try {
            ObjectNode node = objectMapper.createObjectNode();
            if (StringUtils.hasText(definitionJson)) {
                JsonNode existing = objectMapper.readTree(definitionJson);
                if (existing != null && existing.isObject()) {
                    node = (ObjectNode) existing.deepCopy();
                }
            }
            node.put("ref", binding.getPipelineRef());
            node.put("name", binding.getPipelineName());
            return objectMapper.writeValueAsString(node);
        } catch (Exception e) {
            LOGGER.warn("resolveWorkflowDefinitionJson failed agentkId={}", agentkId, e);
            return definitionJson;
        }
    }

    public AgentKCapabilityRefreshResultDto refreshSync(AgentKOutboundIdentity identity, boolean force) {
        if (!refreshing.compareAndSet(false, true)) {
            AgentKCapabilityRefreshResultDto result = new AgentKCapabilityRefreshResultDto();
            result.setSkipped(true);
            result.setRefreshed(false);
            result.setChanged(false);
            result.setMessage("capability refresh already running");
            return result;
        }
        try {
            return refreshNow(identity, force);
        } finally {
            refreshing.set(false);
        }
    }

    /** 建会话补 Profile 用：库空时才同步拉一次，避免每次建会话都打 ListCapabilities。 */
    public void warmupIfEmpty(AgentKOutboundIdentity identity) {
        if (!listEnabled(null).isEmpty()) {
            return;
        }
        refreshSyncIfIdle(identity, false);
    }

    private void refreshAsync(AgentKOutboundIdentity identity) {
        if (!refreshing.compareAndSet(false, true)) {
            return;
        }
        executor.execute(() -> {
            try {
                refreshNow(identity, false);
            } catch (Exception e) {
                LOGGER.warn("agent-k capability async refresh failed", e);
            } finally {
                refreshing.set(false);
            }
        });
    }

    private AgentKCapabilityRefreshResultDto refreshSyncIfIdle(AgentKOutboundIdentity identity, boolean force) {
        if (!refreshing.compareAndSet(false, true)) {
            AgentKCapabilityRefreshResultDto result = new AgentKCapabilityRefreshResultDto();
            result.setSkipped(true);
            result.setRefreshed(false);
            result.setChanged(false);
            result.setMessage("capability refresh already running");
            return result;
        }
        try {
            return refreshNow(identity, force);
        } finally {
            refreshing.set(false);
        }
    }

    private void persistSnapshotAsync(AgentKCapabilitiesDto body) {
        if (body == null) {
            return;
        }
        if (!refreshing.compareAndSet(false, true)) {
            return;
        }
        executor.execute(() -> {
            try {
                applySnapshot(body, false);
            } catch (Exception e) {
                LOGGER.warn("agent-k capability snapshot persist failed", e);
            } finally {
                refreshing.set(false);
            }
        });
    }

    private AgentKCapabilityRefreshResultDto refreshNow(AgentKOutboundIdentity identity, boolean force) {
        LocalDateTime now = LocalDateTime.now();
        AgentKCapabilityRefreshResultDto result = emptyRefreshResult(now);
        try {
            ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/capabilities", null, null,
                    AgentKCapabilitiesDto.class, identity);
            if (!upstream.getStatusCode().is2xxSuccessful()
                    || !(upstream.getBody() instanceof AgentKCapabilitiesDto)) {
                LOGGER.warn("agent-k capabilities fetch failed httpStatus={}", upstream.getStatusCodeValue());
                result.setMessage("agent-k capabilities fetch failed: " + upstream.getStatusCodeValue());
                return result;
            }
            return applySnapshot((AgentKCapabilitiesDto) upstream.getBody(), force);
        } catch (Exception e) {
            LOGGER.warn("agent-k capabilities refresh failed", e);
            result.setMessage("agent-k capabilities refresh failed: " + e.getMessage());
            return result;
        }
    }

    private AgentKCapabilityRefreshResultDto applySnapshot(AgentKCapabilitiesDto body, boolean force) {
        LocalDateTime now = LocalDateTime.now();
        AgentKCapabilityRefreshResultDto result = emptyRefreshResult(now);
        List<AgentKCapabilityItemDto> items = itemsOf(body);
        Map<String, AgentKCapabilityCache> existing = existingByAgentkId();
        if (items.isEmpty() && !existing.isEmpty()) {
            LOGGER.warn("agent-k capabilities returned empty while cache has {} rows, skip DB update",
                    existing.size());
            result.setMessage("agent-k capabilities empty, keep cache");
            return result;
        }
        String snapshotHash = sha256(writeJson(body));
        result.setSnapshotHash(snapshotHash);
        result.setTotal(items.size());

        if (!force && snapshotHash.equals(snapshotHashRef.get())) {
            lastRefreshAtRef.set(now);
            result.setRefreshed(true);
            result.setChanged(false);
            result.setMessage("capabilities unchanged");
            LOGGER.info("agent-k capabilities refresh unchanged count={} hash={}", items.size(), snapshotHash);
            return result;
        }

        Set<String> seen = new HashSet<>();
        int added = 0;
        int updated = 0;
        for (AgentKCapabilityItemDto item : items) {
            if (item == null || !StringUtils.hasText(item.getId())) {
                continue;
            }
            String agentkId = item.getId().trim();
            seen.add(agentkId);
            AgentKCapabilityCache row = existing.get(agentkId);
            String schemaHash = sha256(writeJson(item));
            if (row == null) {
                capabilityMapper.insert(toEntity(item, schemaHash, now));
                added++;
            } else {
                boolean changed = !schemaHash.equals(row.getSchemaHash())
                        || !Boolean.TRUE.equals(row.getEnabled());
                if (changed) {
                    fillEntity(row, item, schemaHash, now);
                    capabilityMapper.updateById(row);
                    updated++;
                }
            }
        }

        int disabled = 0;
        for (AgentKCapabilityCache row : existing.values()) {
            if (Boolean.TRUE.equals(row.getEnabled()) && !seen.contains(row.getAgentkId())) {
                row.setEnabled(false);
                row.setUpdatedAt(now);
                capabilityMapper.updateById(row);
                disabled++;
            }
        }

        snapshotHashRef.set(snapshotHash);
        lastRefreshAtRef.set(now);
        result.setRefreshed(true);
        result.setChanged(added > 0 || updated > 0 || disabled > 0);
        result.setAdded(added);
        result.setUpdated(updated);
        result.setDisabled(disabled);
        result.setMessage("capabilities refreshed");
        LOGGER.info("agent-k capabilities refreshed added={} updated={} disabled={} total={}",
                added, updated, disabled, items.size());
        return result;
    }

    private static AgentKCapabilityRefreshResultDto emptyRefreshResult(LocalDateTime now) {
        AgentKCapabilityRefreshResultDto result = new AgentKCapabilityRefreshResultDto();
        result.setSkipped(false);
        result.setRefreshed(false);
        result.setChanged(false);
        result.setAdded(0);
        result.setUpdated(0);
        result.setDisabled(0);
        result.setRefreshedAt(now);
        return result;
    }

    private AgentKCapabilityCache toEntity(AgentKCapabilityItemDto item, String schemaHash, LocalDateTime now) {
        AgentKCapabilityCache row = new AgentKCapabilityCache();
        row.setCreatedAt(now);
        fillEntity(row, item, schemaHash, now);
        return row;
    }

    private void fillEntity(AgentKCapabilityCache row, AgentKCapabilityItemDto item,
            String schemaHash, LocalDateTime now) {
        row.setAgentkId(item.getId().trim());
        row.setKind(empty(item.getKind()));
        row.setName(resolveName(item));
        row.setDescription(empty(item.getDescription()));
        row.setInputSchemaJson(writeJson(item.getInputSchema()));
        row.setOutputSchemaJson(writeJson(item.getOutputSchema()));
        row.setUiSchemaJson(writeJson(toUiSchema(item.getInputSchema())));
        row.setToolRef(empty(item.getToolRef()));
        row.setPipelineRef(empty(item.getPipelineRef()));
        row.setToolName(item.getTool() == null ? "" : empty(item.getTool().getName()));
        row.setToolDescription(item.getTool() == null ? "" : empty(item.getTool().getDescription()));
        row.setToolMode(item.getTool() == null ? "" : empty(item.getTool().getMode()));
        row.setPipelineName(item.getPipeline() == null ? "" : empty(item.getPipeline().getName()));
        row.setPipelineDescription(item.getPipeline() == null ? "" : empty(item.getPipeline().getDescription()));
        row.setTimeoutSeconds(item.getTimeoutSeconds());
        row.setSchemaHash(schemaHash);
        row.setEnabled(true);
        row.setLastSeenAt(now);
        row.setUpdatedAt(now);
    }

    private AgentKCapabilitiesDto toRawDto(List<AgentKCapabilityCache> rows) {
        AgentKCapabilitiesDto dto = new AgentKCapabilitiesDto();
        List<AgentKCapabilityItemDto> items = new ArrayList<>();
        for (AgentKCapabilityCache row : rows) {
            AgentKCapabilityItemDto item = new AgentKCapabilityItemDto();
            item.setId(row.getAgentkId());
            item.setKind(row.getKind());
            item.setDescription(row.getDescription());
            item.setInputSchema(readJson(row.getInputSchemaJson()));
            item.setOutputSchema(readJson(row.getOutputSchemaJson()));
            item.setTimeoutSeconds(row.getTimeoutSeconds());
            item.setToolRef(row.getToolRef());
            item.setPipelineRef(row.getPipelineRef());
            AgentKCapabilitiesDto.AgentKCapabilityToolDto tool =
                    new AgentKCapabilitiesDto.AgentKCapabilityToolDto();
            tool.setName(row.getToolName());
            tool.setDescription(row.getToolDescription());
            tool.setMode(row.getToolMode());
            item.setTool(tool);
            AgentKCapabilitiesDto.AgentKCapabilityPipelineDto pipeline =
                    new AgentKCapabilitiesDto.AgentKCapabilityPipelineDto();
            pipeline.setName(row.getPipelineName());
            pipeline.setDescription(row.getPipelineDescription());
            item.setPipeline(pipeline);
            items.add(item);
        }
        dto.setCapabilities(items);
        return dto;
    }

    private AgentKCapabilityViewDto toView(AgentKCapabilityCache row) {
        AgentKCapabilityViewDto view = new AgentKCapabilityViewDto();
        view.setId(row.getAgentkId());
        view.setKind(row.getKind());
        view.setName(row.getName());
        view.setTitle(row.getName());
        view.setDescription(row.getDescription());
        view.setToolRef(row.getToolRef());
        view.setPipelineRef(row.getPipelineRef());
        view.setMode(row.getToolMode());
        view.setTimeoutSeconds(row.getTimeoutSeconds());
        view.setInputSchema(readJson(row.getInputSchemaJson()));
        view.setOutputSchema(readJson(row.getOutputSchemaJson()));
        view.setUiSchema(readJson(row.getUiSchemaJson()));
        view.setFields(toFields(view.getInputSchema()));
        return view;
    }

    private AgentKCapabilityViewDto toView(AgentKCapabilityItemDto item) {
        if (item == null || !StringUtils.hasText(item.getId())) {
            return null;
        }
        AgentKCapabilityViewDto view = new AgentKCapabilityViewDto();
        view.setId(item.getId().trim());
        view.setKind(empty(item.getKind()));
        String name = resolveName(item);
        view.setName(name);
        view.setTitle(name);
        view.setDescription(empty(item.getDescription()));
        view.setToolRef(empty(item.getToolRef()));
        view.setPipelineRef(empty(item.getPipelineRef()));
        view.setMode(item.getTool() == null ? "" : empty(item.getTool().getMode()));
        view.setTimeoutSeconds(item.getTimeoutSeconds());
        view.setInputSchema(item.getInputSchema());
        view.setOutputSchema(item.getOutputSchema());
        view.setUiSchema(toUiSchema(item.getInputSchema()));
        view.setFields(toFields(item.getInputSchema()));
        return view;
    }

    private static List<AgentKCapabilityItemDto> itemsOf(AgentKCapabilitiesDto live) {
        if (live == null || live.getCapabilities() == null) {
            return Collections.emptyList();
        }
        return live.getCapabilities();
    }

    private static boolean alreadyHasId(List<AgentKCapabilityViewDto> capabilities, String id) {
        if (!StringUtils.hasText(id)) {
            return false;
        }
        for (AgentKCapabilityViewDto item : capabilities) {
            if (item != null && id.equals(item.getId())) {
                return true;
            }
        }
        return false;
    }

    private static boolean matchesKind(String rowKind, String kind) {
        if (!StringUtils.hasText(kind)) {
            return true;
        }
        return kind.trim().equalsIgnoreCase(empty(rowKind));
    }

    private static boolean matchesWorkflowKey(AgentKCapabilityViewDto view, AgentKCapabilityItemDto item,
            String workflowKey) {
        if (!StringUtils.hasText(workflowKey) || view == null) {
            return true;
        }
        String key = stripPipelinePrefix(workflowKey.trim());
        if (!StringUtils.hasText(key)) {
            return false;
        }
        if (key.equals(view.getId()) || key.equals(view.getName()) || key.equals(view.getTitle())) {
            return true;
        }
        String idLeaf = leafOf(view.getId());
        if (key.equals(idLeaf) || key.equals(leafOf(view.getName()))) {
            return true;
        }
        if (item != null && item.getPipeline() != null) {
            String pipelineName = empty(item.getPipeline().getName());
            if (key.equals(pipelineName) || key.equals(leafOf(pipelineName))) {
                return true;
            }
        }
        String ref = empty(view.getPipelineRef());
        return key.equals(ref) || key.equals(stripPipelinePrefix(ref))
                || ("pipeline:" + key).equals(ref);
    }

    private static String leafOf(String value) {
        if (!StringUtils.hasText(value)) {
            return "";
        }
        String trimmed = value.trim();
        int dot = trimmed.lastIndexOf('.');
        return dot >= 0 ? trimmed.substring(dot + 1) : trimmed;
    }

    private ObjectNode toUiSchema(JsonNode inputSchema) {
        ObjectNode root = objectMapper.createObjectNode();
        ArrayNode fields = root.putArray("fields");
        for (FieldView field : toFields(inputSchema)) {
            fields.add(objectMapper.valueToTree(field));
        }
        return root;
    }

    private List<FieldView> toFields(JsonNode schema) {
        if (schema == null || !schema.isObject() || !schema.path("properties").isObject()) {
            return Collections.emptyList();
        }
        Set<String> required = new HashSet<>();
        JsonNode requiredNode = schema.get("required");
        if (requiredNode != null && requiredNode.isArray()) {
            for (JsonNode item : requiredNode) {
                if (item.isTextual()) {
                    required.add(item.asText());
                }
            }
        }
        List<FieldView> fields = new ArrayList<>();
        schema.path("properties").fields().forEachRemaining(entry -> {
            FieldView field = toField(entry.getKey(), entry.getValue(), required.contains(entry.getKey()));
            fields.add(field);
        });
        return fields;
    }

    private FieldView toField(String name, JsonNode schema, boolean required) {
        FieldView field = new FieldView();
        field.setName(name);
        field.setLabel(labelOf(name, schema));
        field.setDescription(schema.path("description").asText(""));
        field.setType(schema.path("type").asText("string"));
        field.setWidget(widgetOf(name, schema));
        field.setRequired(required);
        field.setSchema(schema);
        if (schema.has("default")) {
            field.setDefaultValue(schema.get("default"));
        }
        if (schema.has("enum") && schema.get("enum").isArray()) {
            List<String> values = new ArrayList<>();
            for (JsonNode value : schema.get("enum")) {
                values.add(value.asText());
            }
            field.setEnumValues(values);
        }
        if ("array".equals(field.getType()) && schema.path("items").isObject()) {
            field.setItems(toField(name + "[]", schema.path("items"), false));
        }
        return field;
    }

    private String widgetOf(String name, JsonNode schema) {
        if (schema.has("enum")) {
            return "select";
        }
        String type = schema.path("type").asText("string");
        String lowerName = name == null ? "" : name.toLowerCase();
        if ("array".equals(type)) {
            JsonNode items = schema.path("items");
            if (items.isObject() && "string".equals(items.path("type").asText())
                    && (lowerName.contains("image") || lowerName.contains("img"))) {
                return "image-url-list";
            }
            return "array";
        }
        if ("boolean".equals(type)) {
            return "switch";
        }
        if ("number".equals(type) || "integer".equals(type)) {
            return "number";
        }
        if ("object".equals(type)) {
            return "json-editor";
        }
        if (lowerName.contains("prompt") || lowerName.contains("description")
                || lowerName.contains("content")) {
            return "textarea";
        }
        return "input";
    }

    private String labelOf(String name, JsonNode schema) {
        String description = schema.path("description").asText("");
        if (StringUtils.hasText(description) && description.length() <= 24) {
            return description;
        }
        return name;
    }

    private AgentKCapabilityCacheMetaDto liveMeta() {
        AgentKCapabilityCacheMetaDto meta = new AgentKCapabilityCacheMetaDto();
        meta.setSource("live");
        meta.setStale(false);
        meta.setRefreshing(refreshing.get());
        meta.setTtlSeconds(0L);
        meta.setLastRefreshAt(LocalDateTime.now());
        meta.setRefreshStatus("live");
        return meta;
    }

    private LocalDateTime resolveLastRefreshAt() {
        LocalDateTime memory = lastRefreshAtRef.get();
        if (memory != null) {
            return memory;
        }
        return deriveLastSeenAtFromDb();
    }

    private boolean isExpired() {
        LocalDateTime lastRefreshAt = resolveLastRefreshAt();
        if (lastRefreshAt == null) {
            return true;
        }
        return Duration.between(lastRefreshAt, LocalDateTime.now()).getSeconds() > CACHE_TTL_SECONDS;
    }

    private LocalDateTime deriveLastSeenAtFromDb() {
        List<AgentKCapabilityCache> rows = listEnabled(null);
        LocalDateTime max = null;
        for (AgentKCapabilityCache row : rows) {
            LocalDateTime candidate = row.getLastSeenAt() != null ? row.getLastSeenAt() : row.getUpdatedAt();
            if (candidate != null && (max == null || candidate.isAfter(max))) {
                max = candidate;
            }
        }
        return max;
    }

    private List<AgentKCapabilityCache> listEnabled(String kind) {
        LambdaQueryWrapper<AgentKCapabilityCache> q = new LambdaQueryWrapper<AgentKCapabilityCache>()
                .eq(AgentKCapabilityCache::getEnabled, true)
                .orderByAsc(AgentKCapabilityCache::getId);
        if (StringUtils.hasText(kind)) {
            q.eq(AgentKCapabilityCache::getKind, kind.trim());
        }
        return capabilityMapper.selectList(q);
    }

    private AgentKCapabilityCache findEnabled(String agentkId) {
        if (!StringUtils.hasText(agentkId)) {
            return null;
        }
        return capabilityMapper.selectOne(new LambdaQueryWrapper<AgentKCapabilityCache>()
                .eq(AgentKCapabilityCache::getAgentkId, agentkId.trim())
                .eq(AgentKCapabilityCache::getEnabled, true)
                .last("LIMIT 1"));
    }

    private AgentKPipelineBindingDto toPipelineBinding(AgentKCapabilityCache cap) {
        if (cap == null || !StringUtils.hasText(cap.getAgentkId())) {
            return null;
        }
        AgentKPipelineBindingDto binding = new AgentKPipelineBindingDto();
        binding.setPipelineRef(StringUtils.hasText(cap.getPipelineRef())
                ? cap.getPipelineRef().trim()
                : "pipeline:" + cap.getAgentkId().trim());
        binding.setPipelineName(resolvePipelineName(cap));
        return binding;
    }

    private String resolvePipelineName(AgentKCapabilityCache cap) {
        if (StringUtils.hasText(cap.getPipelineName())) {
            return cap.getPipelineName().trim();
        }
        if (StringUtils.hasText(cap.getName())) {
            return cap.getName().trim();
        }
        String agentkId = cap.getAgentkId().trim();
        int dot = agentkId.lastIndexOf('.');
        return dot >= 0 ? agentkId.substring(dot + 1) : agentkId;
    }

    /**
     * 把启动 pipeline 的入参归一成 profile.pipelines[].name。
     * <p>
     * 前端常传 workflowKey / capability id（如 {@code agentk.generate_brand_ad}）或
     * {@code pipeline:agentk.xxx}；agent-k Start 只认 profile 里的短 name
     * （如 {@code generate_brand_ad}）。
     */
    public String resolveStartPipelineName(String pipeline) {
        if (!StringUtils.hasText(pipeline)) {
            return pipeline;
        }
        String key = pipeline.trim();
        if (key.startsWith("pipeline:")) {
            key = key.substring("pipeline:".length()).trim();
        }
        if (!StringUtils.hasText(key)) {
            return pipeline.trim();
        }
        AgentKCapabilityCache byId = findEnabled(key);
        if (byId != null) {
            return resolvePipelineName(byId);
        }
        for (AgentKCapabilityCache row : listEnabled(null)) {
            if (row == null) {
                continue;
            }
            if (key.equals(row.getPipelineName()) || key.equals(row.getName())) {
                return resolvePipelineName(row);
            }
            String ref = StringUtils.hasText(row.getPipelineRef())
                    ? row.getPipelineRef().trim()
                    : "";
            if (("pipeline:" + key).equals(ref) || key.equals(stripPipelinePrefix(ref))) {
                return resolvePipelineName(row);
            }
        }
        // 缓存未命中时：agentk.generate_brand_ad → generate_brand_ad
        int dot = key.lastIndexOf('.');
        if (dot >= 0 && dot < key.length() - 1) {
            return key.substring(dot + 1);
        }
        return key;
    }

    private static String stripPipelinePrefix(String ref) {
        if (!StringUtils.hasText(ref)) {
            return "";
        }
        String value = ref.trim();
        return value.startsWith("pipeline:") ? value.substring("pipeline:".length()).trim() : value;
    }

    /**
     * 解析 official/default {@code workflowKey}：优先 agentk_id 精确匹配，再按 name / pipeline_name。
     */
    private AgentKCapabilityCache findByWorkflowKey(String workflowKey) {
        if (!StringUtils.hasText(workflowKey)) {
            return null;
        }
        String key = workflowKey.trim();
        AgentKCapabilityCache exact = findEnabled(key);
        if (exact != null) {
            return exact;
        }
        for (AgentKCapabilityCache row : listEnabled(null)) {
            if (key.equals(row.getAgentkId()) || key.equals(row.getName())
                    || key.equals(row.getPipelineName())) {
                return row;
            }
        }
        return null;
    }

    private boolean matchesKind(AgentKCapabilityCache row, String kind) {
        if (!StringUtils.hasText(kind)) {
            return true;
        }
        return kind.trim().equalsIgnoreCase(empty(row.getKind()));
    }

    private Map<String, AgentKCapabilityCache> existingByAgentkId() {
        Map<String, AgentKCapabilityCache> result = new LinkedHashMap<>();
        List<AgentKCapabilityCache> rows = capabilityMapper.selectList(new LambdaQueryWrapper<>());
        for (AgentKCapabilityCache row : rows) {
            result.put(row.getAgentkId(), row);
        }
        return result;
    }

    private String resolveName(AgentKCapabilityItemDto item) {
        if (item.getPipeline() != null && StringUtils.hasText(item.getPipeline().getName())) {
            return item.getPipeline().getName();
        }
        if (item.getTool() != null && StringUtils.hasText(item.getTool().getName())) {
            return item.getTool().getName();
        }
        return empty(item.getId());
    }

    private JsonNode readJson(String json) {
        if (!StringUtils.hasText(json)) {
            return null;
        }
        try {
            return objectMapper.readTree(json);
        } catch (Exception e) {
            return null;
        }
    }

    private String writeJson(Object value) {
        if (value == null) {
            return null;
        }
        try {
            return objectMapper.writeValueAsString(value);
        } catch (Exception e) {
            return null;
        }
    }

    private static String sha256(String value) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            byte[] hash = digest.digest(empty(value).getBytes(StandardCharsets.UTF_8));
            StringBuilder hex = new StringBuilder();
            for (byte b : hash) {
                String s = Integer.toHexString(0xff & b);
                if (s.length() == 1) {
                    hex.append('0');
                }
                hex.append(s);
            }
            return hex.toString();
        } catch (Exception e) {
            return String.valueOf(empty(value).hashCode());
        }
    }

    private static String empty(String value) {
        return value == null ? "" : value;
    }
}
