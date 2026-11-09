package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;

import javax.annotation.PreDestroy;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.batcheval.config.AgentkProperties;
import com.kuaishou.agent.operation.backend.batcheval.mapper.BatchEvalMapper;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;
import com.kuaishou.agent.operation.backend.batcheval.support.DatasetConversationFields;
import com.kuaishou.agent.operation.backend.batcheval.support.OfflineJobSessionMode;

/**
 * 进程内编排器：同一离线任务内的用例按 {@code submit-stagger-ms} 错开提交，
 * 同时在跑的条数由 {@code max-concurrent-cells} 卡住，其余排队等名额。
 * <p>
 * 并发上限是内存与数据库写入的闸门：每条在跑的用例都在轮询事件并逐条落库，
 * 放开跑会让一个 job 的全部用例同时压上来。{@link AgentkClient} 的出站名额限的是
 * 并发 RPC，管不了并发用例，两者都需要。
 */
@Component
public class OfflineJobOrchestrator {

    private static final Logger logger = LoggerFactory.getLogger(OfflineJobOrchestrator.class);

    private final ExecutorService coordinators =
            Executors.newCachedThreadPool(runnable -> new Thread(runnable, "batcheval-coordinator"));
    private final ExecutorService taskPool;
    private final BatchEvalMapper mapper;
    private final SingleTaskRunner taskRunner;
    private final AgentkProperties properties;

    public OfflineJobOrchestrator(BatchEvalMapper mapper, SingleTaskRunner taskRunner,
            AgentkProperties properties) {
        this.mapper = mapper;
        this.taskRunner = taskRunner;
        this.properties = properties;
        this.taskPool = Executors.newFixedThreadPool(properties.getMaxConcurrentCells(),
                runnable -> new Thread(runnable, "batcheval-task"));
    }

    public void start(String bizId) {
        start(bizId, OfflineJobSessionMode.PER_TASK);
    }

    public void start(String bizId, String sessionMode) {
        start(bizId, OfflineJobSessionMode.parse(sessionMode));
    }

    public void start(String bizId, OfflineJobSessionMode sessionMode) {
        OfflineJobSessionMode mode = sessionMode == null ? OfflineJobSessionMode.PER_TASK : sessionMode;
        coordinators.execute(() -> executeJob(bizId, mode));
    }

    private void executeJob(String bizId, OfflineJobSessionMode sessionMode) {
        OfflineJob job;
        try {
            job = mapper.findJob(bizId)
                    .orElseThrow(() -> new IllegalArgumentException("离线任务不存在: " + bizId));
        } catch (RuntimeException e) {
            logger.error("离线任务启动失败: {}", bizId, e);
            return;
        }

        try {
            syncCellsFromDataset(job);
            job.setStatus("RUNNING");
            job.setStartedAt(LocalDateTime.now());
            mapper.updateJob(job);

            List<OfflineJobCell> cells = new ArrayList<>(job.getCells());
            logger.info("离线任务开始调度: {} cells={} staggerMs={} maxConcurrentCells={} sessionMode={}",
                    bizId, cells.size(), submitStaggerMs(), properties.getMaxConcurrentCells(),
                    sessionMode);
            if (sessionMode.isSingleSession()) {
                executeSingleSessionJob(job, cells);
            } else if (sessionMode.isGrouped()) {
                executeGroupedJob(job, cells);
            } else {
                executeConcurrentJob(job, cells);
            }
            finalizeJob(bizId);
        } catch (RuntimeException e) {
            logger.error("离线任务执行异常，强制收口: {}", bizId, e);
            try {
                forceFailJob(bizId, e.getMessage());
            } catch (RuntimeException failError) {
                logger.error("离线任务强制收口失败: {}", bizId, failError);
            }
        }
    }

    private void executeConcurrentJob(OfflineJob job, List<OfflineJobCell> cells) {
        String bizId = job.getBizId();
        List<Future<?>> futures = new ArrayList<>(cells.size());
        for (int i = 0; i < cells.size(); i++) {
            if (i > 0) {
                sleepStagger();
            }
            OfflineJobCell cell = cells.get(i);
            futures.add(taskPool.submit(() -> taskRunner.run(job, cell)));
        }
        awaitCellFutures(bizId, cells, futures);
    }

    /**
     * 整表一个 session：忽略 conversationKey，按 turnIndex（无则行序）串行。
     * 前一轮失败仍继续跑后续轮，共用已有 session（没有则后续轮自己建）。
     */
    private void executeSingleSessionJob(OfflineJob job, List<OfflineJobCell> cells) {
        Set<String> keys = new HashSet<>();
        for (OfflineJobCell cell : cells) {
            if (StringUtils.hasText(cell.getConversationKey())) {
                keys.add(cell.getConversationKey().trim());
            }
        }
        if (keys.size() > 1) {
            logger.info("sessionMode=SINGLE_SESSION，忽略多个 conversationKey: {} keys={}",
                    job.getBizId(), keys.size());
        }
        List<OfflineJobCell> ordered = new ArrayList<>(cells);
        sortByTurn(ordered);
        executeConversationGroup(job, ordered);
    }

    /**
     * 按 conversationKey 分桶：空键各自独立可并发；同键按轮次串行共用 session。
     */
    private void executeGroupedJob(OfflineJob job, List<OfflineJobCell> cells) {
        boolean anyKey = false;
        for (OfflineJobCell cell : cells) {
            if (StringUtils.hasText(cell.getConversationKey())) {
                anyKey = true;
                break;
            }
        }
        if (!anyKey) {
            logger.warn("sessionMode=GROUPED 但数据集没有 conversationKey，按单轮并发执行: {}",
                    job.getBizId());
        }
        List<List<OfflineJobCell>> buckets = bucketByConversation(cells);
        String bizId = job.getBizId();
        List<Future<?>> futures = new ArrayList<>(buckets.size());
        List<OfflineJobCell> firstCells = new ArrayList<>(buckets.size());
        for (int i = 0; i < buckets.size(); i++) {
            if (i > 0) {
                sleepStagger();
            }
            List<OfflineJobCell> group = buckets.get(i);
            firstCells.add(group.get(0));
            futures.add(taskPool.submit(() -> runConversationGroup(job, group)));
        }
        awaitCellFutures(bizId, firstCells, futures);
    }

    private void runConversationGroup(OfflineJob job, List<OfflineJobCell> cells) {
        try {
            executeConversationGroup(job, cells);
        } catch (RuntimeException e) {
            for (OfflineJobCell cell : cells) {
                if (!isTerminal(cell.getStatus())) {
                    cell.setStatus("ERROR");
                    cell.setError(e.getMessage() == null ? e.getClass().getSimpleName() : e.getMessage());
                    cell.setFinishedAt(LocalDateTime.now());
                    try {
                        mapper.updateCell(job.getBizId(), cell);
                    } catch (RuntimeException updateError) {
                        logger.warn("回写异常 cell 失败: {}/{}", job.getBizId(), cell.getTaskKey(), updateError);
                    }
                }
            }
            throw e;
        }
    }

    private void executeConversationGroup(OfflineJob job, List<OfflineJobCell> cells) {
        String sharedSessionId = null;
        for (OfflineJobCell cell : cells) {
            taskRunner.run(job, cell, sharedSessionId);
            if (sharedSessionId == null && StringUtils.hasText(cell.getAgentkSessionId())) {
                sharedSessionId = cell.getAgentkSessionId().trim();
            }
            if (!"DONE".equals(cell.getStatus())) {
                logger.warn("会话组前序轮次未成功，仍继续后续轮: {}/{} status={} error={}",
                        job.getBizId(), cell.getTaskKey(), cell.getStatus(), cell.getError());
            }
        }
    }

    private void awaitCellFutures(String bizId, List<OfflineJobCell> cells, List<Future<?>> futures) {
        for (int i = 0; i < futures.size(); i++) {
            OfflineJobCell cell = cells.get(i);
            try {
                futures.get(i).get();
            } catch (Exception e) {
                if (!isTerminal(cell.getStatus())) {
                    cell.setStatus("ERROR");
                    cell.setError(e.getMessage() == null ? e.getClass().getSimpleName() : e.getMessage());
                    cell.setFinishedAt(LocalDateTime.now());
                    try {
                        mapper.updateCell(bizId, cell);
                    } catch (RuntimeException updateError) {
                        logger.warn("回写异常 cell 失败: {}/{}", bizId, cell.getTaskKey(), updateError);
                    }
                }
            }
        }
    }

    private static List<List<OfflineJobCell>> bucketByConversation(List<OfflineJobCell> cells) {
        Map<String, List<OfflineJobCell>> buckets = new LinkedHashMap<>();
        int singletonSeq = 0;
        for (OfflineJobCell cell : cells) {
            String key;
            if (StringUtils.hasText(cell.getConversationKey())) {
                key = "g:" + cell.getConversationKey().trim();
            } else {
                key = "s:" + singletonSeq++;
            }
            buckets.computeIfAbsent(key, ignored -> new ArrayList<>()).add(cell);
        }
        List<List<OfflineJobCell>> result = new ArrayList<>(buckets.size());
        for (List<OfflineJobCell> group : buckets.values()) {
            sortByTurn(group);
            result.add(group);
        }
        return result;
    }

    private static void sortByTurn(List<OfflineJobCell> cells) {
        cells.sort(Comparator
                .comparing(OfflineJobCell::getTurnIndex, Comparator.nullsLast(Integer::compareTo))
                .thenComparingLong(OfflineJobCell::getId));
    }

    /**
     * 用例已全部终态但 job 仍停在 RUNNING/PENDING 时补写终态（进程中断、收口异常等）。
     *
     * @return true 若本次写库收口
     */
    public boolean reconcileIfNeeded(OfflineJob job) {
        if (job == null || isFinished(job.getStatus())) {
            return false;
        }
        if (!"RUNNING".equals(job.getStatus()) && !"PENDING".equals(job.getStatus())) {
            return false;
        }
        List<OfflineJobCell> cells = job.getCells();
        if (cells == null || cells.isEmpty()) {
            return false;
        }
        for (OfflineJobCell cell : cells) {
            if (!isTerminal(cell.getStatus())) {
                return false;
            }
        }
        applyTerminalStatus(job, cells);
        mapper.updateJob(job);
        logger.info("离线任务状态已对账收口: {} -> {}, errors={}",
                job.getBizId(), job.getStatus(), job.getErrors());
        return true;
    }

    private void finalizeJob(String bizId) {
        OfflineJob latest = mapper.findJob(bizId)
                .orElseThrow(() -> new IllegalArgumentException("离线任务不存在: " + bizId));
        for (OfflineJobCell cell : latest.getCells()) {
            if (!isTerminal(cell.getStatus())) {
                cell.setStatus("ERROR");
                cell.setError(cell.getError() == null || cell.getError().isEmpty()
                        ? "任务未正常结束" : cell.getError());
                if (cell.getFinishedAt() == null) {
                    cell.setFinishedAt(LocalDateTime.now());
                }
                mapper.updateCell(bizId, cell);
            }
        }
        // 再读一次，保证 errors / status 基于落库结果
        latest = mapper.findJob(bizId)
                .orElseThrow(() -> new IllegalArgumentException("离线任务不存在: " + bizId));
        applyTerminalStatus(latest, latest.getCells());
        mapper.updateJob(latest);
    }

    private void forceFailJob(String bizId, String message) {
        OfflineJob job = mapper.findJob(bizId).orElse(null);
        if (job == null || isFinished(job.getStatus())) {
            return;
        }
        for (OfflineJobCell cell : job.getCells()) {
            if (!isTerminal(cell.getStatus())) {
                cell.setStatus("ERROR");
                cell.setError(message == null ? "离线任务执行异常" : message);
                cell.setFinishedAt(LocalDateTime.now());
                mapper.updateCell(bizId, cell);
            }
        }
        job = mapper.findJob(bizId).orElse(job);
        applyTerminalStatus(job, job.getCells());
        if (!"FAILED".equals(job.getStatus()) && !"SUCCEEDED".equals(job.getStatus())) {
            job.setStatus("FAILED");
            job.setFinishedAt(LocalDateTime.now());
        }
        mapper.updateJob(job);
    }

    private static void applyTerminalStatus(OfflineJob job, List<OfflineJobCell> cells) {
        int errors = 0;
        for (OfflineJobCell cell : cells) {
            if (!"DONE".equals(cell.getStatus())) {
                errors++;
            }
        }
        job.setErrors(errors);
        job.setStatus(errors > 0 ? "FAILED" : "SUCCEEDED");
        if (job.getFinishedAt() == null) {
            job.setFinishedAt(LocalDateTime.now());
        }
    }

    private static boolean isTerminal(String status) {
        return "DONE".equals(status) || "ERROR".equals(status);
    }

    private static boolean isFinished(String status) {
        return "SUCCEEDED".equals(status) || "FAILED".equals(status);
    }

    private int submitStaggerMs() {
        return Math.max(0, properties.getSubmitStaggerMs());
    }

    private void sleepStagger() {
        int delayMs = submitStaggerMs();
        if (delayMs <= 0) {
            return;
        }
        try {
            Thread.sleep(delayMs);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("离线任务调度被中断", e);
        }
    }

    /** 执行前补齐数据集新增用例，避免提交后增 task 漏评。 */
    private void syncCellsFromDataset(OfflineJob job) {
        List<DatasetTask> tasks = mapper.listAllTasks(job.getDatasetId());
        Set<String> existingKeys = new HashSet<>();
        for (OfflineJobCell cell : job.getCells()) {
            existingKeys.add(cell.getTaskKey());
        }
        boolean added = false;
        for (DatasetTask task : tasks) {
            if (existingKeys.contains(task.getTaskKey())) {
                continue;
            }
            OfflineJobCell cell = new OfflineJobCell();
            cell.setTaskKey(task.getTaskKey());
            cell.setPrompt(task.getPrompt());
            DatasetConversationFields.copyToCell(task, cell);
            cell.setStatus("QUEUED");
            mapper.insertJobCell(job.getId(), cell);
            job.getCells().add(cell);
            added = true;
        }
        if (added) {
            job.setTotal(job.getCells().size());
        }
    }

    @PreDestroy
    public void shutdown() {
        coordinators.shutdownNow();
        taskPool.shutdownNow();
    }
}
