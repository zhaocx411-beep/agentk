package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.Comparator;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.regex.Pattern;
import java.util.stream.Collectors;

import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.CreateDatasetTaskRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetExportResult;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetSummaryView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetTaskView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetUploadRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.UpdateDatasetRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.UpdateDatasetTaskRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetListFilter;
import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetTaskListFilter;
import com.kuaishou.agent.operation.backend.batcheval.dto.PagedResult;
import com.kuaishou.agent.operation.backend.batcheval.mapper.BatchEvalMapper;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.Dataset;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;
import com.kuaishou.agent.operation.backend.batcheval.service.DatasetService;
import com.kuaishou.agent.operation.backend.batcheval.service.impl.XlsxDatasetParser.ParseResult;
import com.kuaishou.agent.operation.backend.batcheval.support.DatasetConversationFields;
import com.kuaishou.agent.operation.backend.batcheval.support.DatasetTaskCategories;
import com.kuaishou.agent.operation.backend.batcheval.support.DatasetUsages;
import com.kuaishou.agent.operation.backend.batcheval.support.PageRequests;
import com.kuaishou.agent.operation.backend.batcheval.support.TemplateTaskRandomSample;

@Service
public class DatasetServiceImpl implements DatasetService {

    private static final Pattern TASK_KEY = Pattern.compile("[A-Za-z0-9._-]{1,128}");
    private static final Set<String> DATASET_TYPES = new HashSet<>(
            Arrays.asList("文本", "图片", "视频"));
    /** 查询侧不再回传的原始商品列（已投影到 title / video_prompt / images）。 */
    private static final Set<String> QUERY_EXTRA_FIELDS_HIDDEN = new HashSet<>(Arrays.asList(
            "商品标题", "商品价格", "商品属性", "商品属性信息"));
    private static final int MAX_NAME_LENGTH = 128;
    private static final int MAX_VERSION_LENGTH = 64;
    private static final int MAX_DESCRIPTION_LENGTH = 512;
    private static final int MAX_LINK_URL_LENGTH = 1024;

    private final BatchEvalMapper mapper;
    private final ObjectMapper objectMapper;

    public DatasetServiceImpl(BatchEvalMapper mapper, ObjectMapper objectMapper) {
        this.mapper = mapper;
        this.objectMapper = objectMapper;
    }

    @Override
    public DatasetSummaryView upload(DatasetUploadRequest request) {
        if (request == null) {
            throw new IllegalArgumentException("上传请求不能为空");
        }
        byte[] content = request.getContent();
        if (content == null || content.length == 0) {
            throw new IllegalArgumentException("数据集文件不能为空");
        }
        String datasetName = requireFieldLength(requiredNonBlank(request.getName(), "name"), "name", MAX_NAME_LENGTH);
        String normalizedUsage = DatasetUsages.require(request.getUsage());
        String normalizedPipeline = requirePipelineName(normalizedUsage, request.getPipelineName());
        if (DatasetUsages.TEMPLATE.equals(normalizedUsage)) {
            requireJsonForTemplate(content, request.getFilename());
        }
        // 扩展名 + 魔数双判：前端/浏览器对中文文件名常丢扩展名，否则会当 JSON 解析失败。
        if (isSpreadsheet(content, request.getFilename())) {
            return uploadXlsx(content, request.getDescription(), request.getDatasetType(), request.getCreator(),
                    datasetName, request.getVersion(), request.getTaskInstruction(),
                    normalizedUsage, normalizedPipeline);
        }
        String text = new String(content, StandardCharsets.UTF_8).trim();
        if (!isJsonDocument(text, request.getFilename())) {
            throw new IllegalArgumentException("数据集仅支持 JSON / xlsx，不支持 YAML");
        }
        Map<?, ?> root = parseJsonRoot(text);
        Dataset dataset = buildDataset(root, request.getDescription(), request.getDatasetType(),
                request.getCreator(), datasetName, request.getVersion());
        dataset.setUsage(normalizedUsage);
        dataset.setPipelineName(normalizedPipeline);
        return enrichSummary(mapper.insertDataset(dataset));
    }

    @Override
    public DatasetSummaryView uploadTemplate(DatasetUploadRequest request) {
        if (request == null) {
            throw new IllegalArgumentException("上传请求不能为空");
        }
        request.setUsage(DatasetUsages.TEMPLATE);
        return upload(request);
    }

    private DatasetSummaryView uploadXlsx(byte[] content, String description, String datasetType,
            String creator, String name, String version, String taskInstruction,
            String usage, String pipelineName) {
        ParseResult parsed = XlsxDatasetParser.parse(content, taskInstruction);
        LocalDateTime now = LocalDateTime.now();
        Dataset dataset = new Dataset();
        dataset.setName(name);
        dataset.setVersion(requireFieldLength(firstNonBlank(version, "v1"), "version", MAX_VERSION_LENGTH));
        dataset.setDatasetType(requireUploadDatasetType(datasetType));
        dataset.setUsage(usage);
        dataset.setPipelineName(pipelineName);
        dataset.setDescription(requireOptionalFieldLength(description, "description", MAX_DESCRIPTION_LENGTH));
        dataset.setCreator(creator);
        dataset.setCreatedAt(now);
        dataset.setUpdatedAt(now);

        Set<String> keys = new HashSet<>();
        for (DatasetTask task : parsed.getTasks()) {
            validateTaskKey(task.getTaskKey());
            if (!keys.add(task.getTaskKey())) {
                throw new IllegalArgumentException("数据集 task id 重复: " + task.getTaskKey());
            }
            // xlsx 与 JSON 对齐：id/prompt/linkUrl/referImages/extraFields/category/conversationKey/turnIndex。
            if (task.getReferImages() == null) {
                task.setReferImages(new ArrayList<>());
            }
            if (task.getExtraFields() == null) {
                task.setExtraFields(new LinkedHashMap<>());
            }
            task.setCreatedAt(now);
            task.setUpdatedAt(now);
            dataset.getTasks().add(task);
        }
        DatasetConversationFields.normalizeAndValidate(dataset.getTasks());
        dataset.setTotal(dataset.getTasks().size());
        DatasetSummaryView view = enrichSummary(mapper.insertDataset(dataset));
        view.setColumnMapping(new LinkedHashMap<>(parsed.getColumnMapping()));
        view.setParsePreview(new ArrayList<>(parsed.getPreview()));
        return view;
    }

    @Override
    public PagedResult<List<DatasetSummaryView>> list(DatasetListFilter filter, int current, int pageSize) {
        int normalizedCurrent = PageRequests.normalizeCurrent(current);
        int normalizedPageSize = PageRequests.normalizePageSize(pageSize);
        DatasetListFilter normalizedFilter = normalizeFilter(filter);
        List<DatasetSummaryView> records = mapper
                .listDatasets(normalizedFilter, PageRequests.offset(normalizedCurrent, normalizedPageSize),
                        normalizedPageSize)
                .stream()
                .map(this::enrichSummary)
                .collect(Collectors.toList());
        return PagedResult.of(records, mapper.countDatasets(normalizedFilter), normalizedCurrent, normalizedPageSize);
    }

    @Override
    public DatasetSummaryView get(long id) {
        return enrichSummary(requireDataset(id));
    }

    @Override
    public DatasetSummaryView update(long id, UpdateDatasetRequest request) {
        if (request == null) {
            throw new IllegalArgumentException("请求体不能为空");
        }
        Dataset dataset = requireDataset(id);
        boolean changed = false;
        if (request.getDescription() != null) {
            dataset.setDescription(requireOptionalFieldLength(request.getDescription(), "description",
                    MAX_DESCRIPTION_LENGTH));
            changed = true;
        }
        if (request.getDatasetType() != null && !request.getDatasetType().trim().isEmpty()) {
            dataset.setDatasetType(requireUploadDatasetType(request.getDatasetType()));
            changed = true;
        }
        if (!changed) {
            throw new IllegalArgumentException("至少提供 description 或 datasetType 之一");
        }
        dataset.setUpdatedAt(LocalDateTime.now());
        mapper.updateDataset(dataset);
        return enrichSummary(requireDataset(id));
    }

    @Override
    public PagedResult<List<DatasetTaskView>> listTasks(long datasetId, int current, int pageSize) {
        DatasetTaskListFilter filter = new DatasetTaskListFilter();
        filter.setDatasetId(datasetId);
        return listTasks(filter, current, pageSize);
    }

    @Override
    public PagedResult<List<DatasetTaskView>> listTasks(DatasetTaskListFilter filter, int current, int pageSize) {
        DatasetTaskListFilter normalized = normalizeTaskFilter(filter);
        if (!normalized.hasDatasetId() && !normalized.hasUsage() && !normalized.hasPipelineName()) {
            throw new IllegalArgumentException("datasetId / usage / pipelineName 至少传一个");
        }
        if (normalized.hasDatasetId()) {
            requireDataset(normalized.getDatasetId());
        }
        long total = mapper.countTasks(normalized);
        if (TemplateTaskRandomSample.shouldSample(normalized)) {
            int sampleSize = (int) Math.min(TemplateTaskRandomSample.SAMPLE_SIZE, total);
            List<DatasetTaskView> records = mapper.listTasksRandom(normalized, sampleSize).stream()
                    .map(DatasetServiceImpl::toTaskView)
                    .collect(Collectors.toList());
            return PagedResult.of(records, total, PageRequests.DEFAULT_CURRENT, sampleSize);
        }
        int normalizedCurrent = PageRequests.normalizeCurrent(current);
        int normalizedPageSize = PageRequests.normalizePageSize(pageSize);
        List<DatasetTaskView> records = mapper
                .listTasks(normalized, PageRequests.offset(normalizedCurrent, normalizedPageSize),
                        normalizedPageSize)
                .stream()
                .map(DatasetServiceImpl::toTaskView)
                .collect(Collectors.toList());
        return PagedResult.of(records, total, normalizedCurrent, normalizedPageSize);
    }

    @Override
    public DatasetTaskView createTask(long datasetId, CreateDatasetTaskRequest request) {
        if (request == null) {
            throw new IllegalArgumentException("请求体不能为空");
        }
        requireDataset(datasetId);
        LocalDateTime now = LocalDateTime.now();
        DatasetTask task = new DatasetTask();
        task.setTaskKey(requireFieldLength(requiredNonBlank(request.getTaskKey(), "taskKey"), "taskKey", 128));
        validateTaskKey(task.getTaskKey());
        task.setPrompt(requiredNonBlank(request.getPrompt(), "prompt"));
        task.setLinkUrl(requireOptionalFieldLength(request.getLinkUrl(), "linkUrl", MAX_LINK_URL_LENGTH));
        task.setReferImages(copyStrings(request.getReferImages()));
        task.setExtraFields(copyMap(request.getExtraFields()));
        task.setCategory(DatasetTaskCategories.normalize(request.getCategory()));
        task.setConversationKey(DatasetConversationFields.normalizeKey(request.getConversationKey()));
        task.setTurnIndex(request.getTurnIndex());
        applyLinkUrlFallback(task);
        task.setCreatedAt(now);
        task.setUpdatedAt(now);
        List<DatasetTask> siblings = new ArrayList<>(mapper.listAllTasks(datasetId));
        siblings.add(task);
        DatasetConversationFields.normalizeAndValidate(siblings);
        return toTaskView(mapper.insertTask(datasetId, task));
    }

    @Override
    public DatasetTaskView updateTask(long taskId, UpdateDatasetTaskRequest request) {
        if (request == null) {
            throw new IllegalArgumentException("请求体不能为空");
        }
        DatasetTask task = requireTask(taskId);
        boolean changed = false;
        if (request.getPrompt() != null) {
            task.setPrompt(requiredNonBlank(request.getPrompt(), "prompt"));
            changed = true;
        }
        if (request.getLinkUrl() != null) {
            task.setLinkUrl(requireOptionalFieldLength(request.getLinkUrl(), "linkUrl", MAX_LINK_URL_LENGTH));
            changed = true;
        }
        if (request.getReferImages() != null) {
            task.setReferImages(copyStrings(request.getReferImages()));
            changed = true;
        }
        if (request.getExtraFields() != null) {
            task.setExtraFields(copyMap(request.getExtraFields()));
            changed = true;
        }
        if (request.getCategory() != null) {
            task.setCategory(DatasetTaskCategories.normalize(request.getCategory()));
            changed = true;
        }
        if (request.getConversationKey() != null) {
            task.setConversationKey(DatasetConversationFields.normalizeKey(request.getConversationKey()));
            if (task.getConversationKey() == null) {
                task.setTurnIndex(null);
            }
            changed = true;
        }
        if (request.getTurnIndex() != null) {
            task.setTurnIndex(request.getTurnIndex());
            changed = true;
        }
        if (!changed) {
            throw new IllegalArgumentException(
                    "至少提供 prompt / linkUrl / referImages / extraFields / category / conversationKey / turnIndex 之一");
        }
        applyLinkUrlFallback(task);
        task.setUpdatedAt(LocalDateTime.now());
        long datasetId = mapper.findDatasetIdByTaskId(taskId)
                .orElseThrow(() -> new IllegalArgumentException("数据集用例不存在: " + taskId));
        List<DatasetTask> siblings = new ArrayList<>();
        for (DatasetTask existing : mapper.listAllTasks(datasetId)) {
            siblings.add(existing.getId() == task.getId() ? task : existing);
        }
        DatasetConversationFields.normalizeAndValidate(siblings);
        mapper.updateTask(task);
        return toTaskView(requireTask(taskId));
    }

    @Override
    public void deleteTask(long taskId) {
        DatasetTask task = requireTask(taskId);
        long datasetId = mapper.findDatasetIdByTaskId(taskId)
                .orElseThrow(() -> new IllegalArgumentException("数据集用例不存在: " + taskId));
        int total = mapper.findDataset(datasetId)
                .map(Dataset::getTotal)
                .orElse(0);
        if (total <= 1) {
            throw new IllegalArgumentException("数据集至少保留 1 条用例，无法删除");
        }
        if (!mapper.deleteTask(task.getId())) {
            throw new IllegalArgumentException("数据集用例不存在: " + taskId);
        }
    }

    @Override
    public DatasetExportResult export(long id, String format) {
        Dataset dataset = requireDataset(id);
        List<DatasetTask> tasks = mapper.listAllTasks(id);
        String resolved = normalizeExportFormat(format);
        DatasetExportResult result = new DatasetExportResult();
        result.setFormat(resolved);
        String stamp = dataset.getUpdatedAt() == null
                ? "latest"
                : dataset.getUpdatedAt().toString().replace(':', '-').replace(' ', '_');
        String baseName = sanitizeFilenamePart(dataset.getName()) + "-"
                + sanitizeFilenamePart(dataset.getVersion()) + "-" + stamp;
        if ("xlsx".equals(resolved)) {
            result.setBody(XlsxDatasetWriter.write(tasks));
            result.setFilename(baseName + ".xlsx");
            result.setContentType("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet");
            return result;
        }
        Map<String, Object> root = buildExportDocument(dataset, tasks);
        if ("json".equals(resolved)) {
            try {
                result.setBody(objectMapper.writerWithDefaultPrettyPrinter().writeValueAsBytes(root));
            } catch (IOException e) {
                throw new IllegalStateException("导出 JSON 失败", e);
            }
            result.setFilename(baseName + ".json");
            result.setContentType("application/json;charset=UTF-8");
            return result;
        }
        throw new IllegalArgumentException("format 仅支持 json/xlsx，当前值: " + format);
    }

    @Override
    public void delete(long id) {
        requireDataset(id);
        long jobCount = mapper.countJobsByDatasetId(id);
        if (jobCount > 0) {
            throw new IllegalArgumentException("数据集已被 " + jobCount + " 个离线评测任务引用，无法删除");
        }
        if (!mapper.deleteDataset(id)) {
            throw new IllegalArgumentException("数据集不存在: " + id);
        }
    }

    private DatasetTask requireTask(long taskId) {
        return mapper.findTask(taskId)
                .orElseThrow(() -> new IllegalArgumentException("数据集用例不存在: " + taskId));
    }

    private static DatasetListFilter normalizeFilter(DatasetListFilter filter) {
        if (filter == null) {
            return new DatasetListFilter();
        }
        DatasetListFilter normalized = new DatasetListFilter();
        if (filter.getName() != null && !filter.getName().trim().isEmpty()) {
            normalized.setName(filter.getName().trim());
        }
        if (filter.getDatasetType() != null && !filter.getDatasetType().trim().isEmpty()) {
            normalized.setDatasetType(normalizeDatasetType(filter.getDatasetType().trim()));
        }
        if (filter.getCreator() != null && !filter.getCreator().trim().isEmpty()) {
            normalized.setCreator(filter.getCreator().trim());
        }
        if (filter.getUsage() != null && !filter.getUsage().trim().isEmpty()) {
            normalized.setUsage(DatasetUsages.normalize(filter.getUsage()));
        }
        if (filter.getPipelineName() != null && !filter.getPipelineName().trim().isEmpty()) {
            normalized.setPipelineName(filter.getPipelineName().trim());
        }
        return normalized;
    }

    private static DatasetTaskListFilter normalizeTaskFilter(DatasetTaskListFilter filter) {
        DatasetTaskListFilter normalized = new DatasetTaskListFilter();
        if (filter == null) {
            return normalized;
        }
        if (filter.hasDatasetId()) {
            normalized.setDatasetId(filter.getDatasetId());
        }
        if (filter.hasUsage()) {
            normalized.setUsage(DatasetUsages.normalize(filter.getUsage()));
        }
        if (filter.hasPipelineName()) {
            normalized.setPipelineName(filter.getPipelineName().trim());
        }
        if (filter.hasCategory()) {
            String category = DatasetTaskCategories.normalize(filter.getCategory());
            if (category == null) {
                throw new IllegalArgumentException("category 不能为空");
            }
            normalized.setCategory(category);
        }
        return normalized;
    }

    /** TEMPLATE 必须归属某个 pipeline，否则模版列表无从筛选；EVAL 忽略该字段。 */
    private static String requirePipelineName(String normalizedUsage, String pipelineName) {
        String trimmed = pipelineName == null ? "" : pipelineName.trim();
        if (!DatasetUsages.TEMPLATE.equals(normalizedUsage)) {
            return trimmed.isEmpty() ? null : trimmed;
        }
        if (trimmed.isEmpty()) {
            throw new IllegalArgumentException("usage=TEMPLATE 时 pipelineName 不能为空");
        }
        return trimmed;
    }

    /** TEMPLATE 素材/模版只收 JSON，避免误走 xlsx / YAML 解析。 */
    private static void requireJsonForTemplate(byte[] content, String filename) {
        if (isSpreadsheet(content, filename)) {
            throw new IllegalArgumentException("素材/模版仅支持 JSON 数据集，不支持表格文件");
        }
        String text = new String(content, StandardCharsets.UTF_8).trim();
        if (text.isEmpty() || !isJsonDocument(text, filename)) {
            throw new IllegalArgumentException("素材/模版仅支持 JSON 数据集");
        }
    }

    private static String requiredNonBlank(String value, String field) {
        if (value == null || value.trim().isEmpty()) {
            throw new IllegalArgumentException(field + " 不能为空");
        }
        return value.trim();
    }

    private Dataset buildDataset(Map<?, ?> root, String description, String datasetType, String creator,
            String name, String overrideVersion) {
        LocalDateTime now = LocalDateTime.now();
        Dataset dataset = new Dataset();
        // name 已在 upload 入口强制校验；version 仍可回退到文件内字段
        String version = firstNonBlank(overrideVersion, requiredString(root, "version"));
        dataset.setName(name);
        dataset.setVersion(requireFieldLength(version, "version", MAX_VERSION_LENGTH));
        dataset.setDatasetType(requireUploadDatasetType(datasetType));
        dataset.setDescription(requireOptionalFieldLength(description, "description", MAX_DESCRIPTION_LENGTH));
        dataset.setCreator(creator);
        dataset.setCreatedAt(now);
        dataset.setUpdatedAt(now);

        Object rawTasks = root.get("tasks");
        if (!(rawTasks instanceof List) || ((List<?>) rawTasks).isEmpty()) {
            throw new IllegalArgumentException("数据集 tasks 不能为空");
        }
        Set<String> keys = new HashSet<>();
        int index = 0;
        for (Object rawTask : (List<?>) rawTasks) {
            if (!(rawTask instanceof Map)) {
                throw new IllegalArgumentException("tasks[" + index + "] 必须是对象");
            }
            Map<?, ?> taskMap = (Map<?, ?>) rawTask;
            DatasetTask task = new DatasetTask();
            task.setTaskKey(requiredString(taskMap, "id"));
            validateTaskKey(task.getTaskKey());
            task.setPrompt(requiredString(taskMap, "prompt"));
            task.setLinkUrl(optionalLinkUrl(taskMap));
            task.setReferImages(parseReferImages(taskMap));
            task.setExtraFields(parseExtraFields(taskMap));
            task.setCategory(DatasetTaskCategories.normalize(optionalString(taskMap, "category")));
            DatasetConversationFields.applyFromTaskMap(task, taskMap);
            applyLinkUrlFallback(task);
            task.setCreatedAt(now);
            task.setUpdatedAt(now);
            index++;
            if (!keys.add(task.getTaskKey())) {
                throw new IllegalArgumentException("数据集 task id 重复: " + task.getTaskKey());
            }
            dataset.getTasks().add(task);
        }
        DatasetConversationFields.normalizeAndValidate(dataset.getTasks());
        dataset.setTotal(dataset.getTasks().size());
        return dataset;
    }

    private Map<String, Object> buildExportDocument(Dataset dataset, List<DatasetTask> tasks) {
        Map<String, Object> root = new LinkedHashMap<>();
        root.put("name", dataset.getName());
        root.put("version", dataset.getVersion());
        root.put("datasetType", dataset.getDatasetType());
        if (dataset.getDescription() != null && !dataset.getDescription().isEmpty()) {
            root.put("description", dataset.getDescription());
        }
        List<Map<String, Object>> taskItems = new ArrayList<>();
        // 按 task_key 稳定排序，避免仅依赖自增 id；内容一律取当前 tasks 入参（已是库内最新）。
        List<DatasetTask> sorted = new ArrayList<>(tasks == null ? Collections.emptyList() : tasks);
        sorted.sort(Comparator
                .comparing(DatasetTask::getTaskKey, Comparator.nullsLast(String::compareTo))
                .thenComparingLong(DatasetTask::getId));
        for (DatasetTask task : sorted) {
            Map<String, Object> item = new LinkedHashMap<>();
            item.put("id", task.getTaskKey());
            item.put("prompt", task.getPrompt());
            if (task.getLinkUrl() != null && !task.getLinkUrl().isEmpty()) {
                item.put("linkUrl", task.getLinkUrl());
            }
            if (task.getReferImages() != null && !task.getReferImages().isEmpty()) {
                item.put("referImages", new ArrayList<>(task.getReferImages()));
            }
            if (task.getExtraFields() != null && !task.getExtraFields().isEmpty()) {
                item.put("extraFields", new LinkedHashMap<>(task.getExtraFields()));
            }
            if (task.getCategory() != null && !task.getCategory().isEmpty()) {
                item.put("category", task.getCategory());
            }
            if (task.getConversationKey() != null && !task.getConversationKey().isEmpty()) {
                item.put("conversationKey", task.getConversationKey());
            }
            if (task.getTurnIndex() != null) {
                item.put("turnIndex", task.getTurnIndex());
            }
            taskItems.add(item);
        }
        root.put("tasks", taskItems);
        return root;
    }

    private Map<?, ?> parseDatasetDocument(byte[] content, String filename) {
        String text = new String(content, StandardCharsets.UTF_8).trim();
        if (text.isEmpty()) {
            throw new IllegalArgumentException("数据集文件不能为空");
        }
        if (!isJsonDocument(text, filename)) {
            throw new IllegalArgumentException("数据集仅支持 JSON / xlsx，不支持 YAML");
        }
        return parseJsonRoot(text);
    }

    private static boolean isSpreadsheet(byte[] content, String filename) {
        return isSpreadsheetFilename(filename) || looksLikeSpreadsheet(content);
    }

    private static boolean isSpreadsheetFilename(String filename) {
        if (filename == null) {
            return false;
        }
        String lower = filename.toLowerCase(Locale.ROOT);
        // 兼容 Content-Disposition 里 filename*=UTF-8''xxx.xlsx 或带 query/fragment 的脏文件名
        int q = lower.indexOf('?');
        if (q >= 0) {
            lower = lower.substring(0, q);
        }
        int hash = lower.indexOf('#');
        if (hash >= 0) {
            lower = lower.substring(0, hash);
        }
        return lower.endsWith(".xlsx") || lower.endsWith(".xls");
    }

    /** xlsx=ZIP(PK..)，xls=OLE Compound(D0 CF 11 E0)。 */
    private static boolean looksLikeSpreadsheet(byte[] content) {
        if (content == null || content.length < 4) {
            return false;
        }
        boolean zip = content[0] == 'P' && content[1] == 'K'
                && (content[2] == 3 || content[2] == 5 || content[2] == 7)
                && (content[3] == 4 || content[3] == 6 || content[3] == 8);
        if (zip) {
            return true;
        }
        return (content[0] & 0xFF) == 0xD0
                && (content[1] & 0xFF) == 0xCF
                && (content[2] & 0xFF) == 0x11
                && (content[3] & 0xFF) == 0xE0;
    }

    private static boolean isJsonDocument(String text, String filename) {
        if (filename != null) {
            String lower = filename.toLowerCase(Locale.ROOT);
            if (lower.endsWith(".json")) {
                return true;
            }
            if (lower.endsWith(".yaml") || lower.endsWith(".yml")) {
                return false;
            }
        }
        return text.startsWith("{");
    }

    private Map<?, ?> parseJsonRoot(String text) {
        try {
            Map<String, Object> root = objectMapper.readValue(text, new TypeReference<Map<String, Object>>() {
            });
            if (root == null || root.isEmpty()) {
                throw new IllegalArgumentException("数据集 JSON 顶层必须是对象");
            }
            return root;
        } catch (IllegalArgumentException e) {
            throw e;
        } catch (IOException e) {
            throw new IllegalArgumentException("数据集 JSON 非法: " + e.getMessage(), e);
        }
    }

    private static String normalizeExportFormat(String format) {
        if (format == null || format.trim().isEmpty()) {
            return "json";
        }
        String normalized = format.trim().toLowerCase(Locale.ROOT);
        if ("json".equals(normalized)) {
            return "json";
        }
        if ("xlsx".equals(normalized) || "xls".equals(normalized)) {
            return "xlsx";
        }
        throw new IllegalArgumentException("format 仅支持 json/xlsx，当前值: " + format);
    }

    private static String sanitizeFilenamePart(String raw) {
        if (raw == null || raw.trim().isEmpty()) {
            return "dataset";
        }
        return raw.trim().replaceAll("[^A-Za-z0-9._\\u4e00-\\u9fa5-]", "_");
    }

    private Dataset requireDataset(long id) {
        return mapper.findDataset(id)
                .orElseThrow(() -> new IllegalArgumentException("数据集不存在: " + id));
    }

    private static String requiredString(Map<?, ?> map, String key) {
        Object value = map.get(key);
        String text = value == null ? "" : String.valueOf(value).trim();
        if (text.isEmpty()) {
            throw new IllegalArgumentException(key + " 不能为空");
        }
        return text;
    }

    private static String requireUploadDatasetType(String uploadDatasetType) {
        if (uploadDatasetType == null || uploadDatasetType.trim().isEmpty()) {
            throw new IllegalArgumentException("datasetType 不能为空，可选值：文本、图片、视频");
        }
        String normalized = normalizeDatasetType(uploadDatasetType.trim());
        if (!DATASET_TYPES.contains(normalized)) {
            throw new IllegalArgumentException(
                    "datasetType 仅支持：文本、图片、视频，当前值: " + uploadDatasetType);
        }
        return normalized;
    }

    private static String normalizeDatasetType(String value) {
        switch (value) {
            case "text":
                return "文本";
            case "image":
                return "图片";
            case "video":
                return "视频";
            default:
                return value;
        }
    }

    private static String optionalString(Map<?, ?> map, String key) {
        Object value = map.get(key);
        if (value == null) {
            return null;
        }
        String text = String.valueOf(value).trim();
        return text.isEmpty() ? null : text;
    }

    private static String optionalLinkUrl(Map<?, ?> taskMap) {
        String linkUrl = optionalString(taskMap, "linkUrl");
        if (linkUrl == null) {
            linkUrl = optionalString(taskMap, "link_url");
        }
        if (linkUrl == null) {
            linkUrl = optionalString(taskMap, "url");
        }
        if (linkUrl == null) {
            return null;
        }
        if (linkUrl.length() > MAX_LINK_URL_LENGTH) {
            throw new IllegalArgumentException("linkUrl 长度不能超过 " + MAX_LINK_URL_LENGTH);
        }
        return linkUrl;
    }

    @SuppressWarnings("unchecked")
    private static List<String> parseReferImages(Map<?, ?> taskMap) {
        Object raw = taskMap.get("referImages");
        if (raw == null) {
            raw = taskMap.get("refer_images");
        }
        if (raw == null) {
            return new ArrayList<>();
        }
        if (raw instanceof List) {
            List<String> out = new ArrayList<>();
            for (Object item : (List<?>) raw) {
                if (item == null) {
                    continue;
                }
                String url = String.valueOf(item).trim();
                if (!url.isEmpty()) {
                    out.add(url);
                }
            }
            return out;
        }
        if (raw instanceof String) {
            return new ArrayList<>(XlsxDatasetParser.splitUrls((String) raw));
        }
        throw new IllegalArgumentException("referImages 必须是字符串数组");
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> parseExtraFields(Map<?, ?> taskMap) {
        Object raw = taskMap.get("extraFields");
        if (raw == null) {
            raw = taskMap.get("extra_fields");
        }
        if (raw == null) {
            return new LinkedHashMap<>();
        }
        if (!(raw instanceof Map)) {
            throw new IllegalArgumentException("extraFields 必须是对象");
        }
        Map<String, Object> out = new LinkedHashMap<>();
        for (Map.Entry<?, ?> entry : ((Map<?, ?>) raw).entrySet()) {
            if (entry.getKey() == null) {
                continue;
            }
            out.put(String.valueOf(entry.getKey()), entry.getValue());
        }
        return out;
    }

    /**
     * 兼容旧 YAML/JSON：仅当未提供 referImages 时，把 linkUrl 当作首图。
     * 不再反向用 referImages[0] 覆盖 linkUrl——linkUrl 语义是商品/页面链接。
     */
    private static void applyLinkUrlFallback(DatasetTask task) {
        if (task.getReferImages() == null) {
            task.setReferImages(new ArrayList<>());
        }
        if (task.getExtraFields() == null) {
            task.setExtraFields(new LinkedHashMap<>());
        }
        if (task.getReferImages().isEmpty() && StringUtils.hasText(task.getLinkUrl())) {
            task.getReferImages().add(task.getLinkUrl().trim());
        }
    }

    private static void validateTaskKey(String taskKey) {
        if (!TASK_KEY.matcher(taskKey).matches()) {
            throw new IllegalArgumentException(
                    "task id 仅支持字母、数字、点、下划线和中划线，长度 1-128: " + taskKey);
        }
    }

    private static String requireFieldLength(String value, String field, int maxLength) {
        if (value.length() > maxLength) {
            throw new IllegalArgumentException(field + " 长度不能超过 " + maxLength);
        }
        return value;
    }

    private static String requireOptionalFieldLength(String value, String field, int maxLength) {
        if (value == null || value.trim().isEmpty()) {
            return null;
        }
        String trimmed = value.trim();
        if (trimmed.length() > maxLength) {
            throw new IllegalArgumentException(field + " 长度不能超过 " + maxLength);
        }
        return trimmed;
    }

    private static String firstNonBlank(String... values) {
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

    private static List<String> copyStrings(List<String> source) {
        if (source == null || source.isEmpty()) {
            return new ArrayList<>();
        }
        List<String> out = new ArrayList<>();
        for (String item : source) {
            if (StringUtils.hasText(item)) {
                out.add(item.trim());
            }
        }
        return out;
    }

    private static Map<String, Object> copyMap(Map<String, Object> source) {
        if (source == null || source.isEmpty()) {
            return new LinkedHashMap<>();
        }
        return new LinkedHashMap<>(source);
    }

    private DatasetSummaryView enrichSummary(Dataset dataset) {
        DatasetSummaryView view = toSummaryView(dataset);
        long jobCount = mapper.countJobsByDatasetId(dataset.getId());
        view.setJobCount(jobCount);
        view.setReferencedByJobs(jobCount > 0);
        return view;
    }

    private static DatasetSummaryView toSummaryView(Dataset dataset) {
        DatasetSummaryView view = new DatasetSummaryView();
        view.setId(dataset.getId());
        view.setName(dataset.getName());
        view.setVersion(dataset.getVersion());
        view.setDatasetType(dataset.getDatasetType());
        view.setUsage(DatasetUsages.normalize(dataset.getUsage()));
        view.setPipelineName(dataset.getPipelineName());
        view.setDescription(dataset.getDescription());
        view.setCreator(dataset.getCreator());
        view.setTotal(dataset.getTotal());
        view.setCreatedAt(dataset.getCreatedAt());
        view.setUpdatedAt(dataset.getUpdatedAt());
        return view;
    }

    private static DatasetTaskView toTaskView(DatasetTask task) {
        DatasetTaskView view = new DatasetTaskView();
        view.setId(task.getId());
        view.setDatasetId(task.getDatasetId());
        view.setTaskKey(task.getTaskKey());
        view.setPrompt(task.getPrompt());
        view.setLinkUrl(task.getLinkUrl());
        view.setReferImages(task.getReferImages() == null
                ? new ArrayList<>() : new ArrayList<>(task.getReferImages()));
        view.setCategory(task.getCategory());
        view.setConversationKey(task.getConversationKey());
        view.setTurnIndex(task.getTurnIndex());
        // /tasks/query 等读接口：把展示/pipeline 常用键投影进 extraFields
        // video_prompt←prompt，title←商品标题，images←referImages
        view.setExtraFields(projectExtraFieldsForQuery(task));
        view.setCreatedAt(task.getCreatedAt());
        view.setUpdatedAt(task.getUpdatedAt());
        return view;
    }

    /**
     * 查询侧 extraFields 投影：对齐 generate_brand_ad 等 pipeline 入参键名，
     * 便于前端直接拿 extraFields 填表；库内仍保留原始字段，仅读接口隐藏。
     */
    private static Map<String, Object> projectExtraFieldsForQuery(DatasetTask task) {
        Map<String, Object> source = task.getExtraFields() == null
                ? new LinkedHashMap<>() : task.getExtraFields();
        Map<String, Object> extra = new LinkedHashMap<>();
        for (Map.Entry<String, Object> entry : source.entrySet()) {
            if (!QUERY_EXTRA_FIELDS_HIDDEN.contains(entry.getKey())) {
                extra.put(entry.getKey(), entry.getValue());
            }
        }
        Object productTitle = firstNonBlank(source.get("商品标题"), source.get("title"), source.get("标题"));
        if (productTitle != null) {
            extra.put("title", productTitle);
        }
        if (StringUtils.hasText(task.getPrompt())) {
            extra.put("video_prompt", task.getPrompt().trim());
        }
        List<String> images = task.getReferImages() == null
                ? new ArrayList<>() : new ArrayList<>(task.getReferImages());
        if (!images.isEmpty()) {
            extra.put("images", images);
        }
        return extra;
    }

    private static Object firstNonBlank(Object... values) {
        if (values == null) {
            return null;
        }
        for (Object value : values) {
            if (value != null && StringUtils.hasText(String.valueOf(value))) {
                return String.valueOf(value).trim();
            }
        }
        return null;
    }
}
