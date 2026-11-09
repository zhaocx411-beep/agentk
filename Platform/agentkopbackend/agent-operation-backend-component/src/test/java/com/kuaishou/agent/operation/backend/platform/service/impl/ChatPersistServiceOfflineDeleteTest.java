package com.kuaishou.agent.operation.backend.platform.service.impl;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.atLeastOnce;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import java.util.Arrays;
import java.util.Collections;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import com.baomidou.mybatisplus.core.conditions.Wrapper;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.batcheval.mapper.persistence.BatchevalOfflineJobCellMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.AgentMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.AgentVersionMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.AgentkCallLogMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.ChatAssetMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.ChatJobMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.ChatMessageMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.ChatRunMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.ChatSessionMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.PipelineExecMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.RunEventMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.WorkflowMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.WorkflowVersionMapper;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;

class ChatPersistServiceOfflineDeleteTest {

    private ChatSessionMapper chatSessionMapper;
    private ChatRunMapper chatRunMapper;
    private ChatMessageMapper chatMessageMapper;
    private ChatAssetMapper chatAssetMapper;
    private ChatJobMapper chatJobMapper;
    private PipelineExecMapper pipelineExecMapper;
    private AgentkCallLogMapper callLogMapper;
    private RunEventMapper runEventMapper;
    private ChatPersistServiceImpl service;

    @BeforeEach
    void setUp() {
        chatSessionMapper = mock(ChatSessionMapper.class);
        chatRunMapper = mock(ChatRunMapper.class);
        chatMessageMapper = mock(ChatMessageMapper.class);
        chatAssetMapper = mock(ChatAssetMapper.class);
        chatJobMapper = mock(ChatJobMapper.class);
        pipelineExecMapper = mock(PipelineExecMapper.class);
        callLogMapper = mock(AgentkCallLogMapper.class);
        runEventMapper = mock(RunEventMapper.class);
        service = new ChatPersistServiceImpl(chatSessionMapper, chatRunMapper, chatMessageMapper,
                chatAssetMapper, chatJobMapper, pipelineExecMapper, callLogMapper, runEventMapper,
                mock(AgentMapper.class), mock(AgentVersionMapper.class), mock(WorkflowMapper.class),
                mock(WorkflowVersionMapper.class), new ObjectMapper(),
                mock(BatchevalOfflineJobCellMapper.class));
    }

    @Test
    void shouldCascadeDeleteSessionScopedRowsOnceEventsAreDrained() {
        ChatSession session = new ChatSession();
        session.setId(41L);
        session.setAgentkSessionId("ses_off");
        session.setEntrySource("offline_batch");
        when(chatSessionMapper.selectList(any())).thenReturn(Collections.singletonList(session));
        when(runEventMapper.delete(any(Wrapper.class))).thenReturn(0);

        int deleted = service.purgeOfflineBatchPersisted("eval_abc",
                Collections.singletonList("ses_off"), 500);

        assertThat(deleted).isEqualTo(0);
        verify(runEventMapper).delete(any(Wrapper.class));
        verify(chatAssetMapper, atLeastOnce()).delete(any(Wrapper.class));
        verify(callLogMapper, atLeastOnce()).delete(any(Wrapper.class));
        verify(chatSessionMapper).delete(any(Wrapper.class));
        verify(chatJobMapper, never()).delete(any(Wrapper.class));
        verify(chatMessageMapper, never()).delete(any(Wrapper.class));
        verify(pipelineExecMapper, never()).delete(any(Wrapper.class));
        verify(chatRunMapper, never()).delete(any(Wrapper.class));
    }

    /** 事件没删完就不能动会话：否则剩下的事件成了查不到出处的孤儿。 */
    @Test
    void shouldDeleteEventsFirstAndReturnBeforeTouchingSessions() {
        when(runEventMapper.delete(any(Wrapper.class))).thenReturn(500);

        int deleted = service.purgeOfflineBatchPersisted("eval_abc",
                Collections.singletonList("ses_off"), 500);

        assertThat(deleted).isEqualTo(500);
        verify(runEventMapper).delete(any(Wrapper.class));
        verify(chatAssetMapper, never()).delete(any(Wrapper.class));
        verify(callLogMapper, never()).delete(any(Wrapper.class));
        verify(chatSessionMapper, never()).delete(any(Wrapper.class));
    }

    /** 单次调用的删除总量卡在 batchSize 内，多个 session 一起分摊，不让事务膨胀。 */
    @Test
    void shouldCapOneCallAtBatchSizeAcrossSessions() {
        when(runEventMapper.delete(any(Wrapper.class))).thenReturn(6, 4, 3);

        int deleted = service.purgeOfflineBatchPersisted("eval_abc",
                Arrays.asList("ses_a", "ses_b", "ses_c"), 10);

        assertThat(deleted).isEqualTo(10);
        verify(runEventMapper, times(2)).delete(any(Wrapper.class));
        verify(chatSessionMapper, never()).delete(any(Wrapper.class));
    }

    @Test
    void shouldSkipSessionCascadeWhenNoOfflineSessionFound() {
        when(chatSessionMapper.selectList(any())).thenReturn(Collections.emptyList());
        when(runEventMapper.delete(any(Wrapper.class))).thenReturn(0);

        service.purgeOfflineBatchPersisted("eval_missing", Collections.singletonList("ses_gone"), 500);

        verify(chatSessionMapper, never()).delete(any(Wrapper.class));
        verify(chatRunMapper, never()).delete(any(Wrapper.class));
        verify(chatAssetMapper).delete(any(Wrapper.class));
        verify(callLogMapper).delete(any(Wrapper.class));
    }

    @Test
    void shouldCountOfflineEventsOnlyForGivenSessions() {
        when(runEventMapper.selectCount(any())).thenReturn(37);

        assertThat(service.countOfflineBatchEvents(Collections.singletonList("ses_off"))).isEqualTo(37L);
        assertThat(service.countOfflineBatchEvents(Collections.emptyList())).isEqualTo(0L);
    }
}
