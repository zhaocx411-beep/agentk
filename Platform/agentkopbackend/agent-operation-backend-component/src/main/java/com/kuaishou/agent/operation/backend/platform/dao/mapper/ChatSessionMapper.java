package com.kuaishou.agent.operation.backend.platform.dao.mapper;

import java.util.List;

import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Param;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;

@Mapper
public interface ChatSessionMapper extends BaseMapper<ChatSession> {

    int insertSession(ChatSession session);

    int updateSession(ChatSession session);

    ChatSession selectSessionById(@Param("id") long id);

    ChatSession selectByPublicId(@Param("publicId") String publicId);

    ChatSession selectByAgentkSessionId(@Param("agentkSessionId") String agentkSessionId);

    List<ChatSession> listByUserId(@Param("userId") String userId, @Param("limit") int limit);
}
