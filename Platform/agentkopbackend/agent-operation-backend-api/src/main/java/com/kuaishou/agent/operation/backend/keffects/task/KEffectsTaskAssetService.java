package com.kuaishou.agent.operation.backend.keffects.task;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.zip.CRC32;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.kuaishou.agent.operation.backend.agentk.AgentKMediaArtifactExtractor;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMediaArtifactDto;
import com.kuaishou.agent.operation.backend.keffects.dto.KEffectsCreateTaskRequest;
import com.kuaishou.agent.operation.backend.platform.service.BlobStoreUrlService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.SyncedAsset;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.SyncedPipelineExec;
import com.kuaishou.agent.operation.backend.platform.support.ChatAssetSources;

/**
 * KEffects 固定入参/出参 → {@code aop_pipeline_exec}，媒体产物 → {@code aop_asset}（{@code source_kind=api}）。
 */
@Service
public class KEffectsTaskAssetService {

    private static final Logger LOGGER = LoggerFactory.getLogger(KEffectsTaskAssetService.class);

    /** 外部 HTTP API 任务（与 agent_loop / pipeline 区分）。 */
    public static final String SOURCE_KIND = "api";
    /** 写入 {@code aop_pipeline_exec.profile_version}，便于筛选 API 任务。 */
    private static final String PROFILE_VERSION_API = "api";

    private final ChatPersistService chatPersistService;
    private final ObjectMapper objectMapper;
    private final BlobStoreUrlService blobStoreUrlService;

    public KEffectsTaskAssetService(ChatPersistService chatPersistService,
            ObjectMapper objectMapper,
            BlobStoreUrlService blobStoreUrlService) {
        this.chatPersistService = chatPersistService;
        this.objectMapper = objectMapper;
        this.blobStoreUrlService = blobStoreUrlService;
    }

    public int persistAfterCreate(JsonNode upstream, KEffectsCreateTaskRequest request,
            String userId, AssetProfile profile) {
        JsonNode fallbackInput = request == null ? null : request.getInput();
        return persistTask(upstream, userId, fallbackInput, null, profile);
    }

    public int persistAfterGet(JsonNode upstream, String pathTaskId, String userId, AssetProfile profile) {
        return persistTask(upstream, userId, null, pathTaskId, profile);
    }

    public JsonNode enrichWithBlobUrls(JsonNode upstream) {
        return blobStoreUrlService.enrichTaskOutput(upstream);
    }

    private int persistTask(JsonNode raw, String userId, JsonNode fallbackInput,
            String pathTaskId, AssetProfile profile) {
        AssetProfile effectiveProfile = profile == null ? AssetProfile.of("api") : profile;
        JsonNode task = unwrapTask(raw);
        if (task == null || task.isNull() || task.isMissingNode()) {
            if (!StringUtils.hasText(pathTaskId) && fallbackInput == null) {
                return 0;
            }
            task = objectMapper.createObjectNode();
        }
        String taskId = extractTaskId(task);
        if (!StringUtils.hasText(taskId)) {
            taskId = StringUtils.hasText(pathTaskId) ? pathTaskId.trim() : null;
        }
        if (!StringUtils.hasText(taskId) && fallbackInput != null) {
            // create 响应偶发无 taskId：仍无法幂等落库
            LOGGER.warn("keffects persist skip: taskId missing taskType={}", effectiveProfile.taskType);
            return 0;
        }
        if (!StringUtils.hasText(taskId)) {
            LOGGER.warn("keffects persist skip: taskId missing");
            return 0;
        }

        JsonNode argsNode = resolveArgsNode(task, fallbackInput);
        JsonNode outputNode = firstPresent(task, "output", "result", "results");
        String status = extractStatus(task);
        persistExec(userId, taskId, effectiveProfile.taskType, status, argsNode, outputNode, task);
        LOGGER.info("keffects exec upserted taskId={} taskType={} status={} hasArgs={} hasOutput={}",
                taskId, effectiveProfile.taskType, status,
                argsNode != null && !argsNode.isNull(),
                outputNode != null && !outputNode.isNull());

        if (isFailedStatus(status)) {
            LOGGER.info("keffects exec persisted (failed, skip assets) taskId={} taskType={} status={}",
                    taskId, effectiveProfile.taskType, status);
            return 0;
        }

        String promptText = serializeQuietly(argsNode);
        List<SyncedAsset> assets = new ArrayList<>();
        for (ResolvedOutput out : extractBlobOutputs(task, effectiveProfile)) {
            String blobUrl = blobStoreUrlService.resolveUrl(out.bucket, out.key);
            if (!StringUtils.hasText(blobUrl)) {
                LOGGER.warn("keffects skip asset: blobUrl 签发失败 field={} bucket={} key={}",
                        out.field, out.bucket, out.key);
                continue;
            }
            assets.add(toAsset(effectiveProfile, taskId, promptText, out.assetType, blobUrl, out.field));
        }
        if (assets.isEmpty()) {
            for (AgentKMediaArtifactDto artifact : extractHttpMedia(task)) {
                if (artifact == null || !StringUtils.hasText(artifact.getUrl())) {
                    continue;
                }
                assets.add(toAsset(effectiveProfile, taskId, promptText,
                        artifact.getType(), artifact.getUrl().trim(), null));
            }
        }
        if (assets.isEmpty()) {
            LOGGER.info("keffects asset skip: no output media taskId={} status={} hasOutput={}",
                    taskId, status, task.has("output"));
            return 0;
        }
        long sessionId = sessionIdFromTaskId(taskId);
        int n = chatPersistService.upsertStandaloneAssets(userId, "", sessionId, taskId, assets);
        LOGGER.info("keffects asset persisted userId={} taskId={} sessionId={} taskType={} count={}",
                userId, taskId, sessionId, effectiveProfile.taskType, n);
        return n;
    }

    private void persistExec(String userId, String taskId, String pipelineName, String status,
            JsonNode args, JsonNode output, JsonNode task) {
        SyncedPipelineExec snapshot = new SyncedPipelineExec();
        snapshot.setAgentkExecId(taskId);
        snapshot.setPipelineName(pipelineName);
        snapshot.setStatus(StringUtils.hasText(status) ? status : "running");
        snapshot.setArgs(args);
        snapshot.setOutput(output);
        snapshot.setProfileVersion(PROFILE_VERSION_API);
        snapshot.setError(extractError(task));
        snapshot.setStartedAt(firstText(task, "createdAt", "created_at", "startedAt", "started_at"));
        snapshot.setFinishedAt(firstText(task, "finishedAt", "finished_at", "completedAt", "completed_at"));
        long sessionId = sessionIdFromTaskId(taskId);
        try {
            chatPersistService.upsertStandalonePipelineExec(userId, "", sessionId, taskId, snapshot);
        } catch (Exception e) {
            LOGGER.warn("keffects pipeline exec persist failed taskId={} reason={}", taskId, e.toString());
        }
    }

    private JsonNode resolveArgsNode(JsonNode task, JsonNode fallbackInput) {
        JsonNode fromTask = extractInputNode(task);
        if (fromTask != null) {
            return fromTask;
        }
        return fallbackInput;
    }

    private JsonNode extractInputNode(JsonNode task) {
        JsonNode input = firstPresent(task, "input", "request");
        if (input == null) {
            return null;
        }
        if (input.isObject()) {
            JsonNode nested = input.get("input");
            if (nested != null && nested.isObject()) {
                return nested;
            }
        }
        return input;
    }

    private static String extractError(JsonNode task) {
        for (String field : new String[] {"error", "errorMessage", "error_message", "message", "failReason"}) {
            JsonNode node = task.get(field);
            if (node == null || node.isNull()) {
                continue;
            }
            if (node.isTextual() && StringUtils.hasText(node.asText())) {
                String text = node.asText().trim();
                if ("ok".equalsIgnoreCase(text) || "success".equalsIgnoreCase(text)) {
                    continue;
                }
                return text;
            }
            if (node.isObject()) {
                String nested = firstText(node, "message", "msg", "detail");
                if (StringUtils.hasText(nested)) {
                    return nested.trim();
                }
            }
        }
        return null;
    }

    private SyncedAsset toAsset(AssetProfile profile, String taskId, String promptText,
            String assetType, String url, String outputField) {
        SyncedAsset asset = new SyncedAsset();
        asset.setAssetType(StringUtils.hasText(assetType) ? assetType.trim() : "video");
        asset.setAssetUrl(url);
        asset.setPromptText(promptText);
        asset.setSourceKind(SOURCE_KIND);
        asset.setSource(ChatAssetSources.ONLINE);
        asset.setAgentName(profile.getTaskType());
        asset.setSourceName(profile.getTaskType());
        asset.setWorkflowExecId(taskId);
        return asset;
    }

    private List<ResolvedOutput> extractBlobOutputs(JsonNode task, AssetProfile profile) {
        JsonNode output = firstPresent(task, "output", "result", "results");
        if (output == null || !output.isObject()) {
            return new ArrayList<>();
        }
        Map<String, ResolvedOutput> byBlob = new LinkedHashMap<>();
        addBlobOutput(byBlob, "output", output);
        for (String field : profile.outputFieldPriority) {
            addBlobOutput(byBlob, field, output.get(field));
        }
        Iterator<Map.Entry<String, JsonNode>> fields = output.fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> e = fields.next();
            addBlobOutput(byBlob, e.getKey(), e.getValue());
        }
        return new ArrayList<>(byBlob.values());
    }

    private static void addBlobOutput(Map<String, ResolvedOutput> byBlob, String field, JsonNode node) {
        if (node == null || node.isNull() || node.isMissingNode()) {
            return;
        }
        if (node.isArray()) {
            for (JsonNode item : node) {
                addBlobOutput(byBlob, field, item);
            }
            return;
        }
        if (!node.isObject()) {
            return;
        }
        String bucket = text(node, "bucket");
        String key = text(node, "key");
        if (!StringUtils.hasText(bucket) || !StringUtils.hasText(key)) {
            Iterator<Map.Entry<String, JsonNode>> nested = node.fields();
            while (nested.hasNext()) {
                Map.Entry<String, JsonNode> e = nested.next();
                addBlobOutput(byBlob, StringUtils.hasText(field) ? field : e.getKey(), e.getValue());
            }
            return;
        }
        String dedupe = bucket.trim() + "\n" + key.trim();
        byBlob.putIfAbsent(dedupe, new ResolvedOutput(field, bucket.trim(), key.trim(), inferAssetType(field, key)));
    }

    private List<AgentKMediaArtifactDto> extractHttpMedia(JsonNode task) {
        JsonNode output = firstPresent(task, "output", "result", "results", "data");
        List<AgentKMediaArtifactDto> fromOutput = AgentKMediaArtifactExtractor.extract(
                output != null ? output : task);
        List<AgentKMediaArtifactDto> all = new ArrayList<>(fromOutput);
        collectFlatUrlMedia(all, output != null ? output : task);
        return AgentKMediaArtifactExtractor.filterPublishable(all);
    }

    private void collectFlatUrlMedia(List<AgentKMediaArtifactDto> out, JsonNode node) {
        if (node == null || !node.isObject()) {
            return;
        }
        for (String field : new String[] {"videoUrl", "video_url", "imageUrl", "image_url", "url", "blobUrl"}) {
            addUrlMedia(out, node.get(field));
        }
        addUrlMedia(out, node.get("refImage"));
        addUrlMedia(out, node.get("ref_image"));
    }

    private static void addUrlMedia(List<AgentKMediaArtifactDto> out, JsonNode node) {
        if (node == null || node.isNull()) {
            return;
        }
        if (node.isArray()) {
            ArrayNode array = (ArrayNode) node;
            for (JsonNode item : array) {
                addUrlMedia(out, item);
            }
            return;
        }
        if (!node.isTextual() || !StringUtils.hasText(node.asText())) {
            return;
        }
        String url = node.asText().trim();
        AgentKMediaArtifactDto dto = new AgentKMediaArtifactDto();
        dto.setUrl(url);
        dto.setType(inferAssetType("", url));
        out.add(dto);
    }

    public static JsonNode unwrapTask(JsonNode raw) {
        if (raw == null || raw.isNull() || raw.isMissingNode()) {
            return raw;
        }
        if (raw.hasNonNull("taskId") || raw.hasNonNull("task_id") || raw.has("output")
                || raw.hasNonNull("taskType") || raw.hasNonNull("task_type")
                || raw.hasNonNull("status") || raw.hasNonNull("state")) {
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

    public static String extractTaskId(JsonNode task) {
        for (String field : new String[] {"taskId", "task_id", "id"}) {
            JsonNode node = task.get(field);
            if (node != null && node.isTextual() && StringUtils.hasText(node.asText())) {
                return node.asText().trim();
            }
            if (node != null && node.isNumber()) {
                return String.valueOf(node.asLong());
            }
        }
        JsonNode data = task.get("data");
        if (data != null && data.isObject()) {
            return extractTaskId(data);
        }
        return null;
    }

    public static String extractStatus(JsonNode task) {
        for (String field : new String[] {"status", "taskStatus", "task_status", "state"}) {
            JsonNode node = task.get(field);
            if (node != null && node.isTextual() && StringUtils.hasText(node.asText())) {
                return node.asText().trim();
            }
        }
        JsonNode data = task.get("data");
        if (data != null && data.isObject()) {
            return extractStatus(data);
        }
        return "";
    }

    static boolean isFailedStatus(String status) {
        if (!StringUtils.hasText(status)) {
            return false;
        }
        String n = status.trim().toLowerCase(Locale.ROOT);
        return n.contains("fail") || n.contains("error") || n.contains("cancel");
    }

    static long sessionIdFromTaskId(String taskId) {
        String t = taskId.trim();
        if (t.matches("\\d{1,18}")) {
            try {
                long parsed = Long.parseLong(t);
                if (parsed > 0) {
                    return parsed;
                }
            } catch (NumberFormatException ignored) {
                // fall through
            }
        }
        CRC32 crc = new CRC32();
        crc.update(t.getBytes(StandardCharsets.UTF_8));
        long hashed = crc.getValue();
        return hashed == 0L ? 1L : hashed;
    }

    private static String text(JsonNode node, String field) {
        JsonNode v = node.get(field);
        if (v == null || v.isNull() || !v.isTextual()) {
            return null;
        }
        return v.asText();
    }

    private static String firstText(JsonNode node, String... fields) {
        if (node == null || !node.isObject()) {
            return null;
        }
        for (String field : fields) {
            String value = text(node, field);
            if (StringUtils.hasText(value)) {
                return value;
            }
        }
        return null;
    }

    private static JsonNode firstPresent(JsonNode root, String... fields) {
        if (root == null || !root.isObject()) {
            return null;
        }
        for (String field : fields) {
            JsonNode node = root.get(field);
            if (node != null && !node.isNull() && !node.isMissingNode()) {
                return node;
            }
        }
        return null;
    }

    private String serializeQuietly(Object value) {
        if (value == null) {
            return "";
        }
        try {
            if (value instanceof JsonNode) {
                JsonNode node = (JsonNode) value;
                if (node.isNull() || node.isMissingNode()) {
                    return "";
                }
                return objectMapper.writeValueAsString(node);
            }
            return objectMapper.writeValueAsString(value);
        } catch (Exception e) {
            return String.valueOf(value);
        }
    }

    private static String inferAssetType(String field, String value) {
        String text = ((field == null ? "" : field) + " " + (value == null ? "" : value))
                .toLowerCase(Locale.ROOT);
        if (text.contains("image") || text.contains(".jpg") || text.contains(".jpeg")
                || text.contains(".png") || text.contains(".webp")) {
            return "image";
        }
        return "video";
    }

    public static final class AssetProfile {
        private final String taskType;
        private final List<String> outputFieldPriority;

        private AssetProfile(String taskType, List<String> outputFieldPriority) {
            this.taskType = taskType;
            this.outputFieldPriority = outputFieldPriority == null
                    ? new ArrayList<>() : new ArrayList<>(outputFieldPriority);
        }

        public static AssetProfile of(String taskType, String... outputFieldPriority) {
            List<String> fields = new ArrayList<>();
            if (outputFieldPriority != null) {
                for (String field : outputFieldPriority) {
                    if (StringUtils.hasText(field)) {
                        fields.add(field.trim());
                    }
                }
            }
            return new AssetProfile(taskType, fields);
        }

        public String getTaskType() {
            return taskType;
        }
    }

    private static final class ResolvedOutput {
        private final String field;
        private final String bucket;
        private final String key;
        private final String assetType;

        private ResolvedOutput(String field, String bucket, String key, String assetType) {
            this.field = field;
            this.bucket = bucket;
            this.key = key;
            this.assetType = assetType;
        }
    }
}
