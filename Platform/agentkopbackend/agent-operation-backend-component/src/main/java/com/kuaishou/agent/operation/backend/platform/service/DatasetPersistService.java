package com.kuaishou.agent.operation.backend.platform.service;

import java.util.List;

import com.kuaishou.agent.operation.backend.platform.entity.Dataset;
import com.kuaishou.agent.operation.backend.platform.entity.DatasetSnapshot;

/**
 * Dataset 定义 + 不可变 Snapshot 写入（不含 EvalRun）。
 */
public interface DatasetPersistService {

    Dataset createDataset(String name, String description, String keyColumn, String inputMappingJson,
            String userId, String tenantId);

    Dataset updateMapping(long datasetId, String keyColumn, String inputMappingJson);

    DatasetSnapshot uploadCsvSnapshot(long datasetId, String csvText, String userId);

    List<Dataset> listMine(String userId);

    List<DatasetSnapshot> listSnapshots(long datasetId);

    DatasetSnapshot requireSnapshot(long snapshotId);
}
