package com.kuaishou.agent.operation.backend.batcheval.mapper;

import java.util.List;
import java.util.Optional;

import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetListFilter;
import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetTaskListFilter;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.Dataset;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;

/**
 * P0 存储端口。当前由内存实现支撑 demo，MyBatis 实现用于 MySQL 持久化。
 */
public interface BatchEvalMapper {

    Dataset insertDataset(Dataset dataset);

    long countDatasets(DatasetListFilter filter);

    List<Dataset> listDatasets(DatasetListFilter filter, int offset, int limit);

    Optional<Dataset> findDataset(long id);

    void updateDataset(Dataset dataset);

    /** 删除数据集；实现需同步删除 dataset 下全部 task。 */
    boolean deleteDataset(long id);

    long countJobsByDatasetId(long datasetId);

    long countTasksByDatasetId(long datasetId);

    long countTasks(DatasetTaskListFilter filter);

    List<DatasetTask> listAllTasks(long datasetId);

    List<DatasetTask> listTasks(long datasetId, int offset, int limit);

    List<DatasetTask> listTasks(DatasetTaskListFilter filter, int offset, int limit);

    /** 在 filter 命中范围内随机取最多 limit 条（MySQL 用 ORDER BY RAND()）。 */
    List<DatasetTask> listTasksRandom(DatasetTaskListFilter filter, int limit);

    Optional<DatasetTask> findTask(long taskId);

    Optional<Long> findDatasetIdByTaskId(long taskId);

    DatasetTask insertTask(long datasetId, DatasetTask task);

    void updateTask(DatasetTask task);

    boolean deleteTask(long taskId);

    OfflineJob insertJob(OfflineJob job);

    /** 不含 {@code DELETING}（已删除、后台待清理）。 */
    long countJobs();

    /** 不含 {@code DELETING}（已删除、后台待清理）。 */
    List<OfflineJob> listJobs(int offset, int limit);

    /**
     * 只改 status，不动 errors / 时间戳。
     * 用于把任务标成 {@code DELETING}：这是「已删除但子表还没清完」的唯一标记。
     *
     * @return true 若命中一行
     */
    boolean markJobStatus(String bizId, String status);

    /** 按状态取 bizId（清理器扫 {@code DELETING} 用），最多 {@code limit} 条。 */
    List<String> listJobBizIdsByStatus(String status, int limit);

    /** 返回的 cell 不含 trace_json（大字段），需要时用 {@link #findCellTraceJson}。 */
    Optional<OfflineJob> findJob(String bizId);

    /** 单个 cell 的 trace 摘要 JSON。 */
    Optional<String> findCellTraceJson(String bizId, String taskKey);

    void updateJob(OfflineJob job);

    OfflineJobCell insertJobCell(long jobId, OfflineJobCell cell);

    void updateCell(String bizId, OfflineJobCell cell);

    /** 删除离线任务；实现需同步删除全部 job cell。 */
    boolean deleteJob(String bizId);
}
