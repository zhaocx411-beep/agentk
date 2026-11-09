package com.kuaishou.agent.operation.backend.batcheval.mapper.memory;

import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.time.LocalDateTime;
import java.util.Objects;
import java.util.Optional;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicLong;
import java.util.stream.Collectors;

import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.stereotype.Repository;

import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetListFilter;
import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetTaskListFilter;
import com.kuaishou.agent.operation.backend.batcheval.mapper.BatchEvalMapper;
import com.kuaishou.agent.operation.backend.batcheval.support.DatasetUsages;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.Dataset;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;
import com.kuaishou.agent.operation.backend.batcheval.support.OfflineJobStatuses;

/**
 * P0 demo 内存存储：进程重启后数据丢失，零 MySQL 依赖。
 */
@Repository
@ConditionalOnProperty(name = "batcheval.store", havingValue = "memory")
public class InMemoryBatchEvalMapper implements BatchEvalMapper {

    private final AtomicLong datasetIds = new AtomicLong();
    private final AtomicLong taskIds = new AtomicLong();
    private final AtomicLong jobIds = new AtomicLong();
    private final AtomicLong cellIds = new AtomicLong();
    private final ConcurrentHashMap<Long, Dataset> datasets = new ConcurrentHashMap<>();
    private final ConcurrentHashMap<String, OfflineJob> jobs = new ConcurrentHashMap<>();

    @Override
    public Dataset insertDataset(Dataset dataset) {
        ensureUniqueNameVersion(dataset.getName(), dataset.getVersion(), null);
        dataset.setId(datasetIds.incrementAndGet());
        if (!hasText(dataset.getUsage())) {
            dataset.setUsage(DatasetUsages.EVAL);
        }
        for (DatasetTask task : dataset.getTasks()) {
            task.setId(taskIds.incrementAndGet());
            task.setDatasetId(dataset.getId());
        }
        dataset.setTotal(dataset.getTasks().size());
        datasets.put(dataset.getId(), dataset);
        return dataset;
    }

    @Override
    public long countDatasets(DatasetListFilter filter) {
        return filteredDatasets(filter).size();
    }

    @Override
    public List<Dataset> listDatasets(DatasetListFilter filter, int offset, int limit) {
        return slice(filteredDatasets(filter), offset, limit);
    }

    private List<Dataset> filteredDatasets(DatasetListFilter filter) {
        return sortedDatasets().stream()
                .filter(dataset -> matchesFilter(dataset, filter))
                .collect(Collectors.toList());
    }

    private static boolean matchesFilter(Dataset dataset, DatasetListFilter filter) {
        if (filter == null) {
            return true;
        }
        if (hasText(filter.getName())
                && !filter.getName().trim().equals(dataset.getName())) {
            return false;
        }
        if (hasText(filter.getDatasetType())
                && !filter.getDatasetType().trim().equals(dataset.getDatasetType())) {
            return false;
        }
        if (hasText(filter.getCreator())
                && !filter.getCreator().trim().equals(dataset.getCreator())) {
            return false;
        }
        if (hasText(filter.getUsage())
                && !filter.getUsage().trim().equals(dataset.getUsage())) {
            return false;
        }
        if (hasText(filter.getPipelineName())
                && !filter.getPipelineName().trim().equals(dataset.getPipelineName())) {
            return false;
        }
        return true;
    }

    private List<Dataset> sortedDatasets() {
        List<Dataset> result = new ArrayList<>(datasets.values());
        result.sort(Comparator.comparingLong(Dataset::getId).reversed());
        return result;
    }

    @Override
    public Optional<Dataset> findDataset(long id) {
        return Optional.ofNullable(datasets.get(id));
    }

    @Override
    public void updateDataset(Dataset dataset) {
        Dataset existing = datasets.get(dataset.getId());
        if (existing == null) {
            throw new IllegalArgumentException("数据集不存在: " + dataset.getId());
        }
        existing.setDescription(dataset.getDescription());
        existing.setDatasetType(dataset.getDatasetType());
        existing.setUpdatedAt(dataset.getUpdatedAt());
    }

    @Override
    public boolean deleteDataset(long id) {
        return datasets.remove(id) != null;
    }

    @Override
    public long countJobsByDatasetId(long datasetId) {
        return jobs.values().stream()
                .filter(job -> job.getDatasetId() == datasetId)
                .count();
    }

    @Override
    public long countTasksByDatasetId(long datasetId) {
        Dataset dataset = datasets.get(datasetId);
        return dataset == null ? 0L : dataset.getTotal();
    }

    @Override
    public List<DatasetTask> listAllTasks(long datasetId) {
        Dataset dataset = datasets.get(datasetId);
        if (dataset == null) {
            return Collections.emptyList();
        }
        List<DatasetTask> sorted = new ArrayList<>(dataset.getTasks());
        sorted.sort(Comparator.comparingLong(DatasetTask::getId));
        for (DatasetTask task : sorted) {
            task.setDatasetId(datasetId);
        }
        return sorted;
    }

    @Override
    public List<DatasetTask> listTasks(long datasetId, int offset, int limit) {
        Dataset dataset = datasets.get(datasetId);
        if (dataset == null) {
            return Collections.emptyList();
        }
        List<DatasetTask> sorted = new ArrayList<>(dataset.getTasks());
        sorted.sort(Comparator.comparingLong(DatasetTask::getId));
        for (DatasetTask task : sorted) {
            task.setDatasetId(datasetId);
        }
        return slice(sorted, offset, limit);
    }

    @Override
    public long countTasks(DatasetTaskListFilter filter) {
        return filteredTasks(filter).size();
    }

    @Override
    public List<DatasetTask> listTasks(DatasetTaskListFilter filter, int offset, int limit) {
        return slice(filteredTasks(filter), offset, limit);
    }

    @Override
    public List<DatasetTask> listTasksRandom(DatasetTaskListFilter filter, int limit) {
        if (limit <= 0) {
            return Collections.emptyList();
        }
        List<DatasetTask> matched = new ArrayList<>(filteredTasks(filter));
        Collections.shuffle(matched);
        return slice(matched, 0, limit);
    }

    private List<DatasetTask> filteredTasks(DatasetTaskListFilter filter) {
        List<DatasetTask> matched = new ArrayList<>();
        for (Dataset dataset : sortedDatasets()) {
            if (!matchesTaskFilter(dataset, filter)) {
                continue;
            }
            List<DatasetTask> tasks = new ArrayList<>(dataset.getTasks());
            tasks.sort(Comparator.comparingLong(DatasetTask::getId));
            for (DatasetTask task : tasks) {
                if (!matchesTaskCategory(task, filter)) {
                    continue;
                }
                task.setDatasetId(dataset.getId());
                matched.add(task);
            }
        }
        matched.sort(Comparator.comparingLong(DatasetTask::getId));
        return matched;
    }

    private static boolean matchesTaskFilter(Dataset dataset, DatasetTaskListFilter filter) {
        if (filter == null) {
            return true;
        }
        if (filter.hasDatasetId() && dataset.getId() != filter.getDatasetId()) {
            return false;
        }
        if (filter.hasUsage()) {
            String usage = hasText(dataset.getUsage()) ? dataset.getUsage() : DatasetUsages.EVAL;
            if (!filter.getUsage().equals(usage)) {
                return false;
            }
        }
        if (filter.hasPipelineName()) {
            String pipelineName = dataset.getPipelineName() == null ? "" : dataset.getPipelineName();
            if (!filter.getPipelineName().equals(pipelineName)) {
                return false;
            }
        }
        return true;
    }

    private static boolean matchesTaskCategory(DatasetTask task, DatasetTaskListFilter filter) {
        if (filter == null || !filter.hasCategory()) {
            return true;
        }
        String category = task.getCategory() == null ? "" : task.getCategory();
        return filter.getCategory().equals(category);
    }

    @Override
    public Optional<DatasetTask> findTask(long taskId) {
        for (Dataset dataset : datasets.values()) {
            for (DatasetTask task : dataset.getTasks()) {
                if (task.getId() == taskId) {
                    return Optional.of(task);
                }
            }
        }
        return Optional.empty();
    }

    @Override
    public Optional<Long> findDatasetIdByTaskId(long taskId) {
        for (Dataset dataset : datasets.values()) {
            for (DatasetTask task : dataset.getTasks()) {
                if (task.getId() == taskId) {
                    return Optional.of(dataset.getId());
                }
            }
        }
        return Optional.empty();
    }

    @Override
    public DatasetTask insertTask(long datasetId, DatasetTask task) {
        Dataset dataset = datasets.get(datasetId);
        if (dataset == null) {
            throw new IllegalArgumentException("数据集不存在: " + datasetId);
        }
        if (dataset.getTasks().stream().anyMatch(existing -> existing.getTaskKey().equals(task.getTaskKey()))) {
            throw new DuplicateKeyException("task id 重复: " + task.getTaskKey());
        }
        task.setId(taskIds.incrementAndGet());
        task.setDatasetId(datasetId);
        dataset.getTasks().add(task);
        dataset.setTotal(dataset.getTasks().size());
        dataset.setUpdatedAt(task.getUpdatedAt());
        return task;
    }

    @Override
    public void updateTask(DatasetTask task) {
        DatasetTask existing = findTask(task.getId())
                .orElseThrow(() -> new IllegalArgumentException("数据集用例不存在: " + task.getId()));
        existing.setPrompt(task.getPrompt());
        existing.setLinkUrl(task.getLinkUrl());
        existing.setReferImages(task.getReferImages() == null
                ? new ArrayList<>() : new ArrayList<>(task.getReferImages()));
        existing.setExtraFields(task.getExtraFields() == null
                ? new LinkedHashMap<>() : new LinkedHashMap<>(task.getExtraFields()));
        existing.setCategory(task.getCategory());
        existing.setConversationKey(task.getConversationKey());
        existing.setTurnIndex(task.getTurnIndex());
        existing.setUpdatedAt(task.getUpdatedAt());
        findDatasetIdByTaskId(task.getId()).ifPresent(datasetId -> {
            Dataset dataset = datasets.get(datasetId);
            if (dataset != null) {
                dataset.setUpdatedAt(task.getUpdatedAt());
            }
        });
    }

    @Override
    public boolean deleteTask(long taskId) {
        for (Dataset dataset : datasets.values()) {
            for (int i = 0; i < dataset.getTasks().size(); i++) {
                if (dataset.getTasks().get(i).getId() == taskId) {
                    dataset.getTasks().remove(i);
                    dataset.setTotal(dataset.getTasks().size());
                    dataset.setUpdatedAt(LocalDateTime.now());
                    return true;
                }
            }
        }
        return false;
    }

    @Override
    public OfflineJob insertJob(OfflineJob job) {
        job.setId(jobIds.incrementAndGet());
        for (OfflineJobCell cell : job.getCells()) {
            cell.setId(cellIds.incrementAndGet());
        }
        jobs.put(job.getBizId(), job);
        return job;
    }

    @Override
    public long countJobs() {
        return sortedJobs().size();
    }

    @Override
    public List<OfflineJob> listJobs(int offset, int limit) {
        return slice(sortedJobs(), offset, limit);
    }

    /** 与 MySQL 实现一致：DELETING 已被删除、只等后台清理，不参与列表与计数。 */
    private List<OfflineJob> sortedJobs() {
        List<OfflineJob> result = new ArrayList<>();
        for (OfflineJob job : jobs.values()) {
            if (!OfflineJobStatuses.DELETING.equals(job.getStatus())) {
                result.add(job);
            }
        }
        result.sort(Comparator.comparing(OfflineJob::getCreatedAt, Comparator.nullsLast(Comparator.naturalOrder()))
                .reversed());
        return result;
    }

    private static <T> List<T> slice(List<T> sorted, int offset, int limit) {
        if (offset < 0 || limit <= 0 || offset >= sorted.size()) {
            return Collections.emptyList();
        }
        int to = Math.min(offset + limit, sorted.size());
        return new ArrayList<>(sorted.subList(offset, to));
    }

    @Override
    public Optional<OfflineJob> findJob(String bizId) {
        return Optional.ofNullable(jobs.get(bizId));
    }

    @Override
    public Optional<String> findCellTraceJson(String bizId, String taskKey) {
        OfflineJob job = jobs.get(bizId);
        if (job == null || job.getCells() == null || taskKey == null) {
            return Optional.empty();
        }
        return job.getCells().stream()
                .filter(cell -> taskKey.equals(cell.getTaskKey()))
                .map(OfflineJobCell::getTraceJson)
                .filter(Objects::nonNull)
                .findFirst();
    }

    @Override
    public void updateJob(OfflineJob job) {
        if (!jobs.containsKey(job.getBizId())) {
            throw new IllegalArgumentException("离线任务不存在: " + job.getBizId());
        }
        jobs.put(job.getBizId(), job);
    }

    @Override
    public boolean markJobStatus(String bizId, String status) {
        OfflineJob job = jobs.get(bizId);
        if (job == null) {
            return false;
        }
        job.setStatus(status);
        return true;
    }

    @Override
    public List<String> listJobBizIdsByStatus(String status, int limit) {
        List<String> bizIds = new ArrayList<>();
        if (limit <= 0) {
            return bizIds;
        }
        for (OfflineJob job : jobs.values()) {
            if (status != null && status.equals(job.getStatus())) {
                bizIds.add(job.getBizId());
                if (bizIds.size() >= limit) {
                    break;
                }
            }
        }
        return bizIds;
    }

    @Override
    public OfflineJobCell insertJobCell(long jobId, OfflineJobCell cell) {
        for (OfflineJob job : jobs.values()) {
            if (job.getId() == jobId) {
                cell.setId(cellIds.incrementAndGet());
                job.getCells().add(cell);
                return cell;
            }
        }
        throw new IllegalArgumentException("离线任务不存在: " + jobId);
    }

    @Override
    public void updateCell(String bizId, OfflineJobCell cell) {
        OfflineJob job = jobs.get(bizId);
        if (job == null) {
            throw new IllegalArgumentException("离线任务不存在: " + bizId);
        }
        for (int i = 0; i < job.getCells().size(); i++) {
            if (job.getCells().get(i).getTaskKey().equals(cell.getTaskKey())) {
                job.getCells().set(i, cell);
                return;
            }
        }
        throw new IllegalArgumentException("离线任务单元不存在: " + cell.getTaskKey());
    }

    @Override
    public boolean deleteJob(String bizId) {
        return jobs.remove(bizId) != null;
    }

    private void ensureUniqueNameVersion(String name, String version, Long excludeId) {
        for (Dataset existing : datasets.values()) {
            if (excludeId != null && existing.getId() == excludeId) {
                continue;
            }
            if (existing.getName().equals(name) && existing.getVersion().equals(version)) {
                throw new IllegalArgumentException("数据集已存在: " + name + " / " + version);
            }
        }
    }

    private static boolean hasText(String value) {
        return value != null && !value.trim().isEmpty();
    }
}
