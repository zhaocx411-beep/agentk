package com.kuaishou.agent.operation.backend.keffects.task;

import java.util.Locale;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.agentk.AgentKIdentityResolver;
import com.kuaishou.agent.operation.backend.agentk.AgentKOutboundIdentity;
import com.kuaishou.agent.operation.backend.keffects.KEffectsClient;
import com.kuaishou.agent.operation.backend.keffects.KEffectsClient.KEffectsUpstreamException;
import com.kuaishou.agent.operation.backend.keffects.KEffectsProperties;
import com.kuaishou.agent.operation.backend.keffects.dto.KEffectsCreateTaskRequest;
import com.kuaishou.agent.operation.backend.keffects.task.adapter.ClothingDupeTaskAdapter;
import com.kuaishou.agent.operation.backend.keffects.task.adapter.GoodsScraperTaskAdapter;
import com.kuaishou.agent.operation.backend.keffects.task.adapter.GoodsStudioTaskAdapter;
import com.kuaishou.agent.operation.backend.keffects.task.adapter.KlingAigcEngineUpstream;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;

/**
 * KEffects 任务通用编排：taskType 分发、上游调用、错误映射、执行记录与资产落库。
 * <p>
 * 落库统一走 {@link KEffectsTaskAssetService}，不依赖各 adapter 是否实现 persist。
 */
@Service
public class KEffectsTaskFacade {

    private static final Logger LOGGER = LoggerFactory.getLogger(KEffectsTaskFacade.class);

    private final KEffectsClient kEffectsClient;
    private final KEffectsProperties properties;
    private final KEffectsTaskRegistry registry;
    private final AgentKIdentityResolver identityResolver;
    private final KEffectsTaskAssetService assetService;

    public KEffectsTaskFacade(KEffectsClient kEffectsClient,
            KEffectsProperties properties,
            KEffectsTaskRegistry registry,
            AgentKIdentityResolver identityResolver,
            KEffectsTaskAssetService assetService) {
        this.kEffectsClient = kEffectsClient;
        this.properties = properties;
        this.registry = registry;
        this.identityResolver = identityResolver;
        this.assetService = assetService;
    }

    public Message<JsonNode> createTask(KEffectsCreateTaskRequest request) {
        try {
            KEffectsCreateTaskRequest normalized = request == null ? new KEffectsCreateTaskRequest() : request;
            if (!StringUtils.hasText(normalized.getTaskType())) {
                return Message.error(ResultCode.PARAM_REQUIRED, "taskType is required");
            }
            KEffectsTaskAdapter adapter = registry.require(normalized.getTaskType());
            adapter.validateCreate(normalized);
            adapter.normalizeCreate(normalized);
            JsonNode upstream = kEffectsClient.createTask(normalized, adapter.upstreamBaseUrl());
            JsonNode body = assetService.enrichWithBlobUrls(adapter.enrichTask(upstream));
            tryPersist(() -> {
                int n = assetService.persistAfterCreate(body, normalized, currentUserId(), adapter.assetProfile());
                LOGGER.info("keffects create persist done taskType={} assetCount={}",
                        adapter.taskType(), n);
            });
            return Message.ok(body);
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, e.getMessage());
        } catch (KEffectsUpstreamException e) {
            LOGGER.warn("keffects create failed status={} body={}", e.getStatus(), e.getResponseBody());
            return Message.error(mapUpstreamCode(e.getStatus()),
                    "keffects create failed: " + truncate(e.getResponseBody(), 500));
        } catch (Exception e) {
            LOGGER.error("keffects create failed", e);
            return Message.error(ResultCode.RELY_SERVICE_BUSY, "keffects unavailable: " + e.getMessage());
        }
    }

    public Message<JsonNode> getTask(String taskId, String taskType) {
        if (!StringUtils.hasText(taskId)) {
            return Message.error(ResultCode.PARAM_REQUIRED, "taskId is required");
        }
        String id = taskId.trim();
        try {
            String baseUrl = resolveUpstreamBaseUrl(id, taskType);
            LOGGER.info("keffects get routed taskId={} taskType={} baseUrl={}", id, taskType, baseUrl);
            JsonNode upstream = kEffectsClient.getTask(id, baseUrl);
            KEffectsTaskAdapter adapter = resolveAdapter(upstream, taskType, id);
            if (adapter == null) {
                LOGGER.warn("keffects get persist skipped: no adapter taskId={} taskType={}", id, taskType);
                return Message.ok(upstream);
            }
            JsonNode body = assetService.enrichWithBlobUrls(adapter.enrichTask(upstream));
            tryPersist(() -> {
                int n = assetService.persistAfterGet(body, id, currentUserId(), adapter.assetProfile());
                LOGGER.info("keffects get persist done taskId={} taskType={} assetCount={}",
                        id, adapter.taskType(), n);
            });
            return Message.ok(body);
        } catch (KEffectsUpstreamException e) {
            LOGGER.warn("keffects get failed taskId={} status={} body={}",
                    taskId, e.getStatus(), e.getResponseBody());
            return Message.error(mapUpstreamCode(e.getStatus()),
                    "keffects get failed: " + truncate(e.getResponseBody(), 500));
        } catch (Exception e) {
            LOGGER.error("keffects get failed taskId={}", taskId, e);
            return Message.error(ResultCode.RELY_SERVICE_BUSY, "keffects unavailable: " + e.getMessage());
        }
    }

    public Message<JsonNode> cancelTask(String taskId, String taskType) {
        if (!StringUtils.hasText(taskId)) {
            return Message.error(ResultCode.PARAM_REQUIRED, "taskId is required");
        }
        String id = taskId.trim();
        if (isKlingAigcEngineTaskId(id)) {
            return Message.error(ResultCode.OPERATION_UNSUPPORTED,
                    "kling-aigc-engine 任务（ecs-/egs- taskId）不支持取消；"
                            + "仅 clothing-dupe（UUID taskId）可走取消接口");
        }
        try {
            String baseUrl = resolveUpstreamBaseUrl(id, taskType);
            LOGGER.info("keffects cancel routed taskId={} taskType={} baseUrl={}", id, taskType, baseUrl);
            return Message.ok(kEffectsClient.cancelTask(id, baseUrl));
        } catch (KEffectsUpstreamException e) {
            LOGGER.warn("keffects cancel failed taskId={} status={} body={}",
                    taskId, e.getStatus(), e.getResponseBody());
            ResultCode code = mapCancelUpstreamCode(e.getStatus(), e.getResponseBody());
            return Message.error(code, formatCancelFailureMessage(e.getResponseBody()));
        } catch (Exception e) {
            LOGGER.error("keffects cancel failed taskId={}", taskId, e);
            return Message.error(ResultCode.RELY_SERVICE_BUSY, "keffects unavailable: " + e.getMessage());
        }
    }

    /**
     * 查询/取消上游路由：
     * <ul>
     *   <li>{@code ecs-*} / {@code egs-*} taskId → {@code kling-aigc-engine}</li>
     *   <li>显式 {@code taskType} → 对应 adapter 域名</li>
     *   <li>其余 taskId（如 clothing-dupe UUID）→ {@code kling-aigc-engine}</li>
     * </ul>
     */
    private String resolveUpstreamBaseUrl(String taskId, String taskType) {
        if (isKlingAigcEngineTaskId(taskId)) {
            return upstreamBaseUrlForKlingAigcEngine();
        }
        if (StringUtils.hasText(taskType)) {
            KEffectsTaskAdapter adapter = registry.find(taskType.trim());
            if (adapter != null) {
                return adapter.upstreamBaseUrl();
            }
        }
        return upstreamBaseUrlForClothingDupe();
    }

    private boolean isKlingAigcEngineTaskId(String taskId) {
        return inferTaskTypeFromTaskId(taskId) != null;
    }

    private String upstreamBaseUrlForKlingAigcEngine() {
        KEffectsTaskAdapter adapter = registry.find(GoodsScraperTaskAdapter.TASK_TYPE);
        if (adapter != null) {
            return adapter.upstreamBaseUrl();
        }
        adapter = registry.find(GoodsStudioTaskAdapter.TASK_TYPE);
        if (adapter != null) {
            return adapter.upstreamBaseUrl();
        }
        return KlingAigcEngineUpstream.baseUrl();
    }

    private String upstreamBaseUrlForClothingDupe() {
        KEffectsTaskAdapter adapter = registry.find(ClothingDupeTaskAdapter.TASK_TYPE);
        if (adapter != null) {
            return adapter.upstreamBaseUrl();
        }
        return KlingAigcEngineUpstream.baseUrl();
    }

    /**
     * 解析落库用 adapter：上游 taskType → 查询参数 taskType → taskId 前缀 → clothing-dupe。
     */
    private KEffectsTaskAdapter resolveAdapter(JsonNode upstream, String taskType, String taskId) {
        KEffectsTaskAdapter adapter = registry.find(extractTaskType(upstream));
        if (adapter != null) {
            return adapter;
        }
        if (StringUtils.hasText(taskType)) {
            adapter = registry.find(taskType.trim());
            if (adapter != null) {
                return adapter;
            }
        }
        adapter = registry.find(inferTaskTypeFromTaskId(taskId));
        if (adapter != null) {
            return adapter;
        }
        return registry.find(ClothingDupeTaskAdapter.TASK_TYPE);
    }

    /** {@code ecs-*} → goods-scraper；{@code egs-*} → goods-studio。 */
    static String inferTaskTypeFromTaskId(String taskId) {
        if (!StringUtils.hasText(taskId)) {
            return null;
        }
        String id = taskId.trim().toLowerCase(Locale.ROOT);
        if (id.startsWith("ecs-")) {
            return GoodsScraperTaskAdapter.TASK_TYPE;
        }
        if (id.startsWith("egs-")) {
            return GoodsStudioTaskAdapter.TASK_TYPE;
        }
        return null;
    }

    private String currentUserId() {
        AgentKOutboundIdentity identity = identityResolver.resolve();
        return identity != null && StringUtils.hasText(identity.getUserId())
                ? identity.getUserId().trim() : "";
    }

    private void tryPersist(Runnable action) {
        try {
            action.run();
        } catch (Exception e) {
            LOGGER.error("keffects persist failed", e);
        }
    }

    private static String extractTaskType(JsonNode raw) {
        JsonNode task = unwrapTask(raw);
        if (task == null || !task.isObject()) {
            return null;
        }
        for (String field : new String[] {"taskType", "task_type", "type"}) {
            JsonNode node = task.get(field);
            if (node != null && node.isTextual() && StringUtils.hasText(node.asText())) {
                return node.asText().trim();
            }
        }
        return null;
    }

    private static JsonNode unwrapTask(JsonNode raw) {
        if (raw == null || raw.isNull() || raw.isMissingNode()) {
            return raw;
        }
        if (raw.hasNonNull("taskId") || raw.hasNonNull("task_id") || raw.has("output")
                || raw.hasNonNull("taskType") || raw.hasNonNull("task_type")) {
            return raw;
        }
        JsonNode data = raw.get("data");
        if (data != null && data.isObject()) {
            return data;
        }
        JsonNode result = raw.get("result");
        if (result != null && result.isObject()) {
            return result;
        }
        return raw;
    }

    private static ResultCode mapUpstreamCode(int status) {
        if (status == 400 || status == 422) {
            return ResultCode.BAD_REQUEST;
        }
        if (status == 404) {
            return ResultCode.NOT_FOUND;
        }
        if (status == 401 || status == 403) {
            return ResultCode.UNAUTHORIZED;
        }
        return ResultCode.RELY_SERVICE_BUSY;
    }

    private static ResultCode mapCancelUpstreamCode(int status, String responseBody) {
        if (responseBody != null && responseBody.contains("CANCEL_NOT_SUPPORTED")) {
            return ResultCode.OPERATION_UNSUPPORTED;
        }
        return mapUpstreamCode(status);
    }

    private static String formatCancelFailureMessage(String responseBody) {
        if (responseBody != null && responseBody.contains("CANCEL_NOT_SUPPORTED")) {
            return "该任务不支持取消（kling-aigc-engine ecs-/egs- 任务仅支持查询）";
        }
        return "keffects cancel failed: " + truncate(responseBody, 500);
    }

    private static String truncate(String raw, int max) {
        if (raw == null) {
            return "";
        }
        String text = raw.trim();
        if (text.length() <= max) {
            return text;
        }
        return text.substring(0, max) + "...";
    }
}
