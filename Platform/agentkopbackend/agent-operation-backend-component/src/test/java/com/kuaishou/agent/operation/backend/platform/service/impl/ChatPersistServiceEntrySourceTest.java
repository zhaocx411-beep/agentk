package com.kuaishou.agent.operation.backend.platform.service.impl;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;

import com.baomidou.mybatisplus.core.conditions.Wrapper;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
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
import com.kuaishou.agent.operation.backend.platform.entity.RunEvent;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.agent100.Agent100Constants;
import com.kuaishou.agent.operation.backend.platform.support.ChatEntrySources;
import com.kuaishou.agent.operation.backend.platform.support.OfficialDemoAssets;

/**
 * {@code aop_session_event.entry_source} 是会话入口的冗余副本，离线跑数靠它免 join 筛事件、分批删除。
 */
class ChatPersistServiceEntrySourceTest {

    private ChatSessionMapper chatSessionMapper;
    private RunEventMapper runEventMapper;
    private ChatPersistServiceImpl service;

    @BeforeEach
    void setUp() {
        chatSessionMapper = mock(ChatSessionMapper.class);
        runEventMapper = mock(RunEventMapper.class);
        service = new ChatPersistServiceImpl(chatSessionMapper, mock(ChatRunMapper.class),
                mock(ChatMessageMapper.class), mock(ChatAssetMapper.class), mock(ChatJobMapper.class),
                mock(PipelineExecMapper.class), mock(AgentkCallLogMapper.class), runEventMapper,
                mock(AgentMapper.class), mock(AgentVersionMapper.class), mock(WorkflowMapper.class),
                mock(WorkflowVersionMapper.class), new ObjectMapper(),
                mock(BatchevalOfflineJobCellMapper.class));
    }

    @Test
    void shouldStampExplicitEntrySourceOnNewEvent() {
        givenSession("ses_off", "offline_batch");

        service.persistRunEvent("ses_off", "exe_1", 1, "round.started", payload(),
                "2026-08-21T10:00:00Z", "rnd_1", "evt_1", "exe_1", ChatEntrySources.OFFLINE_BATCH);

        assertThat(captureInserted().getEntrySource()).isEqualTo(ChatEntrySources.OFFLINE_BATCH);
    }

    @Test
    void shouldFollowSessionEntrySourceWhenNotGiven() {
        givenSession("ses_off", "offline_batch");

        service.persistRunEvent("ses_off", "exe_1", 1, "round.started", payload(),
                "2026-08-21T10:00:00Z", "rnd_1", "evt_1", "exe_1");

        assertThat(captureInserted().getEntrySource()).isEqualTo(ChatEntrySources.OFFLINE_BATCH);
    }

    @Test
    void shouldDefaultToOnlineWhenSessionHasNoEntrySource() {
        givenSession("ses_on", "");

        service.persistRunEvent("ses_on", "exe_2", 1, "round.started", payload(), "2026-08-21T10:00:00Z");

        assertThat(captureInserted().getEntrySource()).isEqualTo(ChatEntrySources.ONLINE);
    }

    @Test
    void shouldStampOnlineOnNewSessionWhenEntrySourceMissing() {
        ChatPersistService.EffectiveSnapshot effective = new ChatPersistService.EffectiveSnapshot();
        effective.setSessionKind("agent_loop");

        service.beginCreateSession("alice", "", "新对话", 1L, null, "{}", "{}", effective);

        assertThat(captureInsertedSession().getEntrySource()).isEqualTo(ChatEntrySources.ONLINE);
    }

    @Test
    void shouldKeepExplicitEntrySourceOnNewSession() {
        ChatPersistService.EffectiveSnapshot effective = new ChatPersistService.EffectiveSnapshot();
        effective.setSessionKind("agent_loop");
        effective.setEntrySource(ChatEntrySources.OFFLINE_BATCH);

        service.beginCreateSession("alice", "", "job/t1", 1L, null, "{}", "{}", effective);

        assertThat(captureInsertedSession().getEntrySource()).isEqualTo(ChatEntrySources.OFFLINE_BATCH);
    }

    @Test
    void shouldKeepAgent100EntrySourceOnNewSession() {
        ChatPersistService.EffectiveSnapshot effective = new ChatPersistService.EffectiveSnapshot();
        effective.setSessionKind("workflow");
        effective.setEntrySource(Agent100Constants.ENTRY_SOURCE);

        service.beginCreateSession("alice", "", "Narrato", null, null, "{}", "{}", effective);

        assertThat(captureInsertedSession().getEntrySource()).isEqualTo(Agent100Constants.ENTRY_SOURCE);
    }

    @Test
    void shouldKeepCapabilityLabEntrySourceOnNewSession() {
        ChatPersistService.EffectiveSnapshot effective = new ChatPersistService.EffectiveSnapshot();
        effective.setSessionKind("custom");
        effective.setEntrySource(OfficialDemoAssets.ENTRY_SOURCE_CAPABILITY_LAB);

        service.beginCreateSession("alice", "", "Lab", null, null, "{}", "{}", effective);

        assertThat(captureInsertedSession().getEntrySource())
                .isEqualTo(OfficialDemoAssets.ENTRY_SOURCE_CAPABILITY_LAB);
    }

    @Test
    void shouldTreatOfficialQuickAsOnline() {
        givenSession("ses_on", "official_quick");

        service.persistRunEvent("ses_on", "exe_2", 1, "round.started", payload(), "2026-08-21T10:00:00Z");

        assertThat(captureInserted().getEntrySource()).isEqualTo(ChatEntrySources.ONLINE);
    }

    private ChatSession captureInsertedSession() {
        ArgumentCaptor<ChatSession> captor = ArgumentCaptor.forClass(ChatSession.class);
        verify(chatSessionMapper).insert(captor.capture());
        return captor.getValue();
    }

    private void givenSession(String agentkSessionId, String entrySource) {
        ChatSession session = new ChatSession();
        session.setId(7L);
        session.setAgentkSessionId(agentkSessionId);
        session.setEntrySource(entrySource);
        when(chatSessionMapper.selectOne(any(Wrapper.class))).thenReturn(session);
    }

    private RunEvent captureInserted() {
        ArgumentCaptor<RunEvent> captor = ArgumentCaptor.forClass(RunEvent.class);
        verify(runEventMapper).insert(captor.capture());
        return captor.getValue();
    }

    private static ObjectNode payload() {
        return new ObjectMapper().createObjectNode().put("model", "m");
    }
}
