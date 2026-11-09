package com.kuaishou.agent.operation.backend.platform.service.impl;

import java.nio.charset.StandardCharsets;
import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.DatasetMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.DatasetSnapshotMapper;
import com.kuaishou.agent.operation.backend.platform.entity.Dataset;
import com.kuaishou.agent.operation.backend.platform.entity.DatasetSnapshot;
import com.kuaishou.agent.operation.backend.platform.service.DatasetPersistService;
import com.kuaishou.agent.operation.backend.platform.support.PlatformIds;

/**
 * Dataset 持久化实现（MyBatis-Plus）。
 */
@Service
public class DatasetPersistServiceImpl implements DatasetPersistService {

    private static final int PREVIEW_ROWS = 20;

    private final DatasetMapper datasetMapper;
    private final DatasetSnapshotMapper datasetSnapshotMapper;
    private final ObjectMapper objectMapper;

    public DatasetPersistServiceImpl(DatasetMapper datasetMapper, DatasetSnapshotMapper datasetSnapshotMapper,
            ObjectMapper objectMapper) {
        this.datasetMapper = datasetMapper;
        this.datasetSnapshotMapper = datasetSnapshotMapper;
        this.objectMapper = objectMapper;
    }

    @Override
    public Dataset createDataset(String name, String description, String keyColumn, String inputMappingJson,
            String userId, String tenantId) {
        LocalDateTime now = LocalDateTime.now();
        Dataset ds = new Dataset();
        ds.setName(require(name, "name"));
        ds.setDescription(empty(description));
        ds.setKeyColumn(empty(keyColumn));
        ds.setInputMappingJson(inputMappingJson);
        ds.setOwnerUserId(empty(userId));
        ds.setTenantId(empty(tenantId));
        ds.setLatestSnapshotVersion(0);
        ds.setStatus("active");
        ds.setCreatedAt(now);
        ds.setUpdatedAt(now);
        datasetMapper.insert(ds);
        return ds;
    }

    @Override
    public Dataset updateMapping(long datasetId, String keyColumn, String inputMappingJson) {
        Dataset ds = datasetMapper.selectById(datasetId);
        if (ds == null) {
            throw new IllegalArgumentException("dataset not found: " + datasetId);
        }
        if (keyColumn != null) {
            ds.setKeyColumn(keyColumn);
        }
        if (inputMappingJson != null) {
            ds.setInputMappingJson(inputMappingJson);
        }
        ds.setUpdatedAt(LocalDateTime.now());
        datasetMapper.updateById(ds);
        return ds;
    }

    @Override
    public DatasetSnapshot uploadCsvSnapshot(long datasetId, String csvText, String userId) {
        Dataset ds = datasetMapper.selectById(datasetId);
        if (ds == null) {
            throw new IllegalArgumentException("dataset not found: " + datasetId);
        }
        String csv = require(csvText, "csvText");
        ParsedCsv parsed = parseCsv(csv);
        int next = (ds.getLatestSnapshotVersion() == null ? 0 : ds.getLatestSnapshotVersion()) + 1;
        String checksum = PlatformIds.sha256(csv);
        String previewJson;
        String columnsJson;
        try {
            columnsJson = objectMapper.writeValueAsString(parsed.columns);
            previewJson = objectMapper.writeValueAsString(
                    parsed.rows.subList(0, Math.min(PREVIEW_ROWS, parsed.rows.size())));
        } catch (Exception e) {
            throw new IllegalStateException("serialize snapshot failed", e);
        }

        DatasetSnapshot snap = new DatasetSnapshot();
        snap.setDatasetId(datasetId);
        snap.setVersion(next);
        snap.setColumnsJson(columnsJson);
        snap.setRowCount(parsed.rows.size());
        snap.setSchemaJson(columnsJson);
        snap.setChecksumSha256(checksum);
        snap.setSourceUri("memory://dataset/" + datasetId + "/v" + next + "/source.csv");
        snap.setSnapshotUri("memory://dataset/" + datasetId + "/v" + next + "/snapshot.jsonl");
        snap.setPreviewJson(previewJson);
        snap.setCreatedBy(empty(userId));
        snap.setCreatedAt(LocalDateTime.now());
        datasetSnapshotMapper.insert(snap);

        ds.setLatestSnapshotVersion(next);
        ds.setUpdatedAt(LocalDateTime.now());
        datasetMapper.updateById(ds);
        return snap;
    }

    @Override
    public List<Dataset> listMine(String userId) {
        return datasetMapper.selectList(new LambdaQueryWrapper<Dataset>()
                .eq(Dataset::getOwnerUserId, empty(userId))
                .orderByDesc(Dataset::getId));
    }

    @Override
    public List<DatasetSnapshot> listSnapshots(long datasetId) {
        return datasetSnapshotMapper.selectList(new LambdaQueryWrapper<DatasetSnapshot>()
                .eq(DatasetSnapshot::getDatasetId, datasetId)
                .orderByDesc(DatasetSnapshot::getVersion));
    }

    @Override
    public DatasetSnapshot requireSnapshot(long snapshotId) {
        DatasetSnapshot snap = datasetSnapshotMapper.selectById(snapshotId);
        if (snap == null) {
            throw new IllegalArgumentException("snapshot not found: " + snapshotId);
        }
        return snap;
    }

    private static ParsedCsv parseCsv(String csv) {
        String[] lines = csv.replace("\r\n", "\n").replace('\r', '\n').split("\n");
        if (lines.length == 0 || !StringUtils.hasText(lines[0])) {
            throw new IllegalArgumentException("csv header required");
        }
        if (csv.getBytes(StandardCharsets.UTF_8).length > 10 * 1024 * 1024) {
            throw new IllegalArgumentException("csv exceeds 10MB");
        }
        String[] headers = splitCsvLine(lines[0]);
        if (headers.length == 0 || headers.length > 100) {
            throw new IllegalArgumentException("columns must be 1..100");
        }
        List<Map<String, String>> rows = new ArrayList<>();
        for (int i = 1; i < lines.length; i++) {
            if (!StringUtils.hasText(lines[i])) {
                continue;
            }
            String[] cols = splitCsvLine(lines[i]);
            Map<String, String> row = new LinkedHashMap<>();
            for (int c = 0; c < headers.length; c++) {
                row.put(headers[c], c < cols.length ? cols[c] : "");
            }
            rows.add(row);
            if (rows.size() > 10000) {
                throw new IllegalArgumentException("rows exceed 10000");
            }
        }
        ParsedCsv parsed = new ParsedCsv();
        parsed.columns = Arrays.asList(headers);
        parsed.rows = rows;
        return parsed;
    }

    private static String[] splitCsvLine(String line) {
        return line.split(",", -1);
    }

    private static String require(String v, String field) {
        if (!StringUtils.hasText(v)) {
            throw new IllegalArgumentException(field + " required");
        }
        return v;
    }

    private static String empty(String s) {
        return s == null ? "" : s;
    }

    private static final class ParsedCsv {
        private List<String> columns;
        private List<Map<String, String>> rows;
    }
}
