package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import java.io.IOException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;

/**
 * 将离线 job 跑数结果导出为 JSON（metrics / 产物 / 错误 / 每条用例完整 events）。
 * <p>
 * 事件从 {@code aop_session_event} 按写入顺序取出，不再依赖 {@code trace_json} 大字段。
 */
final class OfflineJobResultExporter {

    private static final ObjectMapper JSON = new ObjectMapper()
            .registerModule(new JavaTimeModule())
            .disable(SerializationFeature.WRITE_DATES_AS_TIMESTAMPS)
            .enable(SerializationFeature.INDENT_OUTPUT);

    private OfflineJobResultExporter() {
    }

    interface CellEvents {
        JsonNode events(OfflineJobCell cell);

        String eventsFrom(OfflineJobCell cell);
    }

    static byte[] writeJson(OfflineJob job, CellEvents cellEvents) {
        Map<String, Object> root = new LinkedHashMap<>();
        root.put("bizId", job.getBizId());
        root.put("datasetId", job.getDatasetId());
        root.put("agentId", job.getAgentId());
        root.put("agentVersionId", job.getAgentVersionId());
        root.put("agentName", job.getAgentName());
        root.put("status", job.getStatus());
        root.put("model", job.getModel());
        root.put("pipeline", job.getPipelineName());
        List<Map<String, Object>> cells = new ArrayList<>();
        for (OfflineJobCell cell : sorted(job.getCells())) {
            cells.add(toJsonCell(cell, cellEvents));
        }
        root.put("cells", cells);
        try {
            return JSON.writeValueAsBytes(root);
        } catch (IOException e) {
            throw new IllegalStateException("导出离线跑数 JSON 失败", e);
        }
    }

    private static List<OfflineJobCell> sorted(List<OfflineJobCell> cells) {
        List<OfflineJobCell> sorted = new ArrayList<>(
                cells == null ? Collections.<OfflineJobCell>emptyList() : cells);
        sorted.sort(Comparator
                .comparing(OfflineJobCell::getTaskKey, Comparator.nullsLast(String::compareTo))
                .thenComparingLong(OfflineJobCell::getId));
        return sorted;
    }

    private static Map<String, Object> toJsonCell(OfflineJobCell cell, CellEvents cellEvents) {
        Map<String, Object> row = new LinkedHashMap<>();
        row.put("taskKey", cell.getTaskKey());
        row.put("prompt", cell.getPrompt());
        row.put("status", cell.getStatus());
        row.put("agentk_session_id", cell.getAgentkSessionId());
        row.put("agent_run_id", cell.getAgentkRunId());
        row.put("metrics_json", parseJsonOrRaw(cell.getMetricsJson()));
        row.put("error_message", cell.getError());
        row.put("started_at", cell.getStartedAt());
        row.put("finished_at", cell.getFinishedAt());
        row.put("output_uris", parseJsonOrRaw(cell.getOutputUris()));
        if (cellEvents != null) {
            JsonNode events = cellEvents.events(cell);
            row.put("events", events == null
                    ? Collections.emptyList() : JSON.convertValue(events, Object.class));
            row.put("eventsFrom", cellEvents.eventsFrom(cell));
        }
        return row;
    }

    private static Object parseJsonOrRaw(String raw) {
        if (raw == null || raw.trim().isEmpty()) {
            return null;
        }
        try {
            JsonNode node = JSON.readTree(raw);
            return JSON.convertValue(node, Object.class);
        } catch (IOException e) {
            return raw;
        }
    }
}
