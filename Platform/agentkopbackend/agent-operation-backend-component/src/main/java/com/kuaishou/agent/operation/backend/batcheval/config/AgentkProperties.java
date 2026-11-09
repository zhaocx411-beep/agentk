package com.kuaishou.agent.operation.backend.batcheval.config;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.stereotype.Component;

import lombok.Data;

/**
 * 离线评测 agent-k 出站配置，prefix {@code agent.operation.backend.agentk.batch-eval}。
 * <p>
 * 连接参数不在此处：出站统一走在线 gRPC 客户端（KESS），端点与鉴权由
 * {@code com.kuaishou.agent.operation.backend.agentk.AgentKProperties} 管。
 * <p>
 * 模型名单优先读 KConf {@code klingai.agentk.models}（与 agent-k 同源）；
 * 下列 chat-models / tool-models 仅作本地/空 kconf 时的兜底。
 * 工具名单优先拉 agent-k {@code GET /v1/capabilities}；失败时用代码内 mock，
 * {@code tools} 仅作额外并入。
 */
@Data
@Component
@ConfigurationProperties(prefix = "agent.operation.backend.agentk.batch-eval")
public class AgentkProperties {

    /**
     * 会话 events 的轮询间隔。每轮都是整页拉回并逐条 upsert（与在线闭环同一套写法），
     * 所以间隔直接决定数据库写入放大：漫剧这类半小时级用例按 500ms 轮询会产生几十万次
     * 大 JSON 重写。默认 5s，够跟上进度看板，又把写入压回可接受量级。
     */
    private int pollIntervalMs = 5000;

    /**
     * 同一时刻真正在跑的用例数上限。以前用 cached pool，一个 job 的全部用例会同时在跑，
     * 每条都在内存里攒事件、往库里写事件，2C4G 容器扛不住。出站 RPC 名额
     * （{@link #maxOutboundInFlight}）限的是并发请求，不是并发用例，两者不能互相替代。
     */
    private int maxConcurrentCells = 2;

    /**
     * pipeline 执行快照的轮询间隔。pipeline 内部按分钟级轮询远端任务，跟着事件流用
     * 500ms 只会空转打 agent-k，故单独给一个更长的间隔。
     */
    private int pipelinePollIntervalMs = 5000;

    /**
     * 单条用例等待 agent-k 会话回到空闲的上限。
     * 与 agent-k 异步视频 workflow 的 Temporal 上限一致（24 小时），避免长视频在
     * job.completed 前被离线轮询误杀。
     */
    private int runTimeoutSeconds = 86400;

    private int maxRetries = 3;

    /**
     * 同一 job 内相邻两条用例提交到执行池的间隔。只平滑 StartExecution 突发，
     * 不限制同时在跑的条数；真正打到 agent-k 的并发见 {@link #maxOutboundInFlight}。
     */
    private int submitStaggerMs = 3000;

    /**
     * 离线打到 agent-k 的同时在途 RPC 上限（建会话 / 发消息 / 拉 events / pipeline）。
     * OkHttp 对同一 host 默认最多 5 路（HTTP/2 gRPC 同样生效），unary 10s 超时含排队时间，
     * 且连接失败不重试。离线默认占 3 路，留给在线对话余量。
     */
    private int maxOutboundInFlight = 3;

    /**
     * 删除离线任务时单批删除的事件行数。一批一个小事务，别调太大：
     * 单条事件 payload 就有几 KB 到几十 KB，批次越大事务持锁越久。
     */
    private int purgeBatchSize = 500;

    /**
     * 删除接口同步清理的事件行数阈值。低于它就当场删完直接返回（小 job 与改造前体感一致），
     * 超过则只标记 {@code DELETING} 并转后台分批清理。
     */
    private int inlinePurgeEventThreshold = 2000;

    /** 清理器扫 {@code DELETING} 任务的间隔，兼作进程重启后的续跑入口。 */
    private int purgeSweepIntervalSeconds = 60;

    /** 可选聊天模型，对应目标环境 models.yaml / kconf 里 kind=chat 的条目名。 */
    private List<String> chatModels = new ArrayList<>();

    /** 缺省聊天模型；留空取 {@link #chatModels} 首项。必须在 chatModels 内。 */
    private String defaultChatModel;

    /**
     * 额外并入工具名单的名称（可选）。主名单来自 agent-k capabilities 或代码内 mock，
     * 本列表用于临时补一条尚未进 mock 的工具名。
     */
    private List<String> tools = new ArrayList<>();

    /**
     * 每个工具可选的模型：工具名 -> 该工具 {@code model} 入参可取的模型条目名。
     * 提交时选中的值编译进 profile 的 {@code toolDefaults.<tool>.model}，与聊天模型互不干扰。
     */
    private Map<String, List<String>> toolModels = new LinkedHashMap<>();

    public int getPollIntervalMillis() {
        return pollIntervalMs;
    }

    public int getPipelinePollIntervalMillis() {
        return pipelinePollIntervalMs;
    }

    public int getRunTimeoutSeconds() {
        return runTimeoutSeconds;
    }

    public int getMaxRetries() {
        return maxRetries;
    }

    public int getSubmitStaggerMs() {
        return submitStaggerMs;
    }

    public int getMaxOutboundInFlight() {
        return maxOutboundInFlight;
    }

    /** 至少 1：配成 0 会让固定线程池直接构造失败，配置写错不该炸启动。 */
    public int getMaxConcurrentCells() {
        return Math.max(1, maxConcurrentCells);
    }

    /** 至少 1：配成 0 会让清理循环一批都删不动，永远收不了尾。 */
    public int getPurgeBatchSize() {
        return Math.max(1, purgeBatchSize);
    }

    /** 允许配 0（一律走异步）。 */
    public int getInlinePurgeEventThreshold() {
        return Math.max(0, inlinePurgeEventThreshold);
    }

    public int getPurgeSweepIntervalSeconds() {
        return purgeSweepIntervalSeconds;
    }
}
