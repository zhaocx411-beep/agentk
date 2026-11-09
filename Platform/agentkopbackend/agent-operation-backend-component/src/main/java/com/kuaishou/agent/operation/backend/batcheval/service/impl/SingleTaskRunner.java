package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import java.time.Duration;
import java.time.LocalDateTime;
import java.time.OffsetDateTime;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;

import javax.annotation.PreDestroy;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkChatModelCatalog;
import com.kuaishou.agent.operation.backend.agentk.tools.AgentkPipelineInfo;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkClient;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkClient.PipelineRunResult;
import com.kuaishou.agent.operation.backend.batcheval.config.AgentkClient.RunResult;
import com.kuaishou.agent.operation.backend.batcheval.support.LoopUserMessage;
import com.kuaishou.agent.operation.backend.batcheval.support.LoopUserMessage.Attachment;
import com.kuaishou.agent.operation.backend.batcheval.mapper.BatchEvalMapper;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;
import com.kuaishou.agent.operation.backend.batcheval.service.impl.OfflineLoopEventSink.CellStream;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.SyncedAsset;

/**
 * 执行单条离线用例。
 * <p>
 * 会话建好后，勾了 tools 就发 messages 跑 Agent Loop，勾了 pipeline 就发 pipeline-runs，
 * 两者都勾时在同一 session 上并行跑——这与在线侧的编排一致，agent-k 本就允许一个会话上
 * 两条通道并存。两条通道都终态后才结算 cell：任一失败即 ERROR，trace 里各存一份便于定位。
 */
@Component
public class SingleTaskRunner {

    private static final Logger logger = LoggerFactory.getLogger(SingleTaskRunner.class);

    private final AgentkClient agentkClient;
    private final BatchEvalMapper mapper;
    private final ObjectMapper objectMapper;
    private final PromptAssembler promptAssembler;
    private final PipelineArgsResolver pipelineArgsResolver;
    private final ProfileCompiler profileCompiler;
    private final OfflineArtifactRecorder artifactRecorder;
    private final OfflineLoopEventSink loopEventSink;
    private final ExecutorService channelPool =
            Executors.newCachedThreadPool(runnable -> new Thread(runnable, "batcheval-channel"));

    public SingleTaskRunner(AgentkClient agentkClient, BatchEvalMapper mapper, ObjectMapper objectMapper,
            PromptAssembler promptAssembler, PipelineArgsResolver pipelineArgsResolver,
            ProfileCompiler profileCompiler, OfflineArtifactRecorder artifactRecorder,
            OfflineLoopEventSink loopEventSink) {
        this.agentkClient = agentkClient;
        this.mapper = mapper;
        this.objectMapper = objectMapper;
        this.promptAssembler = promptAssembler;
        this.pipelineArgsResolver = pipelineArgsResolver;
        this.profileCompiler = profileCompiler;
        this.artifactRecorder = artifactRecorder;
        this.loopEventSink = loopEventSink;
    }

    public void run(OfflineJob job, OfflineJobCell cell) {
        run(job, cell, null);
    }

    public void run(OfflineJob job, OfflineJobCell cell, String sharedSessionId) {
        cell.setStatus("RUNNING");
        cell.setStartedAt(LocalDateTime.now());
        mapper.updateCell(job.getBizId(), cell);
        try {
            DatasetTask task = findDatasetTask(job.getDatasetId(), cell.getTaskKey());
            boolean runLoop = job.getTools() != null && !job.getTools().isEmpty();
            boolean runPipeline = StringUtils.hasText(job.getPipelineName());
            if (!runLoop && !runPipeline) {
                throw new IllegalStateException("任务既未勾选 tools 也未选择 pipeline");
            }
            // 入参在建会话前解析：缺必填时直接失败，不留空跑的会话。
            JsonNode pipelineArgs = runPipeline ? resolvePipelineArgs(job, task) : null;

            String sessionId = resolveSession(job, cell, sharedSessionId);
            cell.setAgentkSessionId(sessionId);
            mapper.updateCell(job.getBizId(), cell);
            // 必须先于发消息：事件与产物都以平台会话为锚，登记不上就等于这条用例白跑。
            if (!artifactRecorder.registerSession(job, cell, sessionId)) {
                throw new IllegalStateException("平台会话登记失败，事件与产物都无法落库: " + sessionId);
            }

            CellStream loopStream = null;
            Future<RunResult> loopFuture = null;
            if (runLoop) {
                String defaultModel = resolveDefaultModel(job);
                LoopUserMessage message = promptAssembler.assembleMessage(
                        cell.getPrompt(),
                        task == null ? Collections.emptyList() : task.getReferImages(),
                        task == null ? Collections.emptyMap() : task.getExtraFields(),
                        job.getTools(),
                        defaultModel);
                logger.info(
                        "离线 Loop 发消息: job={} task={} agentId={} agent={} defaultModel={} "
                                + "attachmentMode={} useAttachments={} attachments={} session={}",
                        job.getBizId(), cell.getTaskKey(), job.getAgentId(), job.getAgentName(),
                        blankToDash(defaultModel),
                        AgentkChatModelCatalog.attachmentsOf(defaultModel).getMode(),
                        message.hasAttachments(), summarizeAttachments(message), sessionId);
                CellStream stream = loopEventSink.open(sessionId, cell.getPrompt(), job.getAgentName());
                loopStream = stream;
                loopFuture = channelPool.submit(() -> agentkClient.runLoop(sessionId, message, stream));
            }
            Future<PipelineRunResult> pipelineFuture = null;
            if (runPipeline) {
                pipelineFuture = channelPool.submit(
                        () -> agentkClient.runPipeline(sessionId, job.getPipelineName(), pipelineArgs));
            }

            ChannelOutcome loop = await(loopFuture);
            ChannelOutcome pipeline = await(pipelineFuture);
            applyOutcome(job, cell, sessionId, loop, pipeline, loopStream);
        } catch (Exception e) {
            cell.setStatus("ERROR");
            cell.setError(e.getMessage() == null ? e.getClass().getSimpleName() : e.getMessage());
        } finally {
            cell.setFinishedAt(LocalDateTime.now());
            mapper.updateCell(job.getBizId(), cell);
        }
    }

    private String resolveSession(OfflineJob job, OfflineJobCell cell, String sharedSessionId) {
        if (StringUtils.hasText(sharedSessionId)) {
            return sharedSessionId.trim();
        }
        return agentkClient.createSession(job.getBizId() + "/" + cell.getTaskKey(), job.getProfileYaml());
    }

    private JsonNode resolvePipelineArgs(OfflineJob job, DatasetTask task) {
        AgentkPipelineInfo pipeline = profileCompiler.findPipeline(job.getPipelineName());
        return objectMapper.valueToTree(pipelineArgsResolver.resolve(task, pipeline));
    }

    /** 通道未启用时返回 null；启用时把执行异常收敛成失败结果，避免一条通道炸掉另一条的结算。 */
    private static <T> ChannelOutcome await(Future<T> future) {
        if (future == null) {
            return null;
        }
        try {
            return ChannelOutcome.of(future.get());
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            return ChannelOutcome.failed("等待 agent-k 时任务被中断");
        } catch (ExecutionException e) {
            Throwable cause = e.getCause() == null ? e : e.getCause();
            return ChannelOutcome.failed(cause.getMessage() == null
                    ? cause.getClass().getSimpleName() : cause.getMessage());
        }
    }

    private void applyOutcome(OfflineJob job, OfflineJobCell cell, String sessionId,
            ChannelOutcome loop, ChannelOutcome pipeline, CellStream loopStream)
            throws JsonProcessingException {
        RunResult loopResult = loop == null ? null : (RunResult) loop.value;
        PipelineRunResult pipelineResult = pipeline == null ? null : (PipelineRunResult) pipeline.value;
        JsonNode loopSummary = loopResult == null ? null : loopResult.getSummary();

        if (loopResult != null) {
            cell.setAgentkRunId(loopResult.getExecutionId());
        }
        if (pipelineResult != null) {
            cell.setPipelineExecId(pipelineResult.getExecId());
        }

        String loopError = loop == null ? null
                : loop.error != null ? loop.error
                        : loopSummary == null ? null
                                : "completed".equals(loopSummary.path("status").asText())
                                        ? null : failureMessage(loopSummary);
        String pipelineError = pipeline == null ? null
                : pipeline.error != null ? pipeline.error
                        : pipelineResult == null ? null
                                : pipelineResult.isSucceeded() ? null : pipelineResult.getError();

        String error = mergeErrors(loopError, pipelineError);
        cell.setTraceJson(objectMapper.writeValueAsString(
                mergeTrace(sessionId, loopSummary, pipelineResult, loopError, pipelineError, error)));
        cell.setMetricsJson(objectMapper.writeValueAsString(metrics(
                loopSummary == null ? objectMapper.createObjectNode() : loopSummary)));

        List<SyncedAsset> loopAssets = loopStream == null
                ? Collections.<SyncedAsset>emptyList() : loopStream.assets();
        List<String> outputUris = artifactRecorder.record(job, cell, sessionId, loopAssets, pipelineResult);
        cell.setOutputUris(objectMapper.writeValueAsString(outputUris));

        if (error == null) {
            cell.setStatus("DONE");
        } else {
            cell.setStatus("ERROR");
            cell.setError(error);
        }
    }

    /** 双通道都失败时保留两边原因，避免只看 Loop 漏掉 pipeline 报错。 */
    static String mergeErrors(String loopError, String pipelineError) {
        if (loopError == null || loopError.trim().isEmpty()) {
            return blankToNull(pipelineError);
        }
        if (pipelineError == null || pipelineError.trim().isEmpty()) {
            return loopError.trim();
        }
        if (loopError.trim().equals(pipelineError.trim())) {
            return loopError.trim();
        }
        return "loop: " + loopError.trim() + "; pipeline: " + pipelineError.trim();
    }

    private static String blankToNull(String value) {
        return value == null || value.trim().isEmpty() ? null : value.trim();
    }

    /**
     * 双通道结算快照合并：Loop 摘要在顶层，pipeline 快照挂在 {@code pipeline} 节点下。
     * <p>
     * 事件本体已经逐条落到 {@code aop_session_event}，这里只存摘要，
     * trace 详情由读侧按会话去事件表拼装。
     */
    private ObjectNode mergeTrace(String sessionId, JsonNode loopSummary, PipelineRunResult pipelineResult,
            String loopError, String pipelineError, String mergedError) {
        ObjectNode merged = loopSummary != null && loopSummary.isObject()
                ? ((ObjectNode) loopSummary).deepCopy() : objectMapper.createObjectNode();
        merged.put("sessionId", sessionId);
        if (pipelineResult != null) {
            ObjectNode pipelineNode = objectMapper.createObjectNode();
            pipelineNode.put("execId", pipelineResult.getExecId());
            pipelineNode.put("pipeline", pipelineResult.getPipeline());
            pipelineNode.put("status", pipelineResult.getStatus());
            pipelineNode.set("snapshot", pipelineResult.getSnapshot());
            merged.set("pipeline", pipelineNode);
        }
        if (pipelineError != null) {
            merged.put("pipelineError", pipelineError);
        }
        if (loopError != null) {
            merged.put("loopError", loopError);
        }
        merged.put("status", mergedError == null ? "completed" : "failed");
        if (mergedError != null) {
            merged.put("error", mergedError);
        } else {
            merged.remove("error");
        }
        return merged;
    }

    @PreDestroy
    public void shutdown() {
        channelPool.shutdownNow();
    }

    /** 一条通道的结果：{@code value} 与 {@code error} 恰有一个非空。 */
    private static final class ChannelOutcome {
        private final Object value;
        private final String error;

        private ChannelOutcome(Object value, String error) {
            this.value = value;
            this.error = error;
        }

        private static ChannelOutcome of(Object value) {
            return new ChannelOutcome(value, null);
        }

        private static ChannelOutcome failed(String error) {
            return new ChannelOutcome(null, error == null ? "agent-k 调用失败" : error);
        }
    }

    private String resolveDefaultModel(OfflineJob job) {
        if (job != null && StringUtils.hasText(job.getModel())) {
            return job.getModel().trim();
        }
        if (job == null || !StringUtils.hasText(job.getProfileYaml())) {
            return null;
        }
        try {
            String model = objectMapper.readTree(job.getProfileYaml()).path("defaultModel").asText("");
            return StringUtils.hasText(model) ? model.trim() : null;
        } catch (Exception ignored) {
            return null;
        }
    }

    private static String summarizeAttachments(LoopUserMessage message) {
        if (message == null || !message.hasAttachments()) {
            return "none";
        }
        StringBuilder sb = new StringBuilder();
        for (Attachment attachment : message.getAttachments()) {
            if (attachment == null) {
                continue;
            }
            if (sb.length() > 0) {
                sb.append("; ");
            }
            sb.append(attachment.getType()).append('=').append(attachment.getUri());
            if (StringUtils.hasText(attachment.getMimeType())) {
                sb.append('(').append(attachment.getMimeType()).append(')');
            }
        }
        return sb.length() == 0 ? "none" : sb.toString();
    }

    private static String blankToDash(String value) {
        return StringUtils.hasText(value) ? value : "-";
    }

    private DatasetTask findDatasetTask(long datasetId, String taskKey) {
        List<DatasetTask> tasks = mapper.listAllTasks(datasetId);
        for (DatasetTask task : tasks) {
            if (taskKey != null && taskKey.equals(task.getTaskKey())) {
                return task;
            }
        }
        return null;
    }

    /** 计数在客户端边收事件边算好；这里只补时长，字段与旧 {@code metrics_json} 一致。 */
    private ObjectNode metrics(JsonNode summary) {
        JsonNode counters = summary.path("metrics");
        ObjectNode metrics = objectMapper.createObjectNode();
        metrics.put("steps", counters.path("steps").asInt());
        metrics.put("inputTokens", counters.path("inputTokens").asInt());
        metrics.put("outputTokens", counters.path("outputTokens").asInt());
        metrics.put("durationMillis", durationMillis(summary));
        metrics.put("toolCalls", counters.path("toolCalls").asInt());
        metrics.put("toolErrors", counters.path("toolErrors").asInt());
        return metrics;
    }

    private static long durationMillis(JsonNode trace) {
        String start = trace.path("createdAt").asText();
        String finish = trace.path("finishedAt").asText();
        if (start.isEmpty() || finish.isEmpty()) {
            return 0;
        }
        try {
            return Duration.between(OffsetDateTime.parse(start), OffsetDateTime.parse(finish)).toMillis();
        } catch (RuntimeException ignored) {
            return 0;
        }
    }

    /** 摘要失败只来自 agent-k {@code execution.finished} 的 status，不把工具过程错误再封装一遍。 */
    private static String failureMessage(JsonNode summary) {
        String error = summary.path("error").asText();
        return error.isEmpty() ? "agent-k execution finished" : error;
    }
}
