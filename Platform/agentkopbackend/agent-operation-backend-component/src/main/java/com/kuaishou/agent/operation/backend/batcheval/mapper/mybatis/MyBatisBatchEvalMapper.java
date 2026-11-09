package com.kuaishou.agent.operation.backend.batcheval.mapper.mybatis;

import java.io.IOException;
import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;

import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.stereotype.Repository;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetListFilter;
import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetTaskListFilter;
import com.kuaishou.agent.operation.backend.batcheval.mapper.BatchEvalMapper;
import com.kuaishou.agent.operation.backend.batcheval.support.DatasetUsages;
import com.kuaishou.agent.operation.backend.batcheval.mapper.persistence.BatchevalDatasetMapper;
import com.kuaishou.agent.operation.backend.batcheval.mapper.persistence.BatchevalDatasetTaskMapper;
import com.kuaishou.agent.operation.backend.batcheval.mapper.persistence.BatchevalOfflineJobCellMapper;
import com.kuaishou.agent.operation.backend.batcheval.mapper.persistence.BatchevalOfflineJobMapper;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.Dataset;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;

@Repository
@ConditionalOnProperty(name = "batcheval.store", havingValue = "mybatis", matchIfMissing = true)
public class MyBatisBatchEvalMapper implements BatchEvalMapper {

    private static final TypeReference<List<String>> STRING_LIST = new TypeReference<List<String>>() {
    };
    private static final TypeReference<Map<String, Object>> STRING_OBJECT_MAP =
            new TypeReference<Map<String, Object>>() {
            };

    private final BatchevalDatasetMapper datasetMapper;
    private final BatchevalDatasetTaskMapper datasetTaskMapper;
    private final BatchevalOfflineJobMapper offlineJobMapper;
    private final BatchevalOfflineJobCellMapper offlineJobCellMapper;
    private final ObjectMapper objectMapper;

    public MyBatisBatchEvalMapper(BatchevalDatasetMapper datasetMapper,
            BatchevalDatasetTaskMapper datasetTaskMapper,
            BatchevalOfflineJobMapper offlineJobMapper,
            BatchevalOfflineJobCellMapper offlineJobCellMapper,
            ObjectMapper objectMapper) {
        this.datasetMapper = datasetMapper;
        this.datasetTaskMapper = datasetTaskMapper;
        this.offlineJobMapper = offlineJobMapper;
        this.offlineJobCellMapper = offlineJobCellMapper;
        this.objectMapper = objectMapper;
    }

    @Override
    @Transactional
    public Dataset insertDataset(Dataset dataset) {
        LocalDateTime now = dataset.getCreatedAt() != null ? dataset.getCreatedAt() : LocalDateTime.now();
        int total = dataset.getTasks().size();
        dataset.setTotal(total);
        dataset.setCreator(nullToEmpty(dataset.getCreator()));
        if (!StringUtils.hasText(dataset.getUsage())) {
            dataset.setUsage(DatasetUsages.EVAL);
        }
        if (dataset.getCreatedAt() == null) {
            dataset.setCreatedAt(now);
        }
        if (dataset.getUpdatedAt() == null) {
            dataset.setUpdatedAt(now);
        }
        try {
            datasetMapper.insert(dataset);
        } catch (DuplicateKeyException e) {
            throw new IllegalArgumentException(
                    "数据集已存在: " + dataset.getName() + " / " + dataset.getVersion(), e);
        }
        if (dataset.getId() <= 0) {
            throw new IllegalStateException("insert dataset failed: no generated key");
        }
        insertTasks(dataset.getId(), dataset.getTasks(), now);
        return dataset;
    }

    private void insertTasks(long datasetId, List<DatasetTask> tasks, LocalDateTime now) {
        for (DatasetTask task : tasks) {
            if (task.getCreatedAt() == null) {
                task.setCreatedAt(now);
            }
            if (task.getUpdatedAt() == null) {
                task.setUpdatedAt(now);
            }
            encodeTaskJson(task);
            datasetTaskMapper.insert(datasetId, task);
            hydrateTask(task);
        }
    }

    @Override
    public long countDatasets(DatasetListFilter filter) {
        return datasetMapper.countByFilter(normalizeFilter(filter));
    }

    @Override
    public List<Dataset> listDatasets(DatasetListFilter filter, int offset, int limit) {
        if (limit <= 0 || offset < 0) {
            return Collections.emptyList();
        }
        return datasetMapper.listByFilter(normalizeFilter(filter), offset, limit);
    }

    @Override
    public void updateDataset(Dataset dataset) {
        int updated = datasetMapper.update(dataset);
        if (updated == 0) {
            throw new IllegalArgumentException("数据集不存在: " + dataset.getId());
        }
    }

    @Override
    @Transactional
    public boolean deleteDataset(long id) {
        datasetTaskMapper.deleteByDatasetId(id);
        return datasetMapper.deleteById(id) > 0;
    }

    @Override
    public long countJobsByDatasetId(long datasetId) {
        return offlineJobMapper.countByDatasetId(datasetId);
    }

    @Override
    public long countTasksByDatasetId(long datasetId) {
        Integer total = datasetMapper.selectTotal(datasetId);
        return total == null ? 0L : total.longValue();
    }

    @Override
    public List<DatasetTask> listAllTasks(long datasetId) {
        return hydrateTasks(datasetTaskMapper.listByDatasetId(datasetId));
    }

    @Override
    public List<DatasetTask> listTasks(long datasetId, int offset, int limit) {
        if (limit <= 0 || offset < 0) {
            return Collections.emptyList();
        }
        return hydrateTasks(datasetTaskMapper.listByDatasetIdPage(datasetId, offset, limit));
    }

    @Override
    public long countTasks(DatasetTaskListFilter filter) {
        return datasetTaskMapper.countByFilter(filter);
    }

    @Override
    public List<DatasetTask> listTasks(DatasetTaskListFilter filter, int offset, int limit) {
        if (limit <= 0 || offset < 0) {
            return Collections.emptyList();
        }
        return hydrateTasks(datasetTaskMapper.listByFilterPage(filter, offset, limit));
    }

    @Override
    public List<DatasetTask> listTasksRandom(DatasetTaskListFilter filter, int limit) {
        if (limit <= 0) {
            return Collections.emptyList();
        }
        return hydrateTasks(datasetTaskMapper.listByFilterRandom(filter, limit));
    }

    @Override
    public Optional<DatasetTask> findTask(long taskId) {
        DatasetTask task = datasetTaskMapper.findById(taskId);
        if (task == null) {
            return Optional.empty();
        }
        hydrateTask(task);
        return Optional.of(task);
    }

    @Override
    public Optional<Long> findDatasetIdByTaskId(long taskId) {
        Long datasetId = datasetTaskMapper.findDatasetIdByTaskId(taskId);
        return datasetId == null ? Optional.empty() : Optional.of(datasetId);
    }

    @Override
    @Transactional
    public DatasetTask insertTask(long datasetId, DatasetTask task) {
        if (!datasetExists(datasetId)) {
            throw new IllegalArgumentException("数据集不存在: " + datasetId);
        }
        LocalDateTime now = task.getCreatedAt() != null ? task.getCreatedAt() : LocalDateTime.now();
        LocalDateTime updatedAt = task.getUpdatedAt() != null ? task.getUpdatedAt() : now;
        task.setCreatedAt(now);
        task.setUpdatedAt(updatedAt);
        encodeTaskJson(task);
        try {
            datasetTaskMapper.insert(datasetId, task);
        } catch (DuplicateKeyException e) {
            throw new IllegalArgumentException("数据集 task id 重复: " + task.getTaskKey(), e);
        }
        adjustDatasetTotal(datasetId, 1, updatedAt);
        hydrateTask(task);
        return task;
    }

    private boolean datasetExists(long datasetId) {
        return datasetMapper.existsById(datasetId) > 0;
    }

    @Override
    public void updateTask(DatasetTask task) {
        encodeTaskJson(task);
        int updated = datasetTaskMapper.update(task);
        if (updated == 0) {
            throw new IllegalArgumentException("数据集用例不存在: " + task.getId());
        }
        findDatasetIdByTaskId(task.getId())
                .ifPresent(datasetId -> touchDatasetUpdatedAt(datasetId, task.getUpdatedAt()));
        hydrateTask(task);
    }

    @Override
    public boolean deleteTask(long taskId) {
        DatasetTask task = datasetTaskMapper.findById(taskId);
        if (task == null) {
            return false;
        }
        Long datasetId = datasetTaskMapper.findDatasetIdByTaskId(taskId);
        int deleted = datasetTaskMapper.deleteById(taskId);
        if (deleted > 0 && datasetId != null) {
            adjustDatasetTotal(datasetId, -1, LocalDateTime.now());
        }
        return deleted > 0;
    }

    private void adjustDatasetTotal(long datasetId, int delta, LocalDateTime updatedAt) {
        datasetMapper.adjustTotal(datasetId, delta, updatedAt);
    }

    private void touchDatasetUpdatedAt(long datasetId, LocalDateTime updatedAt) {
        datasetMapper.touchUpdatedAt(datasetId, updatedAt);
    }

    @Override
    public Optional<Dataset> findDataset(long id) {
        Dataset dataset = datasetMapper.findById(id);
        if (dataset == null) {
            return Optional.empty();
        }
        dataset.setTasks(hydrateTasks(datasetTaskMapper.listByDatasetId(id)));
        return Optional.of(dataset);
    }

    @Override
    @Transactional
    public OfflineJob insertJob(OfflineJob job) {
        if (job.getCreatedAt() == null) {
            job.setCreatedAt(LocalDateTime.now());
        }
        job.setCreator(nullToEmpty(job.getCreator()));
        offlineJobMapper.insert(job, writeToolsJson(job.getTools()));
        if (job.getId() <= 0) {
            throw new IllegalStateException("insert offline job failed: no generated key");
        }
        insertCells(job);
        return job;
    }

    private void insertCells(OfflineJob job) {
        for (OfflineJobCell cell : job.getCells()) {
            offlineJobCellMapper.insert(job.getId(), cell);
        }
    }

    @Override
    public long countJobs() {
        return offlineJobMapper.countAll();
    }

    @Override
    public List<OfflineJob> listJobs(int offset, int limit) {
        if (limit <= 0 || offset < 0) {
            return Collections.emptyList();
        }
        List<OfflineJob> jobs = offlineJobMapper.listPage(offset, limit);
        for (OfflineJob job : jobs) {
            hydrateTools(job);
            job.setCells(new ArrayList<>());
        }
        return jobs;
    }

    @Override
    public Optional<OfflineJob> findJob(String bizId) {
        OfflineJob job = offlineJobMapper.findByBizId(bizId);
        if (job == null) {
            return Optional.empty();
        }
        hydrateTools(job);
        job.setCells(offlineJobCellMapper.listByJobId(job.getId()));
        return Optional.of(job);
    }

    @Override
    public Optional<String> findCellTraceJson(String bizId, String taskKey) {
        if (bizId == null || taskKey == null) {
            return Optional.empty();
        }
        return Optional.ofNullable(offlineJobCellMapper.selectTraceJson(bizId, taskKey));
    }

    @Override
    public boolean markJobStatus(String bizId, String status) {
        return offlineJobMapper.markStatus(bizId, status) > 0;
    }

    @Override
    public List<String> listJobBizIdsByStatus(String status, int limit) {
        if (limit <= 0) {
            return Collections.emptyList();
        }
        return offlineJobMapper.listBizIdsByStatus(status, limit);
    }

    @Override
    public void updateJob(OfflineJob job) {
        int updated = offlineJobMapper.update(job);
        if (updated == 0) {
            throw new IllegalArgumentException("离线任务不存在: " + job.getBizId());
        }
    }

    @Override
    public OfflineJobCell insertJobCell(long jobId, OfflineJobCell cell) {
        offlineJobCellMapper.insert(jobId, cell);
        return cell;
    }

    @Override
    public void updateCell(String bizId, OfflineJobCell cell) {
        int updated = offlineJobCellMapper.updateByBizIdAndTaskKey(bizId, cell);
        if (updated == 0) {
            throw new IllegalArgumentException("离线任务单元不存在: " + cell.getTaskKey());
        }
    }

    @Override
    @Transactional
    public boolean deleteJob(String bizId) {
        OfflineJob job = offlineJobMapper.findByBizId(bizId);
        if (job == null) {
            return false;
        }
        offlineJobCellMapper.deleteByJobId(job.getId());
        return offlineJobMapper.deleteByBizId(bizId) > 0;
    }

    private void hydrateTools(OfflineJob job) {
        job.setTools(readToolsJson(job.getToolsJson()));
        job.setToolsJson(null);
    }

    private List<DatasetTask> hydrateTasks(List<DatasetTask> tasks) {
        if (tasks == null || tasks.isEmpty()) {
            return tasks == null ? Collections.emptyList() : tasks;
        }
        for (DatasetTask task : tasks) {
            hydrateTask(task);
        }
        return tasks;
    }

    private void hydrateTask(DatasetTask task) {
        if (task == null) {
            return;
        }
        task.setReferImages(readStringList(task.getReferImagesJson()));
        task.setExtraFields(readObjectMap(task.getExtraFieldsJson()));
        task.setReferImagesJson(null);
        task.setExtraFieldsJson(null);
    }

    private void encodeTaskJson(DatasetTask task) {
        task.setReferImagesJson(writeJson(task.getReferImages()));
        task.setExtraFieldsJson(writeJson(task.getExtraFields()));
    }

    private String writeJson(Object value) {
        if (value == null) {
            return null;
        }
        if (value instanceof List && ((List<?>) value).isEmpty()) {
            return null;
        }
        if (value instanceof Map && ((Map<?, ?>) value).isEmpty()) {
            return null;
        }
        try {
            return objectMapper.writeValueAsString(value);
        } catch (JsonProcessingException e) {
            throw new IllegalArgumentException("task JSON 序列化失败", e);
        }
    }

    private List<String> readStringList(String raw) {
        if (raw == null || raw.trim().isEmpty()) {
            return new ArrayList<>();
        }
        try {
            List<String> list = objectMapper.readValue(raw, STRING_LIST);
            return list == null ? new ArrayList<>() : list;
        } catch (IOException e) {
            throw new IllegalStateException("refer_images 损坏", e);
        }
    }

    private Map<String, Object> readObjectMap(String raw) {
        if (raw == null || raw.trim().isEmpty()) {
            return new LinkedHashMap<>();
        }
        try {
            Map<String, Object> map = objectMapper.readValue(raw, STRING_OBJECT_MAP);
            return map == null ? new LinkedHashMap<>() : new LinkedHashMap<>(map);
        } catch (IOException e) {
            throw new IllegalStateException("extra_fields 损坏", e);
        }
    }

    private String writeToolsJson(List<String> tools) {
        if (tools == null || tools.isEmpty()) {
            return "[]";
        }
        try {
            return objectMapper.writeValueAsString(tools);
        } catch (JsonProcessingException e) {
            throw new IllegalArgumentException("tools 序列化失败", e);
        }
    }

    private List<String> readToolsJson(String raw) {
        if (raw == null || raw.trim().isEmpty()) {
            return new ArrayList<>();
        }
        try {
            return objectMapper.readValue(raw, STRING_LIST);
        } catch (IOException e) {
            throw new IllegalStateException("tools_json 损坏", e);
        }
    }

    private static String nullToEmpty(String value) {
        return value == null ? "" : value;
    }

    private static DatasetListFilter normalizeFilter(DatasetListFilter filter) {
        if (filter == null) {
            return null;
        }
        DatasetListFilter normalized = new DatasetListFilter();
        normalized.setName(trimToNull(filter.getName()));
        normalized.setDatasetType(trimToNull(filter.getDatasetType()));
        normalized.setCreator(trimToNull(filter.getCreator()));
        // usage / pipelineName 必须透传，否则 datasets/query 筛选条件会被静默丢掉
        normalized.setUsage(trimToNull(filter.getUsage()));
        normalized.setPipelineName(trimToNull(filter.getPipelineName()));
        return normalized;
    }

    private static String trimToNull(String value) {
        if (value == null) {
            return null;
        }
        String trimmed = value.trim();
        return trimmed.isEmpty() ? null : trimmed;
    }
}
