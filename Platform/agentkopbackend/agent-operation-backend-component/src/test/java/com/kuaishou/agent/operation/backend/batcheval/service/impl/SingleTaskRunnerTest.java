package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.argThat;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.ArgumentMatchers.nullable;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.kuaishou.agent.operation.backend.agentk.tools.AgentkPipelineInfo;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkClient;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkClient.PipelineRunResult;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkClient.RunResult;
import com.kuaishou.agent.operation.backend.batcheval.mapper.memory.InMemoryBatchEvalMapper;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.Dataset;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;
import com.kuaishou.agent.operation.backend.batcheval.support.LoopUserMessage;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.CreateSessionBegin;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;

class SingleTaskRunnerTest {

    private final ObjectMapper objectMapper = new ObjectMapper();
    private InMemoryBatchEvalMapper mapper;
    private AgentkClient agentkClient;
    private ProfileCompiler profileCompiler;
    private ChatPersistService chatPersistService;
    private SingleTaskRunner runner;
    private OfflineJob job;
    private OfflineJobCell cell;
    private DatasetTask task;

    @BeforeEach
    void setUp() {
        mapper = new InMemoryBatchEvalMapper();
        agentkClient = mock(AgentkClient.class);
        profileCompiler = mock(ProfileCompiler.class);
        chatPersistService = mock(ChatPersistService.class);
        when(chatPersistService.findByAgentkSessionId(anyString()))
                .thenReturn(java.util.Optional.empty());
        CreateSessionBegin begin = new CreateSessionBegin();
        begin.setSession(new ChatSession());
        when(chatPersistService.beginCreateSession(anyString(), nullable(String.class), anyString(),
                nullable(Long.class), nullable(Long.class), nullable(String.class), anyString(), any()))
                .thenReturn(begin);

        runner = new SingleTaskRunner(agentkClient, mapper, objectMapper, new PromptAssembler(),
                new PipelineArgsResolver(), profileCompiler,
                new OfflineArtifactRecorder(chatPersistService, objectMapper),
                new OfflineLoopEventSink(chatPersistService));

        Dataset dataset = new Dataset();
        dataset.setName("ds");
        dataset.setVersion("v1");
        dataset.setUsage("EVAL");
        task = new DatasetTask();
        task.setTaskKey("t1");
        task.setPrompt("做一个广告");
        task.setReferImages(Collections.singletonList("https://img/a.jpg"));
        Map<String, Object> extra = new LinkedHashMap<>();
        extra.put("商品标题", "YSL");
        task.setExtraFields(extra);
        dataset.getTasks().add(task);
        mapper.insertDataset(dataset);

        job = new OfflineJob();
        job.setBizId("eval_1");
        job.setDatasetId(dataset.getId());
        job.setModel("m");
        job.setSystemPrompt("s");
        job.setProfileYaml("{\"defaultModel\":\"m\",\"system\":\"s\",\"tools\":[]}");
        job.setProfileVersion("abc");
        job.setCreator("alice");
        job.setStatus("RUNNING");
        cell = new OfflineJobCell();
        cell.setTaskKey("t1");
        cell.setPrompt("做一个广告");
        cell.setStatus("QUEUED");
        job.getCells().add(cell);
        mapper.insertJob(job);
    }

    @Test
    void shouldRunLoopOnlyWhenToolsSelected() {
        job.setTools(Collections.singletonList("echo"));
        when(agentkClient.createSession(anyString(), anyString())).thenReturn("ses_1");
        when(agentkClient.runLoop(eq("ses_1"), any(LoopUserMessage.class), any())).thenReturn(loopResult("ses_1", true));

        runner.run(job, cell);

        assertThat(cell.getStatus()).isEqualTo("DONE");
        assertThat(cell.getAgentkSessionId()).isEqualTo("ses_1");
        assertThat(cell.getAgentkRunId()).isEqualTo("exe_1");
        verify(agentkClient).runLoop(eq("ses_1"), any(LoopUserMessage.class), any());
        verify(agentkClient, never()).runPipeline(anyString(), anyString(), any());
    }

    @Test
    void geminiModelSendsReferImagesAsAttachments() {
        job.setTools(Collections.singletonList("echo"));
        job.setModel("gemini-3.1-flash-lite");
        job.setProfileYaml("{\"defaultModel\":\"gemini-3.1-flash-lite\",\"system\":\"s\",\"tools\":[]}");
        Map<String, Object> extra = new LinkedHashMap<>();
        extra.put("audioUrl", "https://cdn/a.mp3");
        extra.put("videoUrl", "https://cdn/v.mp4");
        task.setExtraFields(extra);
        when(agentkClient.createSession(anyString(), anyString())).thenReturn("ses_gemini");
        when(agentkClient.runLoop(eq("ses_gemini"), any(LoopUserMessage.class), any()))
                .thenReturn(loopResult("ses_gemini", true));

        runner.run(job, cell);

        verify(agentkClient).runLoop(eq("ses_gemini"), argThat(message ->
                "做一个广告".equals(message.getText())
                        && message.getAttachments().size() == 3
                        && "image".equals(message.getAttachments().get(0).getType())
                        && "https://img/a.jpg".equals(message.getAttachments().get(0).getUri())
                        && "video".equals(message.getAttachments().get(1).getType())
                        && "audio".equals(message.getAttachments().get(2).getType())
        ), any());
    }

    @Test
    void shouldReuseSharedSessionWhenProvided() {
        job.setTools(Collections.singletonList("echo"));
        when(agentkClient.runLoop(eq("ses_shared"), any(LoopUserMessage.class), any()))
                .thenReturn(loopResult("ses_shared", true));

        runner.run(job, cell, "ses_shared");

        assertThat(cell.getStatus()).isEqualTo("DONE");
        assertThat(cell.getAgentkSessionId()).isEqualTo("ses_shared");
        verify(agentkClient, never()).createSession(anyString(), anyString());
        verify(agentkClient).runLoop(eq("ses_shared"), any(LoopUserMessage.class), any());
    }

    @Test
    void shouldRunPipelineOnlyWhenPipelineSelected() {
        job.setTools(Collections.emptyList());
        job.setPipelineName("generate_brand_ad");
        when(profileCompiler.findPipeline("generate_brand_ad")).thenReturn(brandAdPipeline());
        when(agentkClient.createSession(anyString(), anyString())).thenReturn("ses_2");
        when(agentkClient.runPipeline(eq("ses_2"), eq("generate_brand_ad"), any()))
                .thenReturn(pipelineResult("pex_1", true));

        runner.run(job, cell);

        assertThat(cell.getStatus()).isEqualTo("DONE");
        assertThat(cell.getPipelineExecId()).isEqualTo("pex_1");
        verify(agentkClient, never()).runLoop(anyString(), any(LoopUserMessage.class), any());
        verify(agentkClient).runPipeline(eq("ses_2"), eq("generate_brand_ad"), any());
    }

    @Test
    void shouldFailWhenEitherChannelFails() {
        job.setTools(Collections.singletonList("echo"));
        job.setPipelineName("generate_brand_ad");
        when(profileCompiler.findPipeline("generate_brand_ad")).thenReturn(brandAdPipeline());
        when(agentkClient.createSession(anyString(), anyString())).thenReturn("ses_3");
        when(agentkClient.runLoop(eq("ses_3"), any(LoopUserMessage.class), any())).thenReturn(loopResult("ses_3", false));
        when(agentkClient.runPipeline(eq("ses_3"), eq("generate_brand_ad"), any()))
                .thenReturn(pipelineResult("pex_2", true));

        runner.run(job, cell);

        assertThat(cell.getStatus()).isEqualTo("ERROR");
        assertThat(cell.getError()).contains("loop boom");
        assertThat(cell.getPipelineExecId()).isEqualTo("pex_2");
    }

    @Test
    void shouldNotNpeWhenPipelineClientReturnsNullResult() {
        job.setTools(Collections.emptyList());
        job.setPipelineName("generate_brand_ad");
        when(profileCompiler.findPipeline("generate_brand_ad")).thenReturn(brandAdPipeline());
        when(agentkClient.createSession(anyString(), anyString())).thenReturn("ses_null_pipe");
        when(agentkClient.runPipeline(eq("ses_null_pipe"), eq("generate_brand_ad"), any()))
                .thenReturn(null);

        runner.run(job, cell);

        assertThat(cell.getStatus()).isEqualTo("DONE");
        assertThat(cell.getError()).isNull();
        assertThat(cell.getPipelineExecId()).isNull();
    }

    @Test
    void shouldPersistStreamedEventsAsOfflineBatchAndCollectArtifactsOnce() {
        job.setTools(Collections.singletonList("echo"));
        job.setAgentName("漫剧");
        when(agentkClient.createSession(anyString(), anyString())).thenReturn("ses_4");
        when(agentkClient.runLoop(eq("ses_4"), any(LoopUserMessage.class), any())).thenAnswer(invocation -> {
            AgentkClient.EventSink sink = invocation.getArgument(2);
            // agent-k 每轮都从头返回整页，重复投递是常态
            sink.accept(jobCompletedPage());
            sink.accept(jobCompletedPage());
            return loopResult("ses_4", true);
        });

        runner.run(job, cell);

        assertThat(cell.getStatus()).isEqualTo("DONE");
        // 事件按整页重复交出，落库次数跟着投递次数走，去重交给写入侧的 upsert
        verify(chatPersistService, times(2)).persistRunEvent(eq("ses_4"), eq("exe_1"), eq(8),
                eq("job.completed"), any(), eq("2026-08-21T10:00:00Z"), eq("rnd_1"), eq("evt_1"),
                eq("exe_1"), eq("offline_batch"));
        // 产物按 URL 去重，只认一份
        assertThat(cell.getOutputUris()).isEqualTo("[\"https://cdn/out.mp4\"]");
    }

    @Test
    void mergeErrorsShouldKeepBothSides() {
        assertThat(SingleTaskRunner.mergeErrors("a", "b")).isEqualTo("loop: a; pipeline: b");
        assertThat(SingleTaskRunner.mergeErrors("a", null)).isEqualTo("a");
        assertThat(SingleTaskRunner.mergeErrors(null, "b")).isEqualTo("b");
        assertThat(SingleTaskRunner.mergeErrors(null, null)).isNull();
    }

    private RunResult loopResult(String sessionId, boolean ok) {
        ObjectNode summary = objectMapper.createObjectNode();
        summary.put("sessionId", sessionId);
        summary.put("status", ok ? "completed" : "failed");
        if (!ok) {
            summary.put("error", "loop boom");
        }
        return newRunResult(sessionId, "exe_1", "rnd_1", summary);
    }

    private ArrayNode jobCompletedPage() {
        ArrayNode events = objectMapper.createArrayNode();
        ObjectNode event = events.addObject();
        event.put("eventId", "evt_1")
                .put("seq", 8)
                .put("kind", "job.completed")
                .put("roundId", "rnd_1")
                .put("executionId", "exe_1")
                .put("createdAt", "2026-08-21T10:00:00Z");
        event.putObject("data")
                .put("jobId", "job_1")
                .putArray("artifacts")
                .addObject().put("type", "video").put("uri", "https://cdn/out.mp4");
        return events;
    }

    private PipelineRunResult pipelineResult(String execId, boolean ok) {
        ObjectNode snapshot = objectMapper.createObjectNode();
        snapshot.put("id", execId);
        snapshot.put("status", ok ? "completed" : "failed");
        snapshot.putObject("output").putArray("artifacts")
                .addObject().put("type", "video").put("uri", "https://cdn/out.mp4");
        if (!ok) {
            snapshot.put("error", "pipe boom");
        }
        return newPipelineRunResult(execId, "generate_brand_ad",
                ok ? "completed" : "failed", snapshot, ok ? null : "pipe boom");
    }

    private static RunResult newRunResult(String sessionId, String executionId, String lastRoundId,
            JsonNode trace) {
        try {
            java.lang.reflect.Constructor<RunResult> ctor = RunResult.class.getDeclaredConstructor(
                    String.class, String.class, String.class, JsonNode.class);
            ctor.setAccessible(true);
            return ctor.newInstance(sessionId, executionId, lastRoundId, trace);
        } catch (ReflectiveOperationException e) {
            throw new IllegalStateException(e);
        }
    }

    private static PipelineRunResult newPipelineRunResult(String execId, String pipeline, String status,
            JsonNode snapshot, String error) {
        try {
            java.lang.reflect.Constructor<PipelineRunResult> ctor =
                    PipelineRunResult.class.getDeclaredConstructor(
                            String.class, String.class, String.class, JsonNode.class, String.class);
            ctor.setAccessible(true);
            return ctor.newInstance(execId, pipeline, status, snapshot, error);
        } catch (ReflectiveOperationException e) {
            throw new IllegalStateException(e);
        }
    }

    private static AgentkPipelineInfo brandAdPipeline() {
        AgentkPipelineInfo info = new AgentkPipelineInfo();
        info.setName("generate_brand_ad");
        Map<String, Object> schema = new LinkedHashMap<>();
        schema.put("required", java.util.Arrays.asList("title", "images", "video_prompt"));
        Map<String, Object> properties = new LinkedHashMap<>();
        properties.put("title", Collections.singletonMap("type", "string"));
        properties.put("images", Collections.singletonMap("type", "array"));
        properties.put("video_prompt", Collections.singletonMap("type", "string"));
        schema.put("properties", properties);
        info.setInputSchema(schema);
        return info;
    }
}
