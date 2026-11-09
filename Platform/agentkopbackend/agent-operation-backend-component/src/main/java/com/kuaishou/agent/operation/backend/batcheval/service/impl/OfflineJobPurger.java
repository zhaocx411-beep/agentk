package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

import javax.annotation.PostConstruct;
import javax.annotation.PreDestroy;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.batcheval.config.AgentkProperties;
import com.kuaishou.agent.operation.backend.batcheval.mapper.BatchEvalMapper;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;
import com.kuaishou.agent.operation.backend.batcheval.support.OfflineJobStatuses;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;

/**
 * 标记为 {@code DELETING} 的离线任务的分批清理器。
 * <p>
 * 顺序固定为子表 → 主表：事件（{@code aop_session_event}）→ 产物 / 调用日志 → 会话 →
 * cell → 主表。主表行是「还没清完」的唯一标记，所以任何一步中断后，下一轮扫到同一个
 * {@code DELETING} 任务都能原地续跑；主表删掉才算真正结束。
 * <p>
 * 每批一个独立小事务（见 {@link ChatPersistService#purgeOfflineBatchPersisted}），
 * 不用一个大事务删几万行大 JSON——那会长事务持锁、binlog 膨胀，还容易把删除接口拖到超时。
 */
@Component
public class OfflineJobPurger {

    private static final Logger logger = LoggerFactory.getLogger(OfflineJobPurger.class);

    /** 一次清理最多循环多少批，防御删除条件写错时的死循环。 */
    private static final int MAX_BATCHES_PER_JOB = 10000;
    private static final int SWEEP_JOB_LIMIT = 20;

    private final BatchEvalMapper mapper;
    private final ChatPersistService chatPersistService;
    private final AgentkProperties properties;
    private final ScheduledExecutorService workers = Executors.newSingleThreadScheduledExecutor(
            runnable -> {
                Thread thread = new Thread(runnable, "batcheval-purger");
                thread.setDaemon(true);
                return thread;
            });

    public OfflineJobPurger(BatchEvalMapper mapper, ChatPersistService chatPersistService,
            AgentkProperties properties) {
        this.mapper = mapper;
        this.chatPersistService = chatPersistService;
        this.properties = properties;
    }

    /**
     * 启动即接管上次没清完的任务（进程重启、发布中断都靠它兜底），之后按固定间隔扫。
     */
    @PostConstruct
    public void startSweeping() {
        long intervalSeconds = Math.max(10, properties.getPurgeSweepIntervalSeconds());
        workers.scheduleWithFixedDelay(this::sweepQuietly, 5, intervalSeconds, TimeUnit.SECONDS);
    }

    /** 提交一次后台清理；调用方（删除接口）不等它完成。 */
    public void schedule(String bizId) {
        workers.execute(() -> {
            try {
                purge(bizId);
            } catch (RuntimeException e) {
                // 清不掉不算删除失败：任务已是 DELETING，下一轮 sweep 会重来
                logger.warn("离线任务后台清理失败，等待下轮重试: {}", bizId, e);
            }
        });
    }

    /** 扫一轮 {@code DELETING} 任务，逐个清理。 */
    public int sweep() {
        List<String> pending = mapper.listJobBizIdsByStatus(OfflineJobStatuses.DELETING, SWEEP_JOB_LIMIT);
        int purged = 0;
        for (String bizId : pending) {
            try {
                purge(bizId);
                purged++;
            } catch (RuntimeException e) {
                logger.warn("离线任务清理失败，保留 DELETING 待下轮: {}", bizId, e);
            }
        }
        return purged;
    }

    /**
     * 清完一个任务：分批删子表，最后删 cell 与主表。
     * <p>
     * 幂等：主表已经没了直接返回，重复调用不会报错。
     */
    public void purge(String bizId) {
        OfflineJob job = mapper.findJob(bizId).orElse(null);
        if (job == null) {
            return;
        }
        if (!OfflineJobStatuses.isDeleting(job.getStatus())) {
            throw new IllegalStateException("离线任务未标记删除，拒绝清理: " + bizId);
        }
        List<String> sessionIds = agentkSessionIds(job);
        int batchSize = properties.getPurgeBatchSize();
        long deletedRows = 0;
        int batches = 0;
        while (batches++ < MAX_BATCHES_PER_JOB) {
            int deleted = chatPersistService.purgeOfflineBatchPersisted(bizId, sessionIds, batchSize);
            if (deleted <= 0) {
                break;
            }
            deletedRows += deleted;
        }
        if (batches >= MAX_BATCHES_PER_JOB) {
            // 没删干净就不能删主表，否则会话/事件成孤儿；留着 DELETING 下轮继续
            logger.error("离线任务清理批次超上限，暂缓删主表: {} 已删={}", bizId, deletedRows);
            return;
        }
        mapper.deleteJob(bizId);
        logger.info("离线任务已清理完成: {} 关联行数={} 批次={}", bizId, deletedRows, batches - 1);
    }

    /** 事件数低于阈值时删除接口可以同步清完，用户体感与改造前一致。 */
    public boolean canPurgeInline(List<String> agentkSessionIds) {
        long events = chatPersistService.countOfflineBatchEvents(agentkSessionIds);
        return events <= properties.getInlinePurgeEventThreshold();
    }

    static List<String> agentkSessionIds(OfflineJob job) {
        List<String> ids = new ArrayList<>();
        if (job == null || job.getCells() == null) {
            return ids;
        }
        for (OfflineJobCell cell : job.getCells()) {
            if (cell != null && StringUtils.hasText(cell.getAgentkSessionId())) {
                ids.add(cell.getAgentkSessionId().trim());
            }
        }
        return ids;
    }

    private void sweepQuietly() {
        try {
            sweep();
        } catch (RuntimeException e) {
            logger.warn("离线任务清理巡检异常", e);
        }
    }

    @PreDestroy
    public void shutdown() {
        workers.shutdownNow();
    }
}
