package com.kuaishou.agent.operation.backend.batcheval.service;

import java.util.List;

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

public interface DatasetService {

    DatasetSummaryView upload(DatasetUploadRequest request);

    /**
     * 管理员上传素材/模版：强制 {@code usage=TEMPLATE}，仅 JSON，必须带 {@code pipelineName}。
     * 解析与落库复用 {@link #upload}。
     */
    DatasetSummaryView uploadTemplate(DatasetUploadRequest request);

    PagedResult<List<DatasetSummaryView>> list(DatasetListFilter filter, int current, int pageSize);

    DatasetSummaryView get(long id);

    DatasetSummaryView update(long id, UpdateDatasetRequest request);

    PagedResult<List<DatasetTaskView>> listTasks(long datasetId, int current, int pageSize);

    /**
     * 按 datasetId / usage / pipelineName 筛用例；三者至少传一个。
     * 可选 {@code category} 再按用例类目过滤。
     */
    PagedResult<List<DatasetTaskView>> listTasks(DatasetTaskListFilter filter, int current, int pageSize);

    DatasetTaskView createTask(long datasetId, CreateDatasetTaskRequest request);

    DatasetTaskView updateTask(long taskId, UpdateDatasetTaskRequest request);

    void deleteTask(long taskId);

    /**
     * 按 format 导出当前库内最新用例（含 referImages / extraFields）。
     *
     * @param format json（默认）/ xlsx
     */
    DatasetExportResult export(long id, String format);

    void delete(long id);
}
