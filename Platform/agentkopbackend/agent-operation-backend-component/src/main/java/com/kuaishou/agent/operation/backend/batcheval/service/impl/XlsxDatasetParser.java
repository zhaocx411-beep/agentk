package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import java.io.ByteArrayInputStream;
import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.regex.Pattern;

import org.springframework.util.StringUtils;

import com.alibaba.excel.EasyExcel;
import com.alibaba.excel.context.AnalysisContext;
import com.alibaba.excel.event.AnalysisEventListener;
import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;
import com.kuaishou.agent.operation.backend.batcheval.support.DatasetConversationFields;
import com.kuaishou.agent.operation.backend.batcheval.support.DatasetTaskCategories;

/**
 * xlsx → DatasetTask。标准表头与 JSON 用例字段对齐：
 * {@code id / prompt / linkUrl / referImages / extraFields / category / conversationKey / turnIndex}。
 * 不在这 8 列（及其别名）里的表头，按「原列名 → 单元格值」写入 {@code extraFields}。
 */
public final class XlsxDatasetParser {

    private static final int MAX_ROWS = 5000;
    private static final Pattern URL_SPLIT = Pattern.compile("[\\n\\r,，;；]+");
    private static final ObjectMapper JSON = new ObjectMapper();
    private static final TypeReference<LinkedHashMap<String, Object>> EXTRA_TYPE =
            new TypeReference<LinkedHashMap<String, Object>>() {
            };

    static final String COL_ID = "id";
    static final String COL_PROMPT = "prompt";
    static final String COL_LINK = "linkUrl";
    static final String COL_REFER = "referImages";
    static final String COL_EXTRA = "extraFields";
    static final String COL_CATEGORY = "category";
    static final String COL_CONV = "conversationKey";
    static final String COL_TURN = "turnIndex";

    /** 商品表旧列，写入 extraFields / 并入 referImages，不是 JSON 一等字段。 */
    private static final String COL_TITLE = "商品标题";
    private static final String COL_COVER = "首图url";
    private static final String COL_PRICE = "商品价格";
    private static final String COL_ATTR = "商品属性信息";

    private XlsxDatasetParser() {
    }

    public static ParseResult parse(byte[] content, String taskInstruction) {
        if (content == null || content.length == 0) {
            throw new IllegalArgumentException("数据集文件不能为空");
        }
        RowListener listener = new RowListener(taskInstruction);
        try {
            EasyExcel.read(new ByteArrayInputStream(content), listener)
                    .sheet()
                    .headRowNumber(1)
                    .doRead();
        } catch (IllegalArgumentException e) {
            throw e;
        } catch (RuntimeException e) {
            throw new IllegalArgumentException("xlsx 解析失败: " + e.getMessage(), e);
        }
        if (listener.tasks.isEmpty()) {
            throw new IllegalArgumentException("xlsx 没有有效数据行");
        }
        return new ParseResult(listener.columnMapping, listener.tasks, listener.preview);
    }

    static String normalizeHeader(String raw) {
        if (raw == null) {
            return "";
        }
        return raw.replace(" ", "").replace("\u00A0", "").trim().toLowerCase(Locale.ROOT);
    }

    static String resolveCanonical(String normalizedHeader) {
        if (normalizedHeader.isEmpty()) {
            return null;
        }
        if (aliases(COL_ID).contains(normalizedHeader)) {
            return COL_ID;
        }
        if (aliases(COL_PROMPT).contains(normalizedHeader)) {
            return COL_PROMPT;
        }
        if (aliases(COL_LINK).contains(normalizedHeader)) {
            return COL_LINK;
        }
        if (aliases(COL_REFER).contains(normalizedHeader)) {
            return COL_REFER;
        }
        if (aliases(COL_EXTRA).contains(normalizedHeader)) {
            return COL_EXTRA;
        }
        if (aliases(COL_CATEGORY).contains(normalizedHeader)) {
            return COL_CATEGORY;
        }
        if (aliases(COL_CONV).contains(normalizedHeader)) {
            return COL_CONV;
        }
        if (aliases(COL_TURN).contains(normalizedHeader)) {
            return COL_TURN;
        }
        if (aliases(COL_TITLE).contains(normalizedHeader)) {
            return COL_TITLE;
        }
        if (aliases(COL_COVER).contains(normalizedHeader)) {
            return COL_COVER;
        }
        if (aliases(COL_PRICE).contains(normalizedHeader)) {
            return COL_PRICE;
        }
        if (aliases(COL_ATTR).contains(normalizedHeader)) {
            return COL_ATTR;
        }
        return null;
    }

    private static List<String> aliases(String canonical) {
        switch (canonical) {
            case COL_ID:
                return list("id", "序号", "taskkey", "task_key", "编号");
            case COL_PROMPT:
                return list("prompt", "用例输入", "case输入情况", "输入情况",
                        "本轮指令", "轮次指令", "turnprompt", "userprompt");
            case COL_LINK:
                return list("linkurl", "link_url", "链接", "商品链接", "link", "url");
            case COL_REFER:
                return list("referimages", "refer_images", "参考图", "商品详情图", "详情图",
                        "detailimages");
            case COL_EXTRA:
                return list("extrafields", "extra_fields");
            case COL_CATEGORY:
                return list("category", "类目");
            case COL_CONV:
                return list("conversationkey", "conversation_key", "conversationid",
                        "conversation_id", "会话id", "sessionid");
            case COL_TURN:
                return list("turnindex", "turn_index", "turn", "轮次");
            case COL_TITLE:
                return list("商品标题", "标题", "title");
            case COL_COVER:
                return list("首图url", "首图", "首图链接", "cover", "firstframe");
            case COL_PRICE:
                return list("商品价格", "价格", "price");
            case COL_ATTR:
                return list("商品属性信息", "商品属性", "属性", "attributes");
            default:
                return Collections.emptyList();
        }
    }

    private static List<String> list(String... values) {
        List<String> out = new ArrayList<>(values.length);
        for (String value : values) {
            out.add(normalizeHeader(value));
        }
        return out;
    }

    static List<String> splitUrls(String raw) {
        if (!StringUtils.hasText(raw)) {
            return Collections.emptyList();
        }
        String[] parts = URL_SPLIT.split(raw.trim());
        List<String> urls = new ArrayList<>();
        for (String part : parts) {
            if (!StringUtils.hasText(part)) {
                continue;
            }
            String url = part.trim();
            if (!url.isEmpty()) {
                urls.add(url);
            }
        }
        return urls;
    }

    static String formatCellValue(Object value) {
        if (value == null) {
            return null;
        }
        if (value instanceof BigDecimal) {
            return ((BigDecimal) value).stripTrailingZeros().toPlainString();
        }
        if (value instanceof Double || value instanceof Float) {
            return BigDecimal.valueOf(((Number) value).doubleValue()).stripTrailingZeros().toPlainString();
        }
        if (value instanceof Number) {
            return String.valueOf(value);
        }
        String text = String.valueOf(value).trim();
        return text.isEmpty() ? null : text;
    }

    static Map<String, Object> parseExtraFieldsJson(String raw) {
        if (!StringUtils.hasText(raw)) {
            return new LinkedHashMap<>();
        }
        try {
            JsonNode node = JSON.readTree(raw.trim());
            if (node == null || node.isNull()) {
                return new LinkedHashMap<>();
            }
            if (!node.isObject()) {
                throw new IllegalArgumentException("extraFields 必须是 JSON 对象");
            }
            LinkedHashMap<String, Object> parsed = JSON.convertValue(node, EXTRA_TYPE);
            return parsed == null ? new LinkedHashMap<>() : parsed;
        } catch (IllegalArgumentException e) {
            throw e;
        } catch (Exception e) {
            throw new IllegalArgumentException("extraFields 必须是 JSON 对象", e);
        }
    }

    /**
     * 商品表兜底：没有 prompt 列时，用结构化字段 + 上传表单 {@code taskInstruction} 拼 prompt。
     */
    static String buildPrompt(Map<String, Object> extraFields, String taskInstruction) {
        if (!StringUtils.hasText(taskInstruction)) {
            throw new IllegalArgumentException("taskInstruction 不能为空");
        }
        StringBuilder sb = new StringBuilder();
        if (extraFields != null) {
            appendField(sb, "商品标题", extraFields.get(COL_TITLE));
            appendField(sb, "商品价格", extraFields.get(COL_PRICE));
            appendField(sb, "商品属性", extraFields.get(COL_ATTR));
        }
        if (sb.length() > 0) {
            sb.append('\n');
        }
        sb.append(taskInstruction.trim());
        return sb.toString().trim();
    }

    private static void appendField(StringBuilder sb, String label, Object value) {
        if (value == null) {
            return;
        }
        String text = String.valueOf(value).trim();
        if (text.isEmpty()) {
            return;
        }
        sb.append(label).append(':').append(text).append('\n');
    }

    public static final class ParseResult {
        private final Map<String, String> columnMapping;
        private final List<DatasetTask> tasks;
        private final List<Map<String, Object>> preview;

        ParseResult(Map<String, String> columnMapping, List<DatasetTask> tasks,
                List<Map<String, Object>> preview) {
            this.columnMapping = columnMapping;
            this.tasks = tasks;
            this.preview = preview;
        }

        public Map<String, String> getColumnMapping() {
            return columnMapping;
        }

        public List<DatasetTask> getTasks() {
            return tasks;
        }

        public List<Map<String, Object>> getPreview() {
            return preview;
        }
    }

    private static final class RowListener extends AnalysisEventListener<Map<Integer, String>> {
        private final String taskInstruction;
        private final Map<Integer, String> indexToCanonical = new LinkedHashMap<>();
        private final Map<Integer, String> indexToExtraKey = new LinkedHashMap<>();
        private final Map<String, String> columnMapping = new LinkedHashMap<>();
        private final List<DatasetTask> tasks = new ArrayList<>();
        private final List<Map<String, Object>> preview = new ArrayList<>();
        private boolean headerResolved;

        private RowListener(String taskInstruction) {
            this.taskInstruction = taskInstruction;
        }

        @Override
        public void invokeHeadMap(Map<Integer, String> headMap, AnalysisContext context) {
            resolveHeader(headMap);
        }

        @Override
        public void invoke(Map<Integer, String> data, AnalysisContext context) {
            if (!headerResolved) {
                resolveHeader(data);
                return;
            }
            int excelRow = context.readRowHolder().getRowIndex() + 1;
            if (tasks.size() >= MAX_ROWS) {
                throw new IllegalArgumentException("xlsx 行数不能超过 " + MAX_ROWS);
            }
            try {
                DatasetTask task = toTask(data);
                tasks.add(task);
                if (preview.size() < 3) {
                    preview.add(previewOf(task));
                }
            } catch (IllegalArgumentException e) {
                throw new IllegalArgumentException("xlsx 第 " + excelRow + " 行: " + e.getMessage(), e);
            }
        }

        @Override
        public void doAfterAllAnalysed(AnalysisContext context) {
            // no-op
        }

        private void resolveHeader(Map<Integer, String> headMap) {
            if (headerResolved || headMap == null || headMap.isEmpty()) {
                return;
            }
            for (Map.Entry<Integer, String> entry : headMap.entrySet()) {
                String original = entry.getValue() == null ? "" : entry.getValue().trim();
                String canonical = resolveCanonical(normalizeHeader(original));
                if (canonical != null) {
                    indexToCanonical.put(entry.getKey(), canonical);
                    columnMapping.put(canonical, original);
                    continue;
                }
                if (!original.isEmpty()) {
                    indexToExtraKey.put(entry.getKey(), original);
                    columnMapping.put(original, original);
                }
            }
            if (!indexToCanonical.containsValue(COL_ID)) {
                throw new IllegalArgumentException("xlsx 缺少必填列: id");
            }
            headerResolved = true;
        }

        private DatasetTask toTask(Map<Integer, String> data) {
            Map<String, String> cells = new LinkedHashMap<>();
            for (Map.Entry<Integer, String> entry : indexToCanonical.entrySet()) {
                cells.put(entry.getValue(), formatCellValue(data.get(entry.getKey())));
            }
            String taskKey = cells.get(COL_ID);
            if (!StringUtils.hasText(taskKey)) {
                throw new IllegalArgumentException("id 不能为空");
            }
            taskKey = taskKey.trim();
            if (taskKey.endsWith(".0")) {
                taskKey = taskKey.substring(0, taskKey.length() - 2);
            }

            List<String> referImages = new ArrayList<>();
            addUrl(referImages, cells.get(COL_COVER));
            for (String url : splitUrls(cells.get(COL_REFER))) {
                addUrl(referImages, url);
            }

            Map<String, Object> extraFields = parseExtraFieldsJson(cells.get(COL_EXTRA));
            putExtra(extraFields, COL_TITLE, cells.get(COL_TITLE));
            putExtra(extraFields, COL_PRICE, cells.get(COL_PRICE));
            putExtra(extraFields, COL_ATTR, cells.get(COL_ATTR));
            for (Map.Entry<Integer, String> extraCol : indexToExtraKey.entrySet()) {
                String extraValue = formatCellValue(data.get(extraCol.getKey()));
                extraFields.put(extraCol.getValue(), extraValue == null ? "" : extraValue);
            }

            DatasetTask task = new DatasetTask();
            task.setTaskKey(taskKey);
            task.setReferImages(referImages);
            task.setExtraFields(extraFields);
            String linkUrl = cells.get(COL_LINK);
            if (StringUtils.hasText(linkUrl)) {
                task.setLinkUrl(linkUrl.trim());
            }
            task.setCategory(DatasetTaskCategories.normalize(cells.get(COL_CATEGORY)));
            task.setConversationKey(DatasetConversationFields.normalizeKey(cells.get(COL_CONV)));
            task.setTurnIndex(DatasetConversationFields.parseTurnIndex(cells.get(COL_TURN)));
            String prompt = cells.get(COL_PROMPT);
            if (StringUtils.hasText(prompt)) {
                task.setPrompt(prompt.trim());
            } else {
                task.setPrompt(buildPrompt(extraFields, taskInstruction));
            }
            return task;
        }

        private static void addUrl(List<String> urls, String raw) {
            if (!StringUtils.hasText(raw)) {
                return;
            }
            String url = raw.trim();
            if (!urls.contains(url)) {
                urls.add(url);
            }
        }

        private static void putExtra(Map<String, Object> extraFields, String key, String value) {
            if (StringUtils.hasText(value)) {
                extraFields.put(key, value.trim());
            }
        }

        private static Map<String, Object> previewOf(DatasetTask task) {
            Map<String, Object> row = new LinkedHashMap<>();
            row.put("id", task.getTaskKey());
            row.put("prompt", task.getPrompt());
            row.put("linkUrl", task.getLinkUrl());
            row.put("referImages", new ArrayList<>(task.getReferImages()));
            row.put("extraFields", new LinkedHashMap<>(task.getExtraFields()));
            row.put("category", task.getCategory());
            row.put("conversationKey", task.getConversationKey());
            row.put("turnIndex", task.getTurnIndex());
            return row;
        }
    }
}
