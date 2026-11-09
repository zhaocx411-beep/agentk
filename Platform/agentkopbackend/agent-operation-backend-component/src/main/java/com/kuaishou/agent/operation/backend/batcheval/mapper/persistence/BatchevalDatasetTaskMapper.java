package com.kuaishou.agent.operation.backend.batcheval.mapper.persistence;

import java.util.List;

import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Param;

import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetTaskListFilter;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;

@Mapper
public interface BatchevalDatasetTaskMapper {

    int insert(@Param("datasetId") long datasetId, @Param("task") DatasetTask task);

    List<DatasetTask> listByDatasetId(@Param("datasetId") long datasetId);

    List<DatasetTask> listByDatasetIdPage(@Param("datasetId") long datasetId,
            @Param("offset") int offset, @Param("limit") int limit);

    long countByFilter(@Param("filter") DatasetTaskListFilter filter);

    List<DatasetTask> listByFilterPage(@Param("filter") DatasetTaskListFilter filter,
            @Param("offset") int offset, @Param("limit") int limit);

    List<DatasetTask> listByFilterRandom(@Param("filter") DatasetTaskListFilter filter,
            @Param("limit") int limit);

    DatasetTask findById(@Param("id") long id);

    Long findDatasetIdByTaskId(@Param("id") long taskId);

    int update(DatasetTask task);

    int deleteById(@Param("id") long id);

    int deleteByDatasetId(@Param("datasetId") long datasetId);
}
