package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyInt;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.doNothing;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.spy;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import java.time.LocalDateTime;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Optional;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import com.baomidou.mybatisplus.core.metadata.IPage;
import com.baomidou.mybatisplus.extension.plugins.pagination.Page;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkProperties;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.AgentConfigView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetExportResult;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.OfflineJobView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.SubmitOfflineJobRequest;
import com.kuaishou.agent.operation.backend.batcheval.mapper.memory.InMemoryBatchEvalMapper;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.Dataset;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;
import com.kuaishou.agent.operation.backend.batcheval.support.DatasetUsages;
import com.kuaishou.agent.operation.backend.batcheval.support.OfflineJobSessionMode;
import com.kuaishou.agent.operation.backend.platform.entity.Agent;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.entity.RunEvent;
import com.kuaishou.agent.operation.backend.platform.service.AgentVersionService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.service.OfficialDemoService;

class OfflineJobServiceImplTest {

    private InMemoryBatchEvalMapper mapper;
    private OfflineJobOrchestrator orchestrator;
    private OfficialDemoService officialDemoService;
    private AgentVersionService agentVersionService;
    private ChatPersistService chatPersistService;
    private OfflineJobPurger purger;
    private OfflineJobServiceImpl service;

    @BeforeEach
    void setUp() {
        mapper = new InMemoryBatchEvalMapper();
        orchestrator = mock(OfflineJobOrchestrator.class);
        officialDemoService = mock(OfficialDemoService.class);
        agentVersionService = mock(AgentVersionService.class);
        chatPersistService = mock(ChatPersistService.class);
        // spy：schedule 走线程池会与断言抢跑，异步分支只验证「有没有排上清理」
        purger = spy(new OfflineJobPurger(mapper, chatPersistService, new AgentkProperties()));
        doNothing().when(purger).schedule(anyString());
        service = new OfflineJobServiceImpl(mapper, orchestrator, new ObjectMapper(),
                officialDemoService, agentVersionService, chatPersistService, purger);
    }

    @Test
    void shouldRejectTemplateDatasetOnSubmit() {
        Dataset dataset = insertDataset(DatasetUsages.TEMPLATE, "generate_brand_ad");
        SubmitOfflineJobRequest request = new SubmitOfflineJobRequest();
        request.setDatasetId(dataset.getId());
        request.setAgentId(1L);

        assertThatThrownBy(() -> service.submit(request, "alice"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("模版数据集不能提交离线跑数");
    }

    @Test
    void shouldRejectWhenAgentIdMissing() {
        Dataset dataset = insertDataset(DatasetUsages.EVAL, null);
        SubmitOfflineJobRequest request = new SubmitOfflineJobRequest();
        request.setDatasetId(dataset.getId());

        assertThatThrownBy(() -> service.submit(request, "alice"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("agentId");
    }

    @Test
    void shouldSubmitWithBoundOfficialAgentProfileSnapshot() {
        Dataset dataset = insertDataset(DatasetUsages.EVAL, null);
        Agent agent = officialAgent(11L, "官方助手");
        AgentVersion version = publishedVersion(110L, 11L, 3,
                "{\"defaultModel\":\"m1\",\"system\":\"sys\",\"tools\":[{\"name\":\"echo\"}],"
                        + "\"pipelines\":[{\"name\":\"generate_brand_ad\","
                        + "\"ref\":\"pipeline:agentk.generate_brand_ad\"}]}");
        when(agentVersionService.findAgent(11L)).thenReturn(agent);
        when(officialDemoService.findPublishedVersion(agent)).thenReturn(Optional.of(version));

        SubmitOfflineJobRequest request = new SubmitOfflineJobRequest();
        request.setDatasetId(dataset.getId());
        request.setAgentId(11L);

        OfflineJobView view = service.submit(request, "alice");
        OfflineJob job = mapper.findJob(view.getBizId()).orElseThrow(AssertionError::new);
        assertThat(job.getAgentId()).isEqualTo(11L);
        assertThat(job.getAgentVersionId()).isEqualTo(110L);
        assertThat(job.getAgentName()).isEqualTo("官方助手");
        assertThat(job.getModel()).isEqualTo("m1");
        assertThat(job.getSystemPrompt()).isEqualTo("sys");
        assertThat(job.getTools()).containsExactly("echo");
        assertThat(job.getPipelineName()).isEqualTo("generate_brand_ad");
        assertThat(job.getPipelineRef()).isEqualTo("pipeline:agentk.generate_brand_ad");
        assertThat(job.getProfileYaml()).contains("\"defaultModel\":\"m1\"");
        assertThat(view.getAgentId()).isEqualTo(11L);
        assertThat(view.getAgentVersionId()).isEqualTo(110L);
        verify(orchestrator).start(eq(view.getBizId()), eq(OfflineJobSessionMode.PER_TASK));
    }

    @Test
    void shouldPassMultiRoundFlagToOrchestrator() {
        Dataset dataset = insertDataset(DatasetUsages.EVAL, null);
        Agent agent = officialAgent(12L, "官方助手");
        AgentVersion version = publishedVersion(120L, 12L, 3,
                "{\"defaultModel\":\"m1\",\"system\":\"sys\",\"tools\":[{\"name\":\"echo\"}]}");
        when(agentVersionService.findAgent(12L)).thenReturn(agent);
        when(officialDemoService.findPublishedVersion(agent)).thenReturn(Optional.of(version));

        SubmitOfflineJobRequest request = new SubmitOfflineJobRequest();
        request.setDatasetId(dataset.getId());
        request.setAgentId(12L);
        request.setMultiRoundConversation(true);

        OfflineJobView view = service.submit(request, "alice");

        verify(orchestrator).start(eq(view.getBizId()), eq(OfflineJobSessionMode.SINGLE_SESSION));
        assertThat(mapper.findJob(view.getBizId()).orElseThrow(AssertionError::new).getSessionMode())
                .isEqualTo("SINGLE_SESSION");
        assertThat(view.getSessionMode()).isEqualTo("SINGLE_SESSION");
    }

    @Test
    void shouldPreferSessionModeOverLegacyBoolean() {
        Dataset dataset = insertDataset(DatasetUsages.EVAL, null);
        dataset.getTasks().get(0).setConversationKey("conv-a");
        dataset.getTasks().get(0).setTurnIndex(1);
        Agent agent = officialAgent(13L, "官方助手");
        AgentVersion version = publishedVersion(130L, 13L, 3,
                "{\"defaultModel\":\"m1\",\"system\":\"sys\",\"tools\":[{\"name\":\"echo\"}]}");
        when(agentVersionService.findAgent(13L)).thenReturn(agent);
        when(officialDemoService.findPublishedVersion(agent)).thenReturn(Optional.of(version));

        SubmitOfflineJobRequest request = new SubmitOfflineJobRequest();
        request.setDatasetId(dataset.getId());
        request.setAgentId(13L);
        request.setSessionMode("GROUPED");
        request.setMultiRoundConversation(true);

        OfflineJobView view = service.submit(request, "alice");

        verify(orchestrator).start(eq(view.getBizId()), eq(OfflineJobSessionMode.GROUPED));
        OfflineJob job = mapper.findJob(view.getBizId()).orElseThrow(AssertionError::new);
        assertThat(job.getSessionMode()).isEqualTo("GROUPED");
        assertThat(job.getCells().get(0).getConversationKey()).isEqualTo("conv-a");
        assertThat(job.getCells().get(0).getTurnIndex()).isEqualTo(1);
    }

    @Test
    void shouldRejectAgentNotOwnedAndNotOfficial() {
        Dataset dataset = insertDataset(DatasetUsages.EVAL, null);
        Agent agent = new Agent();
        agent.setId(22L);
        agent.setName("别人的");
        agent.setStatus("active");
        agent.setIsDefault(false);
        agent.setOwnerUserId("bob");
        when(agentVersionService.findAgent(22L)).thenReturn(agent);

        SubmitOfflineJobRequest request = new SubmitOfflineJobRequest();
        request.setDatasetId(dataset.getId());
        request.setAgentId(22L);

        assertThatThrownBy(() -> service.submit(request, "alice"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("无权使用该 agent");
    }

    @Test
    void shouldListOfficialAndMineAgentsInConfig() {
        Agent official = officialAgent(1L, "官方A");
        Agent mine = personalAgent(2L, "我的B", "alice");
        AgentVersion v1 = publishedVersion(101L, 1L, 1, "{\"tools\":[{\"name\":\"echo\"}]}");
        AgentVersion v2 = publishedVersion(102L, 2L, 2, "{\"tools\":[{\"name\":\"echo\"}]}");
        when(officialDemoService.listDefaultAgents()).thenReturn(Collections.singletonList(official));
        when(agentVersionService.listMine("alice")).thenReturn(Arrays.asList(official, mine));
        when(officialDemoService.findPublishedVersion(any(Agent.class))).thenAnswer(invocation -> {
            Agent agent = invocation.getArgument(0);
            if (agent.getId() == 1L) {
                return Optional.of(v1);
            }
            if (agent.getId() == 2L) {
                return Optional.of(v2);
            }
            return Optional.empty();
        });

        AgentConfigView config = service.getAgentConfig("alice");
        assertThat(config.getAgents()).hasSize(2);
        assertThat(config.getAgents().get(0).getSource()).isEqualTo("official");
        assertThat(config.getAgents().get(0).getAgentId()).isEqualTo(1L);
        assertThat(config.getAgents().get(1).getSource()).isEqualTo("mine");
        assertThat(config.getAgents().get(1).getAgentId()).isEqualTo(2L);
        assertThat(config.getAgents().get(1).getAgentVersionId()).isEqualTo(102L);
        assertThat(config.getDefaultSessionMode()).isEqualTo("PER_TASK");
        assertThat(config.getSessionModes()).extracting(OfflineJobSessionMode.View::getCode)
                .containsExactly("PER_TASK", "SINGLE_SESSION", "GROUPED");
        assertThat(config.getSessionModes().get(0).getLabel()).isEqualTo("单轮");
        assertThat(config.getSessionModes().get(0).getDescription()).contains("可并发");
    }

    @Test
    void shouldExportJobResultsAsJsonWithEventsFromSessionTable() throws Exception {
        insertExportableJob();
        when(chatPersistService.pageSessionEventsInWriteOrder("sess_1", "run_1", 1, 2000))
                .thenReturn(eventPage(1, 2000, 1,
                        event(10L, 3, "assistant.tool_call", "{\"toolName\":\"generate_image\"}")));

        DatasetExportResult json = service.export("eval_export_demo");
        assertThat(json.getFormat()).isEqualTo("json");
        assertThat(json.getFilename()).isEqualTo("eval_export_demo-results.json");

        JsonNode root = new ObjectMapper().readTree(json.getBody());
        assertThat(root.path("bizId").asText()).isEqualTo("eval_export_demo");
        assertThat(root.path("agentId").asLong()).isEqualTo(9L);
        assertThat(root.path("cells")).hasSize(1);
        JsonNode cell = root.path("cells").get(0);
        assertThat(cell.path("taskKey").asText()).isEqualTo("t1");
        assertThat(cell.path("metrics_json").path("steps").asInt()).isEqualTo(1);
        assertThat(cell.path("output_uris").get(0).asText()).isEqualTo("https://cdn/a.png");
        assertThat(cell.has("trace_json")).isFalse();
        assertThat(cell.path("eventsFrom").asText()).isEqualTo("session_event");
        assertThat(cell.path("events")).hasSize(1);
        assertThat(cell.path("events").get(0).path("kind").asText()).isEqualTo("assistant.tool_call");
        assertThat(cell.path("events").get(0).path("data").path("toolName").asText())
                .isEqualTo("generate_image");
    }

    @Test
    void shouldExportAllEventPagesFromSessionTable() throws Exception {
        insertExportableJob();
        when(chatPersistService.pageSessionEventsInWriteOrder("sess_1", "run_1", 1, 2000))
                .thenReturn(eventPage(1, 2000, 2001, event(1L, 1, "round.started", "{}")));
        when(chatPersistService.pageSessionEventsInWriteOrder("sess_1", "run_1", 2, 2000))
                .thenReturn(eventPage(2, 2000, 2001, event(2L, 2, "execution.finished", "{}")));

        JsonNode cell = new ObjectMapper().readTree(service.export("eval_export_demo").getBody())
                .path("cells").get(0);

        assertThat(cell.path("eventsFrom").asText()).isEqualTo("session_event");
        assertThat(cell.path("events")).hasSize(2);
        assertThat(cell.path("events").get(0).path("kind").asText()).isEqualTo("round.started");
        assertThat(cell.path("events").get(1).path("kind").asText()).isEqualTo("execution.finished");
    }

    @Test
    void shouldExportLegacyTraceEventsWhenSessionTableEmpty() throws Exception {
        insertExportableJob();
        setTraceJson("{\"status\":\"completed\",\"events\":[{\"kind\":\"assistant.tool_call\"}]}");
        when(chatPersistService.pageSessionEventsInWriteOrder("sess_1", "run_1", 1, 2000))
                .thenReturn(eventPage(1, 2000, 0));

        JsonNode cell = new ObjectMapper().readTree(service.export("eval_export_demo").getBody())
                .path("cells").get(0);

        assertThat(cell.path("eventsFrom").asText()).isEqualTo("trace_snapshot");
        assertThat(cell.path("events")).hasSize(1);
        assertThat(cell.path("events").get(0).path("kind").asText()).isEqualTo("assistant.tool_call");
    }

    @Test
    void shouldBuildTraceEventsInWriteOrderNotSeqOrder() {
        insertExportableJob();
        // seq 递减、id 递增：还原顺序必须跟着 id，跟着 seq 就反了
        when(chatPersistService.pageSessionEventsInWriteOrder("sess_1", "run_1", 1, 200))
                .thenReturn(eventPage(1, 200, 2,
                        event(1L, 7, "assistant.text", "{\"text\":\"hi\"}"),
                        event(2L, 3, "job.completed", "{\"url\":\"https://cdn/a.mp4\"}")));

        JsonNode trace = service.getTrace("eval_export_demo", "t1", 1, 200);

        assertThat(trace.path("status").asText()).isEqualTo("completed");
        assertThat(trace.path("taskKey").asText()).isEqualTo("t1");
        assertThat(trace.path("sessionId").asText()).isEqualTo("sess_1");
        assertThat(trace.path("eventsFrom").asText()).isEqualTo("session_event");
        assertThat(trace.path("events")).hasSize(2);
        assertThat(trace.path("events").get(0).path("kind").asText()).isEqualTo("assistant.text");
        assertThat(trace.path("events").get(0).path("data").path("text").asText()).isEqualTo("hi");
        assertThat(trace.path("events").get(1).path("kind").asText()).isEqualTo("job.completed");
        assertThat(trace.path("events").get(1).path("executionId").asText()).isEqualTo("run_1");
        assertThat(trace.path("page").path("total").asInt()).isEqualTo(2);
        assertThat(trace.path("page").path("pages").asInt()).isEqualTo(1);
    }

    @Test
    void shouldCapTracePageSizeAndPassPagingDown() {
        insertExportableJob();
        when(chatPersistService.pageSessionEventsInWriteOrder("sess_1", "run_1", 2, 2000))
                .thenReturn(eventPage(2, 2000, 2500, event(501L, 1, "assistant.text", "{}")));

        JsonNode trace = service.getTrace("eval_export_demo", "t1", 2, 5000);

        assertThat(trace.path("page").path("current").asInt()).isEqualTo(2);
        assertThat(trace.path("page").path("pageSize").asInt()).isEqualTo(2000);
        assertThat(trace.path("page").path("pages").asInt()).isEqualTo(2);
        verify(chatPersistService).pageSessionEventsInWriteOrder("sess_1", "run_1", 2, 2000);
    }

    @Test
    void shouldFallBackToLegacySnapshotEventsWhenNoRowsPersisted() {
        insertExportableJob();
        setTraceJson("{\"status\":\"completed\",\"events\":[{\"kind\":\"a\"},{\"kind\":\"b\"}]}");
        when(chatPersistService.pageSessionEventsInWriteOrder("sess_1", "run_1", 1, 1))
                .thenReturn(eventPage(1, 1, 0));

        JsonNode trace = service.getTrace("eval_export_demo", "t1", 1, 1);

        assertThat(trace.path("eventsFrom").asText()).isEqualTo("trace_snapshot");
        assertThat(trace.path("events")).hasSize(1);
        assertThat(trace.path("events").get(0).path("kind").asText()).isEqualTo("a");
        assertThat(trace.path("page").path("total").asInt()).isEqualTo(2);
    }

    @Test
    void shouldReturnEmptyEventsWhenSummaryExistsButNothingPersisted() {
        insertExportableJob();
        when(chatPersistService.pageSessionEventsInWriteOrder("sess_1", "run_1", 1, 200))
                .thenReturn(eventPage(1, 200, 0));

        JsonNode trace = service.getTrace("eval_export_demo", "t1", 1, 200);

        assertThat(trace.path("status").asText()).isEqualTo("completed");
        assertThat(trace.path("events")).isEmpty();
    }

    @Test
    void shouldRejectTraceWhenSummaryMissing() {
        insertExportableJob();
        setTraceJson(null);
        when(chatPersistService.pageSessionEventsInWriteOrder("sess_1", "run_1", 1, 200))
                .thenReturn(eventPage(1, 200, 0));

        assertThatThrownBy(() -> service.getTrace("eval_export_demo", "t1", 1, 200))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("trace 尚未生成");
    }

    @Test
    void shouldRejectExportWhenJobMissing() {
        assertThatThrownBy(() -> service.export("missing"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("离线任务不存在");
    }

    @Test
    void shouldDeleteSmallJobInlineIncludingPersistedRows() {
        insertExportableJob();
        List<String> sessions = Collections.singletonList("sess_1");
        when(chatPersistService.countOfflineBatchEvents(sessions)).thenReturn(12L);
        // 第一批删 12 行，第二批返回 0 表示清空
        when(chatPersistService.purgeOfflineBatchPersisted("eval_export_demo", sessions, 500))
                .thenReturn(12, 0);

        service.delete("eval_export_demo");

        verify(chatPersistService, times(2))
                .purgeOfflineBatchPersisted("eval_export_demo", sessions, 500);
        assertThat(mapper.findJob("eval_export_demo")).isEmpty();
    }

    @Test
    void shouldOnlyMarkDeletingForLargeJobAndHideItFromReads() {
        insertExportableJob();
        List<String> sessions = Collections.singletonList("sess_1");
        when(chatPersistService.countOfflineBatchEvents(sessions)).thenReturn(50_000L);

        service.delete("eval_export_demo");

        // 主表还在（清理进度靠它续跑），但对读接口已经不存在
        OfflineJob marked = mapper.findJob("eval_export_demo").orElseThrow(AssertionError::new);
        assertThat(marked.getStatus()).isEqualTo("DELETING");
        assertThat(service.list(1, 10).getRecords()).isEmpty();
        assertThat(service.list(1, 10).getTotal()).isEqualTo(0);
        assertThatThrownBy(() -> service.get("eval_export_demo"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("离线任务不存在");
        assertThatThrownBy(() -> service.export("eval_export_demo"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("离线任务不存在");
        verify(purger).schedule("eval_export_demo");
        verify(chatPersistService, never()).purgeOfflineBatchPersisted(any(), any(), anyInt());
    }

    @Test
    void shouldResumePurgeForJobLeftInDeleting() {
        insertExportableJob();
        mapper.markJobStatus("eval_export_demo", "DELETING");
        List<String> sessions = Collections.singletonList("sess_1");
        when(chatPersistService.purgeOfflineBatchPersisted("eval_export_demo", sessions, 500))
                .thenReturn(500, 500, 0);

        assertThat(purger.sweep()).isEqualTo(1);

        verify(chatPersistService, times(3))
                .purgeOfflineBatchPersisted("eval_export_demo", sessions, 500);
        assertThat(mapper.findJob("eval_export_demo")).isEmpty();
    }

    @Test
    void shouldKeepJobWhenPurgeFailsMidway() {
        insertExportableJob();
        mapper.markJobStatus("eval_export_demo", "DELETING");
        when(chatPersistService.purgeOfflineBatchPersisted(any(), any(), anyInt()))
                .thenThrow(new IllegalStateException("db down"));

        assertThat(purger.sweep()).isEqualTo(0);

        // 主表与 DELETING 标记都留着，下一轮还能续删
        OfflineJob stillThere = mapper.findJob("eval_export_demo").orElseThrow(AssertionError::new);
        assertThat(stillThere.getStatus()).isEqualTo("DELETING");
    }

    @Test
    void shouldRejectDeleteWhenJobStillRunning() {
        insertExportableJob();
        OfflineJob job = mapper.findJob("eval_export_demo").orElseThrow(AssertionError::new);
        job.setStatus("RUNNING");
        mapper.updateJob(job);

        assertThatThrownBy(() -> service.delete("eval_export_demo"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("执行中");
        assertThat(mapper.findJob("eval_export_demo")).isPresent();
        verify(chatPersistService, never()).purgeOfflineBatchPersisted(any(), any(), anyInt());
    }

    private void insertExportableJob() {
        OfflineJob job = new OfflineJob();
        job.setBizId("eval_export_demo");
        job.setDatasetId(1L);
        job.setAgentId(9L);
        job.setAgentVersionId(90L);
        job.setAgentName("demo");
        job.setStatus("SUCCEEDED");
        job.setTotal(1);
        OfflineJobCell cell = new OfflineJobCell();
        cell.setTaskKey("t1");
        cell.setPrompt("hello");
        cell.setStatus("DONE");
        cell.setAgentkSessionId("sess_1");
        cell.setAgentkRunId("run_1");
        cell.setMetricsJson("{\"steps\":1}");
        cell.setTraceJson("{\"status\":\"completed\"}");
        cell.setError("");
        cell.setStartedAt(LocalDateTime.of(2026, 8, 13, 10, 0, 0));
        cell.setFinishedAt(LocalDateTime.of(2026, 8, 13, 10, 1, 0));
        cell.setOutputUris("[\"https://cdn/a.png\"]");
        job.getCells().add(cell);
        mapper.insertJob(job);
    }

    private void setTraceJson(String traceJson) {
        OfflineJob job = mapper.findJob("eval_export_demo").orElseThrow(AssertionError::new);
        job.getCells().get(0).setTraceJson(traceJson);
        mapper.updateJob(job);
    }

    private static IPage<RunEvent> eventPage(long current, long size, long total, RunEvent... events) {
        Page<RunEvent> page = new Page<>(current, size, total);
        page.setRecords(Arrays.asList(events));
        return page;
    }

    private static RunEvent event(long id, int seq, String kind, String payloadJson) {
        RunEvent event = new RunEvent();
        event.setId(id);
        event.setSeq(seq);
        event.setKind(kind);
        event.setPayloadJson(payloadJson);
        event.setAgentkRunId("run_1");
        event.setAgentkSessionId("sess_1");
        event.setAgentkEventId("evt_" + id);
        event.setAgentkCreatedAt("2026-08-13T10:00:0" + seq + "Z");
        return event;
    }

    private Dataset insertDataset(String usage, String pipelineName) {
        Dataset dataset = new Dataset();
        dataset.setName("ds-" + usage);
        dataset.setVersion("v1");
        dataset.setDatasetType("文本");
        dataset.setUsage(usage);
        dataset.setPipelineName(pipelineName);
        DatasetTask task = new DatasetTask();
        task.setTaskKey("t1");
        task.setPrompt("p");
        dataset.getTasks().add(task);
        return mapper.insertDataset(dataset);
    }

    private static Agent officialAgent(long id, String name) {
        Agent agent = new Agent();
        agent.setId(id);
        agent.setName(name);
        agent.setStatus("active");
        agent.setIsDefault(true);
        return agent;
    }

    private static Agent personalAgent(long id, String name, String owner) {
        Agent agent = new Agent();
        agent.setId(id);
        agent.setName(name);
        agent.setStatus("active");
        agent.setIsDefault(false);
        agent.setOwnerUserId(owner);
        return agent;
    }

    private static AgentVersion publishedVersion(long id, long agentId, int version, String profile) {
        AgentVersion v = new AgentVersion();
        v.setId(id);
        v.setAgentId(agentId);
        v.setVersion(version);
        v.setStatus("published");
        v.setProfileYaml(profile);
        v.setContentHash("abcdefghijklmnop");
        return v;
    }
}
