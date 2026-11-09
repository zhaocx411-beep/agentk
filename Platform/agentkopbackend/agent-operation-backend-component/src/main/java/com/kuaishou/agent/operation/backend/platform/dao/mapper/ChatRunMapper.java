package com.kuaishou.agent.operation.backend.platform.dao.mapper;

import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Param;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import com.kuaishou.agent.operation.backend.platform.entity.ChatRun;

@Mapper
public interface ChatRunMapper extends BaseMapper<ChatRun> {

    int insertExecution(ChatRun run);

    int updateExecution(ChatRun run);

    ChatRun selectExecutionById(@Param("id") long id);

    ChatRun selectByAgentkExecutionId(@Param("agentkExecutionId") String agentkExecutionId);
}
