package com.kuaishou.agent.operation.backend.platform.dao.mapper;

import java.util.List;

import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Param;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import com.kuaishou.agent.operation.backend.platform.entity.ChatMessage;

@Mapper
public interface ChatMessageMapper extends BaseMapper<ChatMessage> {

    int insertMessage(ChatMessage message);

    int updateMessage(ChatMessage message);

    List<ChatMessage> listBySessionId(@Param("sessionId") long sessionId, @Param("limit") int limit);

    ChatMessage selectBySessionAndEventSeq(@Param("sessionId") long sessionId,
            @Param("eventSeq") int eventSeq);
}
