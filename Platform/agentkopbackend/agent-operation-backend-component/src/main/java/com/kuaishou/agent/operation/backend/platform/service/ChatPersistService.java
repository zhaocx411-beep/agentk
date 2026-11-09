package com.kuaishou.agent.operation.backend.platform.service;

import java.time.LocalDateTime;
import java.util.Collection;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;

import com.baomidou.mybatisplus.core.metadata.IPage;
import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.platform.entity.AgentkCallLog;
import com.kuaishou.agent.operation.backend.platform.entity.ChatAsset;
import com.kuaishou.agent.operation.backend.platform.entity.ChatMessage;
import com.kuaishou.agent.operation.backend.platform.entity.ChatRun;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.entity.PipelineExec;
import com.kuaishou.agent.operation.backend.platform.entity.RunEvent;

import lombok.Data;

/**
 * Chat 落库：先写平台行 → 调 agent-k → 回填 sessionId/runId/游标。
 */
public interface ChatPersistService {

    CreateSessionBegin beginCreateSession(String userId, String tenantId, String title,
            Long agentVersionId, Long workflowVersionId, String specJson, String profileYaml);

    CreateSessionBegin beginCreateSession(String userId, String tenantId, String title,
            Long agentVersionId, Long workflowVersionId, String specJson, String profileYaml,
            EffectiveSnapshot effective);

    void completeCreateSession(CreateSessionBegin begin, String agentkSessionId, String profileVersion,
            boolean success, Integer httpStatus, String error);

    /**
     * 同一条「建会话」平台行上改 profile 后重试上游（不 insert 新行）。
     * 用于官方快速开始：工具 name/ref 失败后清空 tools 再调 agent-k。
     */
    void reviseCreateSessionAttempt(CreateSessionBegin begin, String specJson, String profileYaml,
            EffectiveSnapshot effective);

    PostMessageBegin beginPostMessage(ChatSession session, String text);

    void completePostMessage(PostMessageBegin begin, String agentkRunId, String status,
            boolean success, Integer httpStatus, String error);

    void updateStreamCursor(String agentkSessionId, String agentkRunId, int lastEventSeq,
            String activeRunStatus);

    /**
     * 带归属校验的游标更新；session 不存在或非本人时抛错。
     */
    void updateStreamCursorOwned(String userId, String agentkSessionId, String agentkRunId,
            int lastEventSeq, String activeRunStatus);

    void upsertJob(String agentkSessionId, String agentkRunId, String agentkJobId,
            String toolCallId, String kind, String status, String wakeRunId);

    Optional<ChatSession> findByAgentkSessionId(String agentkSessionId);

    Optional<ChatSession> findById(long sessionId);

    Optional<ChatSession> findByPublicId(String publicId);

    /**
     * 校验 session 归属当前用户；不存在 → IllegalArgumentException；非本人 → PlatformAccessException。
     */
    ChatSession requireOwnedSession(long sessionId, String userId);

    ChatSession requireOwnedByPublicId(String publicId, String userId);

    ChatSession requireOwnedByAgentkSessionId(String agentkSessionId, String userId);

    /**
     * 手动修改会话标题（仅平台库；agent-k 无 UpdateSession）。
     * 按 agentkSessionId（ses_xxx）定位 aop_session；改名后不再参与首条消息自动派生。
     */
    ChatSession updateSessionTitleOwned(String agentkSessionId, String userId, String title);

    List<ChatSession> listMine(String userId, int limit);

    /** 按绑定的 AgentLoop 版本查「我的」会话。 */
    List<ChatSession> listMineByAgentVersion(String userId, long agentVersionId, int limit);

    /** 按绑定的 Workflow 版本查「我的」会话。 */
    List<ChatSession> listMineByWorkflowVersion(String userId, long workflowVersionId, int limit);

    /**
     * 管理回看：按 AgentLoop 版本查全部用户会话（不按 user 过滤）。
     */
    List<ChatSession> listByAgentVersion(long agentVersionId, int limit);

    /** 管理回看：按多个 AgentLoop 版本 id 查会话。 */
    List<ChatSession> listByAgentVersions(List<Long> agentVersionIds, int limit);

    /** 管理回看：按 Workflow 版本查全部用户会话。 */
    List<ChatSession> listByWorkflowVersion(long workflowVersionId, int limit);

    /** 管理回看：按多个 Workflow 版本 id 查会话。 */
    List<ChatSession> listByWorkflowVersions(List<Long> workflowVersionIds, int limit);

    /**
     * 管理回看：条件 + MyBatis-Plus 分页（条件下推 SQL）。
     * <p>
     * 版本 id 列表可空：空则为全局会话分页（鉴权由调用方保证仅 admin）。
     */
    IPage<ChatSession> pageForAdminReview(AdminSessionPageQuery query);

    /** 管理回看：按平台 session 主键取行（不做归属校验）。 */
    ChatSession requireSession(long sessionId);

    List<ChatMessage> listMessages(long sessionId, int limit);

    List<ChatMessage> listMessagesOwned(long sessionId, String userId, int limit);

    List<ChatAsset> listAssetsOwned(String userId, String assetType, int limit);

    /** 管理回看 Case：按平台 session 主键取产物（不做归属校验）。 */
    List<ChatAsset> listAssetsBySession(long sessionId, int limit);

    IPage<ChatAsset> pageAssetsOwned(String userId, ChatAssetPageQuery query);

    /** 作品集公开展示：不按 userId 隔离。 */
    IPage<ChatAsset> pageAssets(ChatAssetPageQuery query);

    /**
     * 作品集按 round 聚合用：按筛选条件拉取资产（新→旧），上限 {@code maxRows}。
     * 调用方再按 roundKey 分组分页。
     */
    List<ChatAsset> listAssetsForPortfolioGrouping(ChatAssetPageQuery query, int maxRows);

    ChatAsset requireOwnedAsset(long assetId, String userId);

    ChatAsset requireAsset(long assetId);

    Map<Long, ChatSession> mapSessionsByIds(List<Long> sessionIds);

    Optional<ChatRun> findRun(long runId);

    List<ChatAsset> listAssetsInSameRound(ChatAsset anchor);

    /**
     * 会话绑定的智能体名（仅 aop_agent.name，不含 workflow）。
     */
    String resolveSessionAgentName(ChatSession session);

    /**
     * 会话绑定的 Workflow 名；仅 {@code session_kind=workflow} 时有值。
     */
    String resolveSessionWorkflowName(ChatSession session);

    /**
     * 按智能体精确名称反查 session id（仅 aop_agent.name；排除 session_kind=workflow）。
     */
    List<Long> listSessionIdsByBoundAgentName(String agentName);

    /**
     * 按 Workflow 精确名称反查 session id（仅 session_kind=workflow 且 aop_workflow.name）。
     */
    List<Long> listSessionIdsByBoundWorkflowName(String workflowName);

    /**
     * 作品集筛选项：{@code aop_asset.agent_name} 去重。
     * {@code userId} 非空则仅本人；空则全站。
     * {@code publicOnly} 为 true 时排除 private 智能体 / Workflow；API taskType 原样保留。
     */
    List<String> listBoundAgentNames(String userId, boolean publicOnly);

    /**
     * 作品集筛选项：有作品的 Workflow 去重名（仅 session_kind=workflow）。
     */
    List<String> listBoundWorkflowNames(String userId);

    List<ChatSession> listSessionsForAssetBackfill(String userId, int limit, int offset);

    /** 全站会话（作品集历史回填），按 updated_at 倒序。 */
    List<ChatSession> listSessionsForGlobalAssetBackfill(int limit, int offset);

    List<ChatRun> listRuns(long sessionId, int limit);

    List<ChatRun> listRunsOwned(long sessionId, String userId, int limit);

    ChatRun requireRun(long runId);

    /** 按 agent-k run id 取平台 run（不做归属校验，供管理回看）。 */
    ChatRun requireRunByAgentkId(String agentkRunId);

    /** 批量查出 status=failed 的 agent-k execution id。 */
    Set<String> listFailedAgentkRunIds(Collection<String> agentkRunIds);

    List<RunEvent> listRunEvents(long runId, int limit);

    /** 管理回看 Case：按会话拉事件，seq 升序（完整过程）。 */
    List<RunEvent> listSessionEvents(long sessionId, String agentkSessionId, int limit);

    /** 管理回看：按 agentkSessionId 分页查事件，平台自增 seq 倒序。 */
    IPage<RunEvent> pageSessionEventsByAgentk(String agentkSessionId, long page, long pageSize);

    List<AgentkCallLog> listCallLogsBySession(long sessionId, int limit);

    ChatRun requireOwnedRun(long runId, String userId);

    ChatRun requireOwnedRunByAgentkId(String agentkRunId, String userId);

    List<RunEvent> listRunEventsOwned(long runId, String userId, int limit);

    List<RunEvent> listRunEventsByAgentkRunOwned(String agentkRunId, String userId, int limit);

    List<AgentkCallLog> listCallLogsMine(String userId, int limit);

    List<AgentkCallLog> listCallLogsBySessionOwned(long sessionId, String userId, int limit);

    void persistRunEvent(String agentkSessionId, String agentkRunId, int seq, String kind,
            JsonNode payload, String agentkCreatedAt);

    void persistRunEvent(String agentkSessionId, String agentkRunId, int seq, String kind,
            JsonNode payload, String agentkCreatedAt, String roundId, String agentkEventId,
            String eventExecutionId);

    /**
     * 同上，额外指定事件行的 {@code entry_source}（只有 {@code online} / {@code offline_batch}）。
     * <p>
     * 离线跑数用它把事件标成 {@code offline_batch}，从而不必 join 会话就能筛出、分批删除。
     * 传空时跟随会话自身的入口；空和历史 {@code official_quick} 都按 {@code online}。
     * 在线闭环调上面的重载即可。
     */
    void persistRunEvent(String agentkSessionId, String agentkRunId, int seq, String kind,
            JsonNode payload, String agentkCreatedAt, String roundId, String agentkEventId,
            String eventExecutionId, String entrySource);

    /**
     * 按<strong>写入顺序</strong>（自增 id）翻页取会话事件，用于拼装 trace 详情。
     * <p>
     * 不能按 {@code seq} 排：agent-k gRPC 适配层的 seq 是 ListEvents 页内序号，
     * 新事件在前，同一条事件的 seq 会随着后续事件到达而改变。
     */
    IPage<RunEvent> pageSessionEventsInWriteOrder(String agentkSessionId, long page, long pageSize);

    /**
     * 同 {@link #pageSessionEventsInWriteOrder(String, long, long)}，可再按本轮 {@code agentkRunId}
     * （executionId）收窄。多轮共用 session 时每条 cell 只看自己这一轮。
     */
    IPage<RunEvent> pageSessionEventsInWriteOrder(String agentkSessionId, String agentkRunId,
            long page, long pageSize);

    void upsertAssets(String agentkSessionId, String agentkRunId, List<SyncedAsset> assets);

    /**
     * 一次离线跑数残留的事件行数，用于判断删除走同步还是异步。
     */
    long countOfflineBatchEvents(List<String> agentkSessionIds);

    /**
     * 分批删除一次离线跑数落到事件/作品/会话体系的数据，每次调用是一个小事务。
     * <p>
     * 离线会写 {@code aop_session_event}、登记 {@code aop_chat_session}、写
     * {@code aop_agentk_call_log}、产物进 {@code aop_asset}，删除范围与此对齐。按 cell 的
     * agent-k sessionId，以及 {@code entry_source=offline_batch} 且标题为
     * {@code jobBizId/taskKey} 的会话定位；只动离线入口，不会删在线对话。
     * <p>
     * 顺序是事件 → 产物/调用日志 → 会话：事件按 {@code agentk_session_id} 删，会话行没了也能续删。
     *
     * @param batchSize 本次最多删除的事件行数
     * @return 本次删除的行数；返回 0 表示已清空，调用方可以收尾删主表
     */
    int purgeOfflineBatchPersisted(String jobBizId, List<String> agentkSessionIds, int batchSize);

    /**
     * 无平台 ChatSession 时的外部任务资产落库（如 KEffects）。
     * <p>
     * {@code sessionId} 由调用方指定（例如把 taskId 解析/哈希为 Long）；
     * {@code agentkSessionId} 建议写 taskId 原文以便溯源；
     * 资产侧 {@code workflowExecId} 同样建议写 taskId，对齐 pipeline 唯一键。
     *
     * @return 本次写入或更新的条数
     */
    int upsertStandaloneAssets(String userId, String tenantId, long sessionId,
            String agentkSessionId, List<SyncedAsset> assets);

    /**
     * 投影/更新一次 pipeline 单独执行：完整 args / output / status。
     * 按 {@code agentkExecId} 幂等 upsert；会话不存在时静默跳过。
     */
    PipelineExec upsertPipelineExec(String agentkSessionId, SyncedPipelineExec exec);

    /**
     * 无平台 ChatSession 时的外部 API 任务执行落库（如 KEffects）。
     * <p>
     * 按 {@code agentkExecId}（通常为上游 taskId）幂等 upsert；
     * {@code sessionId} / {@code agentkSessionId} 由调用方指定（建议 sessionId 哈希、agentkSessionId=taskId）。
     * {@code pipelineName}（taskType）按 {@code aop_workflow.agentk_id} 解析并写入 {@code workflow_id}。
     */
    PipelineExec upsertStandalonePipelineExec(String userId, String tenantId, long sessionId,
            String agentkSessionId, SyncedPipelineExec exec);

    List<PipelineExec> listPipelineExecsOwned(long sessionId, String userId, int limit);

    /** 按 {@code aop_pipeline_exec.agentk_session_id} 查执行列表（不做归属校验）。 */
    List<PipelineExec> listPipelineExecsByAgentkSessionId(String agentkSessionId, int limit);

    /**
     * 按 {@code pipeline_name} 分页查询（精确匹配）。
     * {@code userId} 非空时只返回该用户记录；为空则不按用户过滤。
     */
    IPage<PipelineExec> pagePipelineExecsByPipelineName(String pipelineName, String userId,
            long page, long pageSize);

    /**
     * Capability Lab 历史：{@code pipeline_name=capability_run}，可按 capabilityId / userId / status 筛。
     * {@code userId} 非空只返回该用户。
     */
    IPage<PipelineExec> pageCapabilityLabExecs(String capabilityId, String userId, String status,
            long page, long pageSize);

    /**
     * Agent100 / Narrato 历史：按 pipeline_name 集合筛选，按创建时间倒序。
     * {@code userId} 非空只返回该用户。
     */
    List<PipelineExec> listPipelineExecsByPipelineNames(Collection<String> pipelineNames, String userId,
            int limit);

    /**
     * 当前过滤条件下出现过的 capabilityId（去重，升序），给「按工具」Tab。
     */
    List<String> listCapabilityLabCapabilityIds(String userId);

    PipelineExec requireOwnedPipelineExec(long id, String userId);

    PipelineExec requireOwnedPipelineExecByAgentkId(String agentkExecId, String userId);

    /** 按主键取 pipeline 执行（不做归属校验）。 */
    PipelineExec requirePipelineExec(long id);

    /** 按 agent-k exec id 取 pipeline 执行（不做归属校验）。 */
    PipelineExec requirePipelineExecByAgentkId(String agentkExecId);

    /** 按 agentk_exec_id 批量查询，保持传入顺序去重。 */
    List<PipelineExec> listPipelineExecsByAgentkExecIds(Collection<String> agentkExecIds);

    /** 刷新资产预签名 URL（过期续签后回写，避免下次列表再签）。 */
    void refreshAssetUrl(long assetId, String assetUrl);

    /** 批量刷新预签名 URL；空 map 直接返回。 */
    void batchRefreshAssetUrls(Map<Long, String> assetIdToUrl);

    @Data
    class CreateSessionBegin {
        private ChatSession session;
        private AgentkCallLog callLog;
    }

    @Data
    class PostMessageBegin {
        private ChatSession session;
        private ChatRun run;
        private ChatMessage message;
        private AgentkCallLog callLog;
    }

    @Data
    class SyncedAsset {
        private Integer seq;
        private String assetType;
        private String assetUrl;
        private String promptText;
        /** agent_loop / pipeline / capability / api */
        private String sourceKind;
        /** online / offline，默认可由 batcheval_offline_job_cell 推断 */
        private String source;
        private String sourceName;
        /** 智能体名冗余；写入时快照当时的 aop_agent.name（在线 pipeline 也会写）。 */
        private String agentName;
        private String roundId;
        private String workflowExecId;
    }

    /** pipeline 执行投影入参（Service 层）。 */
    @Data
    class SyncedPipelineExec {
        private String agentkExecId;
        private String pipelineName;
        /** Lab 写入 agent-k capability id，便于按工具查历史。 */
        private String capabilityId;
        private String status;
        private JsonNode args;
        private JsonNode output;
        private String profileVersion;
        private String error;
        /** 上游 ISO 时间，可空 */
        private String startedAt;
        private String finishedAt;
    }

    @Data
    class EffectiveSnapshot {
        private String profileVersion;
        private String model;
        private String toolsJson;
        private String toolModelsJson;
        private String configSource;
        /** agent_loop / workflow / custom */
        private String sessionKind;
        /**
         * 会话入口，只区分在线 / 离线：空则落库 {@code online}（含官方快速入口）；
         * 离线跑数写 {@code offline_batch}。
         */
        private String entrySource;
    }

    /** 管理回看分页查询条件（Service 层）。 */
    @Data
    class AdminSessionPageQuery {
        private List<Long> agentVersionIds;
        private List<Long> workflowVersionIds;
        /** agent_loop / workflow / custom */
        private String sessionKind;
        private String userId;
        private String status;
        private String titleKeyword;
        private LocalDateTime createdFrom;
        private LocalDateTime createdTo;
        /** 平台会话主键，精确匹配 */
        private Long sessionId;
        /** agent-k session id（ses_xxx），精确匹配 */
        private String agentkSessionId;
        /**
         * 为 true 时不过滤 offline_batch；默认 false，会话列表排除离线跑数入口。
         */
        private boolean includeOfflineBatch;
        private long page = 1;
        private long pageSize = 20;
    }

    /** 作品集分页查询（Service 层）。 */
    @Data
    class ChatAssetPageQuery {
        /** 可选；公开展示场景不强制隔离，传入则按 user_id 精确过滤 */
        private String userId;
        private String assetType;
        private String sourceKind;
        /** online / offline，可选 */
        private String source;
        private Long sessionId;
        private Long runId;
        private String agentkRunId;
        private String workflowExecId;
        private String promptKeyword;
        /**
         * 智能体名称（精确匹配 aop_agent.name）。
         */
        private String agentName;
        /**
         * Workflow 名称（精确匹配 aop_workflow.name；仅 session_kind=workflow）。
         */
        private String workflowName;
        private long page = 1;
        private long pageSize = 20;
    }
}
