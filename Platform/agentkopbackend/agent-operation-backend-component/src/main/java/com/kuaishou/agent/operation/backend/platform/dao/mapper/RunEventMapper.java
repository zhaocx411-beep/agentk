package com.kuaishou.agent.operation.backend.platform.dao.mapper;

import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Param;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import com.kuaishou.agent.operation.backend.platform.entity.RunEvent;

@Mapper
public interface RunEventMapper extends BaseMapper<RunEvent> {

    int insertEvent(RunEvent event);

    RunEvent selectByAgentkExecutionIdAndSeq(@Param("agentkExecutionId") String agentkExecutionId,
            @Param("seq") int seq);
}
