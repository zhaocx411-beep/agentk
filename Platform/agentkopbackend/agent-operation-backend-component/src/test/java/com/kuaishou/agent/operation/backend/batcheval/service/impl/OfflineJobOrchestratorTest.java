package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;

import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.batcheval.config.AgentkProperties;
import com.kuaishou.agent.operation.backend.batcheval.mapper.memory.InMemoryBatchEvalMapper;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.Dataset;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;
import com.kuaishou.agent.operation.backend.batcheval.support.OfflineJobSessionMode;

class OfflineJobOrchestratorTest {

    private InMemoryBatchEvalMapper mapper;
    private OfflineJobOrchestrator orchestrator;
    private AgentkProperties properties;

    @BeforeEach
    void setUp() {
        mapper = new InMemoryBatchEvalMapper();
        properties = new AgentkProperties();
        properties.setSubmitStaggerMs(0);
        orchestrator = new OfflineJobOrchestrator(mapper, mock(SingleTaskRunner.class), properties);
    }

    @Test
    void shouldReconcileRunningJobWhenAllCellsTerminalWithErrors() {
        Dataset dataset = new Dataset();
        dataset.setName("ds");
        dataset.setVersion("v1");
        dataset.setCreatedAt(LocalDateTime.now());
        DatasetTask task = new DatasetTask();
        task.setTaskKey("1");
        task.setPrompt("p");
        dataset.getTasks().add(task);
        mapper.insertDataset(dataset);

        OfflineJob job = new OfflineJob();
        job.setBizId("eval_stuck");
        job.setDatasetId(dataset.getId());
        job.setModel("m");
        job.setSystemPrompt("s");
        job.setTools(Collections.emptyList());
        job.setProfileVersion("v");
        job.setProfileYaml("{}");
        job.setStatus("RUNNING");
        job.setTotal(1);
        job.setErrors(0);
        job.setCreatedAt(LocalDateTime.now());
        job.setStartedAt(LocalDateTime.now());
        OfflineJobCell cell = new OfflineJobCell();
        cell.setTaskKey("1");
        cell.setPrompt("p");
        cell.setStatus("ERROR");
        cell.setError("boom");
        cell.setFinishedAt(LocalDateTime.now());
        job.getCells().add(cell);
        mapper.insertJob(job);

        assertThat(orchestrator.reconcileIfNeeded(
                mapper.findJob("eval_stuck").orElseThrow(AssertionError::new)))
                .isTrue();
        OfflineJob updated = mapper.findJob("eval_stuck").orElseThrow(AssertionError::new);
        assertThat(updated.getStatus()).isEqualTo("FAILED");
        assertThat(updated.getErrors()).isEqualTo(1);
        assertThat(updated.getFinishedAt()).isNotNull();
    }

    @Test
    void shouldStaggerSubmitsWithinConcurrencyBudget() throws Exception {
        properties.setSubmitStaggerMs(150);
        properties.setMaxConcurrentCells(3);
        SingleTaskRunner runner = mock(SingleTaskRunner.class);
        orchestrator = new OfflineJobOrchestrator(mapper, runner, properties);

        List<Long> startedAt = Collections.synchronizedList(new ArrayList<>());
        CountDownLatch allStarted = new CountDownLatch(3);
        CountDownLatch hold = new CountDownLatch(1);
        doAnswer(invocation -> {
            startedAt.add(System.nanoTime());
            allStarted.countDown();
            if (!hold.await(5, TimeUnit.SECONDS)) {
                throw new IllegalStateException("hold timed out");
            }
            return null;
        }).when(runner).run(any(), any());

        insertQueuedJob("eval_stagger", 3);
        orchestrator.start("eval_stagger");

        assertThat(allStarted.await(3, TimeUnit.SECONDS)).isTrue();
        assertThat(startedAt).hasSize(3);
        for (int i = 1; i < startedAt.size(); i++) {
            long gapMs = TimeUnit.NANOSECONDS.toMillis(startedAt.get(i) - startedAt.get(i - 1));
            assertThat(gapMs).isGreaterThanOrEqualTo(100L);
        }
        hold.countDown();
    }

    @Test
    void shouldQueueCellsBeyondConcurrencyLimit() throws Exception {
        properties.setSubmitStaggerMs(0);
        properties.setMaxConcurrentCells(2);
        SingleTaskRunner runner = mock(SingleTaskRunner.class);
        orchestrator = new OfflineJobOrchestrator(mapper, runner, properties);

        CountDownLatch twoStarted = new CountDownLatch(2);
        CountDownLatch thirdStarted = new CountDownLatch(3);
        CountDownLatch hold = new CountDownLatch(1);
        doAnswer(invocation -> {
            twoStarted.countDown();
            thirdStarted.countDown();
            if (!hold.await(5, TimeUnit.SECONDS)) {
                throw new IllegalStateException("hold timed out");
            }
            return null;
        }).when(runner).run(any(), any());

        insertQueuedJob("eval_capped", 3);
        orchestrator.start("eval_capped");

        assertThat(twoStarted.await(3, TimeUnit.SECONDS)).isTrue();
        // 前两条占满名额后第三条只能排队，直到有名额释放
        assertThat(thirdStarted.await(300, TimeUnit.MILLISECONDS)).isFalse();
        hold.countDown();
        assertThat(thirdStarted.await(3, TimeUnit.SECONDS)).isTrue();
    }

    @Test
    void shouldReuseSingleSessionWhenMultiRoundEnabled() throws Exception {
        SingleTaskRunner runner = mock(SingleTaskRunner.class);
        orchestrator = new OfflineJobOrchestrator(mapper, runner, properties);

        List<String> sharedSessionIds = Collections.synchronizedList(new ArrayList<>());
        CountDownLatch allFinished = new CountDownLatch(3);
        doAnswer(invocation -> {
            OfflineJobCell cell = invocation.getArgument(1);
            String sharedSessionId = invocation.getArgument(2);
            sharedSessionIds.add(sharedSessionId);
            cell.setAgentkSessionId(sharedSessionId == null ? "ses_shared" : sharedSessionId);
            cell.setStatus("DONE");
            allFinished.countDown();
            return null;
        }).when(runner).run(any(), any(), any());

        insertQueuedJob("eval_multi_round", 3);
        orchestrator.start("eval_multi_round", OfflineJobSessionMode.SINGLE_SESSION);

        assertThat(allFinished.await(3, TimeUnit.SECONDS)).isTrue();
        assertThat(sharedSessionIds).containsExactly(null, "ses_shared", "ses_shared");
    }

    @Test
    void shouldShareSessionWithinConversationGroupOnly() throws Exception {
        SingleTaskRunner runner = mock(SingleTaskRunner.class);
        orchestrator = new OfflineJobOrchestrator(mapper, runner, properties);

        Map<String, String> sharedByTask = Collections.synchronizedMap(new java.util.LinkedHashMap<>());
        CountDownLatch allFinished = new CountDownLatch(4);
        doAnswer(invocation -> {
            OfflineJobCell cell = invocation.getArgument(1);
            String sharedSessionId = invocation.getArgument(2);
            sharedByTask.put(cell.getTaskKey(), sharedSessionId);
            cell.setAgentkSessionId(sharedSessionId == null ? "ses_" + cell.getTaskKey() : sharedSessionId);
            cell.setStatus("DONE");
            allFinished.countDown();
            return null;
        }).when(runner).run(any(), any(), any());

        insertQueuedJob("eval_grouped", cell("a2", "conv-a", 2), cell("a1", "conv-a", 1),
                cell("s1", null, null), cell("b1", "conv-b", 1));
        orchestrator.start("eval_grouped", OfflineJobSessionMode.GROUPED);

        assertThat(allFinished.await(3, TimeUnit.SECONDS)).isTrue();
        assertThat(sharedByTask.get("a1")).isNull();
        assertThat(sharedByTask.get("a2")).isEqualTo("ses_a1");
        assertThat(sharedByTask.get("s1")).isNull();
        assertThat(sharedByTask.get("b1")).isNull();
    }

    @Test
    void shouldContinueLaterTurnsWhenPriorTurnFails() throws Exception {
        SingleTaskRunner runner = mock(SingleTaskRunner.class);
        orchestrator = new OfflineJobOrchestrator(mapper, runner, properties);

        CountDownLatch allFinished = new CountDownLatch(2);
        doAnswer(invocation -> {
            OfflineJobCell cell = invocation.getArgument(1);
            String sharedSessionId = invocation.getArgument(2);
            if ("t1".equals(cell.getTaskKey())) {
                cell.setAgentkSessionId("ses_shared");
                cell.setStatus("ERROR");
                cell.setError("boom");
            } else {
                assertThat(sharedSessionId).isEqualTo("ses_shared");
                cell.setAgentkSessionId(sharedSessionId);
                cell.setStatus("DONE");
            }
            cell.setFinishedAt(LocalDateTime.now());
            allFinished.countDown();
            return null;
        }).when(runner).run(any(), any(), any());

        insertQueuedJob("eval_continue_group", cell("t1", "conv-a", 1), cell("t2", "conv-a", 2));
        orchestrator.start("eval_continue_group", OfflineJobSessionMode.SINGLE_SESSION);

        assertThat(allFinished.await(3, TimeUnit.SECONDS)).isTrue();
        OfflineJob job = waitJobFinished("eval_continue_group");
        assertThat(job.getCells().get(0).getStatus()).isEqualTo("ERROR");
        assertThat(job.getCells().get(0).getError()).isEqualTo("boom");
        assertThat(job.getCells().get(1).getStatus()).isEqualTo("DONE");
        assertThat(job.getCells().get(1).getAgentkSessionId()).isEqualTo("ses_shared");
        verify(runner, times(2)).run(any(), any(), any());
    }

    private void insertQueuedJob(String bizId, int cellCount) {
        Dataset dataset = new Dataset();
        dataset.setName(bizId);
        dataset.setVersion("v1");
        dataset.setCreatedAt(LocalDateTime.now());
        OfflineJob job = new OfflineJob();
        job.setBizId(bizId);
        job.setModel("m");
        job.setSystemPrompt("s");
        job.setTools(Collections.emptyList());
        job.setProfileVersion("v");
        job.setProfileYaml("{}");
        job.setStatus("PENDING");
        job.setTotal(cellCount);
        job.setErrors(0);
        job.setCreatedAt(LocalDateTime.now());
        for (int i = 1; i <= cellCount; i++) {
            DatasetTask task = new DatasetTask();
            task.setTaskKey(String.valueOf(i));
            task.setPrompt("p" + i);
            dataset.getTasks().add(task);
            OfflineJobCell cell = new OfflineJobCell();
            cell.setTaskKey(String.valueOf(i));
            cell.setPrompt("p" + i);
            cell.setStatus("QUEUED");
            job.getCells().add(cell);
        }
        mapper.insertDataset(dataset);
        job.setDatasetId(dataset.getId());
        mapper.insertJob(job);
    }

    private void insertQueuedJob(String bizId, OfflineJobCell... cells) {
        Dataset dataset = new Dataset();
        dataset.setName(bizId);
        dataset.setVersion("v1");
        dataset.setCreatedAt(LocalDateTime.now());
        OfflineJob job = new OfflineJob();
        job.setBizId(bizId);
        job.setModel("m");
        job.setSystemPrompt("s");
        job.setTools(Collections.emptyList());
        job.setProfileVersion("v");
        job.setProfileYaml("{}");
        job.setStatus("PENDING");
        job.setTotal(cells.length);
        job.setErrors(0);
        job.setCreatedAt(LocalDateTime.now());
        for (OfflineJobCell cell : cells) {
            DatasetTask task = new DatasetTask();
            task.setTaskKey(cell.getTaskKey());
            task.setPrompt(cell.getPrompt());
            task.setConversationKey(cell.getConversationKey());
            task.setTurnIndex(cell.getTurnIndex());
            dataset.getTasks().add(task);
            job.getCells().add(cell);
        }
        mapper.insertDataset(dataset);
        job.setDatasetId(dataset.getId());
        mapper.insertJob(job);
    }

    private static OfflineJobCell cell(String taskKey, String conversationKey, Integer turnIndex) {
        OfflineJobCell cell = new OfflineJobCell();
        cell.setTaskKey(taskKey);
        cell.setPrompt("p-" + taskKey);
        cell.setConversationKey(conversationKey);
        cell.setTurnIndex(turnIndex);
        cell.setStatus("QUEUED");
        return cell;
    }

    private OfflineJob waitJobFinished(String bizId) throws InterruptedException {
        for (int i = 0; i < 50; i++) {
            OfflineJob job = mapper.findJob(bizId).orElseThrow(AssertionError::new);
            if ("SUCCEEDED".equals(job.getStatus()) || "FAILED".equals(job.getStatus())) {
                return job;
            }
            Thread.sleep(50);
        }
        throw new AssertionError("job did not finish: " + bizId);
    }
}
