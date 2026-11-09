package com.kuaishou.agent.operation.backend.batcheval.mapper.persistence;

import java.util.List;

import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Param;

import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;

@Mapper
public interface BatchevalOfflineJobCellMapper {

    int insert(@Param("jobId") long jobId, @Param("cell") OfflineJobCell cell);

    /** 不含 trace_json；大字段只在 trace 详情按 cell 单取。 */
    List<OfflineJobCell> listByJobId(@Param("jobId") long jobId);

    /** trace 摘要（cell 级），列表路径不取。 */
    String selectTraceJson(@Param("bizId") String bizId, @Param("taskKey") String taskKey);

    int updateByBizIdAndTaskKey(@Param("bizId") String bizId, @Param("cell") OfflineJobCell cell);

    int deleteByJobId(@Param("jobId") long jobId);

    /** agent-k 会话/回合是否属于离线批量跑数 cell。 */
    int countByAgentkRef(@Param("agentkSessionId") String agentkSessionId,
            @Param("agentkRunId") String agentkRunId);
}
