package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import java.io.ByteArrayOutputStream;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import org.springframework.util.StringUtils;

import com.alibaba.excel.EasyExcel;
import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;

/**
 * 按 JSON 用例字段写回 xlsx，与 {@link XlsxDatasetParser} 标准列对齐。
 */
final class XlsxDatasetWriter {

    private static final ObjectMapper JSON = new ObjectMapper();
    private static final List<String> HEADERS = Arrays.asList(
            "id", "prompt", "linkUrl", "referImages", "extraFields",
            "category", "conversationKey", "turnIndex");

    private XlsxDatasetWriter() {
    }

    static byte[] write(List<DatasetTask> tasks) {
        List<DatasetTask> sorted = new ArrayList<>(
                tasks == null ? Collections.<DatasetTask>emptyList() : tasks);
        sorted.sort(Comparator
                .comparing(DatasetTask::getTaskKey, Comparator.nullsLast(String::compareTo))
                .thenComparingLong(DatasetTask::getId));
        List<List<String>> rows = new ArrayList<>(sorted.size() + 1);
        rows.add(HEADERS);
        for (DatasetTask task : sorted) {
            rows.add(toRow(task));
        }
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        EasyExcel.write(out).sheet("sheet1").doWrite(rows);
        return out.toByteArray();
    }

    private static List<String> toRow(DatasetTask task) {
        return Arrays.asList(
                nullToEmpty(task.getTaskKey()),
                nullToEmpty(task.getPrompt()),
                nullToEmpty(task.getLinkUrl()),
                joinUrls(task.getReferImages()),
                writeExtraFields(task.getExtraFields()),
                nullToEmpty(task.getCategory()),
                nullToEmpty(task.getConversationKey()),
                task.getTurnIndex() == null ? "" : String.valueOf(task.getTurnIndex()));
    }

    private static String joinUrls(List<String> images) {
        if (images == null || images.isEmpty()) {
            return "";
        }
        StringBuilder sb = new StringBuilder();
        for (String url : images) {
            if (!StringUtils.hasText(url)) {
                continue;
            }
            if (sb.length() > 0) {
                sb.append('\n');
            }
            sb.append(url.trim());
        }
        return sb.toString();
    }

    private static String writeExtraFields(Map<String, Object> extra) {
        if (extra == null || extra.isEmpty()) {
            return "";
        }
        try {
            return JSON.writeValueAsString(new LinkedHashMap<>(extra));
        } catch (JsonProcessingException e) {
            throw new IllegalStateException("导出 extraFields 失败", e);
        }
    }

    private static String nullToEmpty(String value) {
        return value == null ? "" : value.trim();
    }
}
