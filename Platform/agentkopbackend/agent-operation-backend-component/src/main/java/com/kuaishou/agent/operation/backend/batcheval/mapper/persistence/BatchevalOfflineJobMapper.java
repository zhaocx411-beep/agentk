package com.kuaishou.agent.operation.backend.batcheval.mapper.persistence;

import java.util.List;

import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Param;

import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;

@Mapper
public interface BatchevalOfflineJobMapper {

    int insert(@Param("job") OfflineJob job, @Param("toolsJson") String toolsJson);

    /** 不含 DELETING（已删除、待后台清理）。 */
    long countAll();

    /** 不含 DELETING（已删除、待后台清理）。 */
    List<OfflineJob> listPage(@Param("offset") int offset, @Param("limit") int limit);

    int markStatus(@Param("bizId") String bizId, @Param("status") String status);

    List<String> listBizIdsByStatus(@Param("status") String status, @Param("limit") int limit);

    OfflineJob findByBizId(@Param("bizId") String bizId);

    int update(OfflineJob job);

    long countByDatasetId(@Param("datasetId") long datasetId);

    int deleteByBizId(@Param("bizId") String bizId);
}
