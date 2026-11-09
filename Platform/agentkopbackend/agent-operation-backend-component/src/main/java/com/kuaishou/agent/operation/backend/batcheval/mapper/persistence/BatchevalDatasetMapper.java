package com.kuaishou.agent.operation.backend.batcheval.mapper.persistence;

import java.time.LocalDateTime;
import java.util.List;

import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Param;

import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetListFilter;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.Dataset;

@Mapper
public interface BatchevalDatasetMapper {

    int insert(Dataset dataset);

    long countByFilter(@Param("filter") DatasetListFilter filter);

    List<Dataset> listByFilter(@Param("filter") DatasetListFilter filter,
            @Param("offset") int offset, @Param("limit") int limit);

    Dataset findById(@Param("id") long id);

    int update(Dataset dataset);

    int deleteById(@Param("id") long id);

    int existsById(@Param("id") long id);

    Integer selectTotal(@Param("id") long id);

    int adjustTotal(@Param("id") long id, @Param("delta") int delta,
            @Param("updatedAt") LocalDateTime updatedAt);

    int touchUpdatedAt(@Param("id") long id, @Param("updatedAt") LocalDateTime updatedAt);
}
