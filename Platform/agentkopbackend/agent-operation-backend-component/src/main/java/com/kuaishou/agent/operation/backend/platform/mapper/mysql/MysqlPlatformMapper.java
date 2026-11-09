package com.kuaishou.agent.operation.backend.platform.mapper.mysql;

import java.sql.PreparedStatement;
import java.sql.Statement;
import java.sql.Timestamp;
import java.time.LocalDateTime;
import java.util.List;
import java.util.Optional;

import org.springframework.jdbc.core.BeanPropertyRowMapper;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.jdbc.support.GeneratedKeyHolder;
import org.springframework.jdbc.support.KeyHolder;
import org.springframework.stereotype.Repository;

import com.kuaishou.agent.operation.backend.platform.dao.mapper.ChatMessageMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.ChatRunMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.ChatSessionMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.RunEventMapper;
import com.kuaishou.agent.operation.backend.platform.entity.ChatMessage;
import com.kuaishou.agent.operation.backend.platform.entity.ChatRun;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.entity.RunEvent;
import com.kuaishou.agent.operation.backend.platform.mapper.PlatformMapper;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.Agent;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.AgentkCallLog;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.ChatJob;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.Dataset;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.DatasetSnapshot;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.Workflow;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.WorkflowVersion;

/**
 * MySQL 实现：写入 docs/sql/aop_platform_v1.sql 中的 aop_* 表。
 */
@Repository
public class MysqlPlatformMapper implements PlatformMapper {

    private final JdbcTemplate jdbc;
    private final ChatSessionMapper chatSessionMapper;
    private final ChatRunMapper chatRunMapper;
    private final ChatMessageMapper chatMessageMapper;
    private final RunEventMapper runEventMapper;

    public MysqlPlatformMapper(JdbcTemplate jdbc, ChatSessionMapper chatSessionMapper,
            ChatRunMapper chatRunMapper, ChatMessageMapper chatMessageMapper, RunEventMapper runEventMapper) {
        this.jdbc = jdbc;
        this.chatSessionMapper = chatSessionMapper;
        this.chatRunMapper = chatRunMapper;
        this.chatMessageMapper = chatMessageMapper;
        this.runEventMapper = runEventMapper;
    }

    private static <T> RowMapper<T> mapper(Class<T> type) {
        return BeanPropertyRowMapper.newInstance(type);
    }

    private static Timestamp ts(LocalDateTime t) {
        return t == null ? null : Timestamp.valueOf(t);
    }

    private long insertAndReturnId(String sql, PreparedStatementBinder binder) {
        KeyHolder keys = new GeneratedKeyHolder();
        jdbc.update(con -> {
            PreparedStatement ps = con.prepareStatement(sql, Statement.RETURN_GENERATED_KEYS);
            binder.bind(ps);
            return ps;
        }, keys);
        Number key = keys.getKey();
        if (key == null) {
            throw new IllegalStateException("no generated key for: " + sql);
        }
        return key.longValue();
    }

    @FunctionalInterface
    private interface PreparedStatementBinder {
        void bind(PreparedStatement ps) throws java.sql.SQLException;
    }

    // ---------- call log ----------

    @Override
    public AgentkCallLog insertCallLog(AgentkCallLog log) {
        long id = insertAndReturnId(
                "INSERT INTO aop_agentk_call_log (biz_type, biz_id, user_id, tenant_id, api, request_json, "
                        + "status, http_status, error, agentk_session_id, agentk_run_id, agentk_job_id, "
                        + "agentk_workflow_exec_id, created_at, finished_at) "
                        + "VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
                ps -> {
                    ps.setString(1, nullToEmpty(log.getBizType()));
                    ps.setLong(2, log.getBizId());
                    ps.setString(3, nullToEmpty(log.getUserId()));
                    ps.setString(4, nullToEmpty(log.getTenantId()));
                    ps.setString(5, nullToEmpty(log.getApi()));
                    ps.setString(6, log.getRequestJson());
                    ps.setString(7, nullToEmpty(log.getStatus()));
                    if (log.getHttpStatus() == null) {
                        ps.setObject(8, null);
                    } else {
                        ps.setInt(8, log.getHttpStatus());
                    }
                    ps.setString(9, log.getError());
                    ps.setString(10, log.getAgentkSessionId());
                    ps.setString(11, log.getAgentkRunId());
                    ps.setString(12, log.getAgentkJobId());
                    ps.setString(13, log.getAgentkWorkflowExecId());
                    ps.setTimestamp(14, ts(log.getCreatedAt() == null ? LocalDateTime.now() : log.getCreatedAt()));
                    ps.setTimestamp(15, ts(log.getFinishedAt()));
                });
        log.setId(id);
        return log;
    }

    @Override
    public void updateCallLog(AgentkCallLog log) {
        jdbc.update(
                "UPDATE aop_agentk_call_log SET status=?, http_status=?, error=?, agentk_session_id=?, "
                        + "agentk_run_id=?, agentk_job_id=?, agentk_workflow_exec_id=?, finished_at=? WHERE id=?",
                log.getStatus(), log.getHttpStatus(), log.getError(), log.getAgentkSessionId(),
                log.getAgentkRunId(), log.getAgentkJobId(), log.getAgentkWorkflowExecId(),
                ts(log.getFinishedAt()), log.getId());
    }

    // ---------- agent ----------

    @Override
    public Agent insertAgent(Agent agent) {
        long id = insertAndReturnId(
                "INSERT INTO aop_agent (name, description, owner_user_id, tenant_id, latest_version, status, "
                        + "created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)",
                ps -> {
                    ps.setString(1, nullToEmpty(agent.getName()));
                    ps.setString(2, nullToEmpty(agent.getDescription()));
                    ps.setString(3, nullToEmpty(agent.getOwnerUserId()));
                    ps.setString(4, nullToEmpty(agent.getTenantId()));
                    ps.setInt(5, agent.getLatestVersion());
                    ps.setString(6, nullToEmpty(agent.getStatus()));
                    ps.setTimestamp(7, ts(agent.getCreatedAt() == null ? LocalDateTime.now() : agent.getCreatedAt()));
                    ps.setTimestamp(8, ts(agent.getUpdatedAt() == null ? LocalDateTime.now() : agent.getUpdatedAt()));
                });
        agent.setId(id);
        return agent;
    }

    @Override
    public void updateAgent(Agent agent) {
        jdbc.update(
                "UPDATE aop_agent SET name=?, description=?, owner_user_id=?, tenant_id=?, latest_version=?, "
                        + "status=?, updated_at=? WHERE id=?",
                agent.getName(), agent.getDescription(), agent.getOwnerUserId(), agent.getTenantId(),
                agent.getLatestVersion(), agent.getStatus(),
                ts(agent.getUpdatedAt() == null ? LocalDateTime.now() : agent.getUpdatedAt()), agent.getId());
    }

    @Override
    public Optional<Agent> findAgent(long id) {
        List<Agent> list = jdbc.query("SELECT * FROM aop_agent WHERE id=?", mapper(Agent.class), id);
        return list.stream().findFirst();
    }

    @Override
    public List<Agent> listAgentsByOwner(String userId) {
        return jdbc.query("SELECT * FROM aop_agent WHERE owner_user_id=? ORDER BY id DESC",
                mapper(Agent.class), userId);
    }

    @Override
    public AgentVersion insertAgentVersion(AgentVersion version) {
        long id = insertAndReturnId(
                "INSERT INTO aop_agent_version (agent_id, version, profile_yaml, content_hash, "
                        + "status, created_by, created_at, published_at, published_by) "
                        + "VALUES (?,?,?,?,?,?,?,?,?)",
                ps -> {
                    ps.setLong(1, version.getAgentId());
                    ps.setInt(2, version.getVersion());
                    ps.setString(3, nullToEmpty(version.getProfileYaml()));
                    ps.setString(4, nullToEmpty(version.getContentHash()));
                    ps.setString(5, nullToEmpty(version.getStatus()));
                    ps.setString(6, nullToEmpty(version.getCreatedBy()));
                    ps.setTimestamp(7, ts(version.getCreatedAt() == null ? LocalDateTime.now() : version.getCreatedAt()));
                    ps.setTimestamp(8, ts(version.getPublishedAt()));
                    ps.setString(9, nullToEmpty(version.getPublishedBy()));
                });
        version.setId(id);
        return version;
    }

    @Override
    public void updateAgentVersion(AgentVersion version) {
        jdbc.update(
                "UPDATE aop_agent_version SET profile_yaml=?, content_hash=?, status=?, published_at=?, published_by=? "
                        + "WHERE id=?",
                version.getProfileYaml(), version.getContentHash(), version.getStatus(),
                ts(version.getPublishedAt()), nullToEmpty(version.getPublishedBy()), version.getId());
    }

    @Override
    public Optional<AgentVersion> findAgentVersion(long id) {
        List<AgentVersion> list = jdbc.query("SELECT * FROM aop_agent_version WHERE id=?",
                mapper(AgentVersion.class), id);
        return list.stream().findFirst();
    }

    @Override
    public Optional<AgentVersion> findLatestPublishedAgentVersion(long agentId) {
        List<AgentVersion> list = jdbc.query(
                "SELECT * FROM aop_agent_version WHERE agent_id=? AND status='published' "
                        + "ORDER BY version DESC LIMIT 1",
                mapper(AgentVersion.class), agentId);
        return list.stream().findFirst();
    }

    @Override
    public List<AgentVersion> listAgentVersions(long agentId) {
        return jdbc.query(
                "SELECT * FROM aop_agent_version WHERE agent_id=? ORDER BY version DESC",
                mapper(AgentVersion.class), agentId);
    }

    // ---------- workflow ----------

    @Override
    public Workflow insertWorkflow(Workflow workflow) {
        long id = insertAndReturnId(
                "INSERT INTO aop_workflow (name, description, owner_user_id, tenant_id, latest_version, status, "
                        + "created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)",
                ps -> {
                    ps.setString(1, nullToEmpty(workflow.getName()));
                    ps.setString(2, nullToEmpty(workflow.getDescription()));
                    ps.setString(3, nullToEmpty(workflow.getOwnerUserId()));
                    ps.setString(4, nullToEmpty(workflow.getTenantId()));
                    ps.setInt(5, workflow.getLatestVersion());
                    ps.setString(6, nullToEmpty(workflow.getStatus()));
                    ps.setTimestamp(7, ts(workflow.getCreatedAt() == null ? LocalDateTime.now() : workflow.getCreatedAt()));
                    ps.setTimestamp(8, ts(workflow.getUpdatedAt() == null ? LocalDateTime.now() : workflow.getUpdatedAt()));
                });
        workflow.setId(id);
        return workflow;
    }

    @Override
    public void updateWorkflow(Workflow workflow) {
        jdbc.update(
                "UPDATE aop_workflow SET name=?, description=?, owner_user_id=?, tenant_id=?, latest_version=?, "
                        + "status=?, updated_at=? WHERE id=?",
                workflow.getName(), workflow.getDescription(), workflow.getOwnerUserId(), workflow.getTenantId(),
                workflow.getLatestVersion(), workflow.getStatus(),
                ts(workflow.getUpdatedAt() == null ? LocalDateTime.now() : workflow.getUpdatedAt()),
                workflow.getId());
    }

    @Override
    public Optional<Workflow> findWorkflow(long id) {
        List<Workflow> list = jdbc.query("SELECT * FROM aop_workflow WHERE id=?", mapper(Workflow.class), id);
        return list.stream().findFirst();
    }

    @Override
    public List<Workflow> listWorkflowsByOwner(String userId) {
        return jdbc.query("SELECT * FROM aop_workflow WHERE owner_user_id=? ORDER BY id DESC",
                mapper(Workflow.class), userId);
    }

    @Override
    public WorkflowVersion insertWorkflowVersion(WorkflowVersion version) {
        long id = insertAndReturnId(
                "INSERT INTO aop_workflow_version (workflow_id, version, definition_json, content_hash, status, "
                        + "created_by, created_at, published_at, published_by) VALUES (?,?,?,?,?,?,?,?,?)",
                ps -> {
                    ps.setLong(1, version.getWorkflowId());
                    ps.setInt(2, version.getVersion());
                    ps.setString(3, nullToEmpty(version.getDefinitionJson()));
                    ps.setString(4, nullToEmpty(version.getContentHash()));
                    ps.setString(5, nullToEmpty(version.getStatus()));
                    ps.setString(6, nullToEmpty(version.getCreatedBy()));
                    ps.setTimestamp(7, ts(version.getCreatedAt() == null ? LocalDateTime.now() : version.getCreatedAt()));
                    ps.setTimestamp(8, ts(version.getPublishedAt()));
                    ps.setString(9, nullToEmpty(version.getPublishedBy()));
                });
        version.setId(id);
        return version;
    }

    @Override
    public void updateWorkflowVersion(WorkflowVersion version) {
        jdbc.update(
                "UPDATE aop_workflow_version SET definition_json=?, content_hash=?, status=?, published_at=?, "
                        + "published_by=? WHERE id=?",
                version.getDefinitionJson(), version.getContentHash(), version.getStatus(),
                ts(version.getPublishedAt()), nullToEmpty(version.getPublishedBy()), version.getId());
    }

    @Override
    public Optional<WorkflowVersion> findWorkflowVersion(long id) {
        List<WorkflowVersion> list = jdbc.query("SELECT * FROM aop_workflow_version WHERE id=?",
                mapper(WorkflowVersion.class), id);
        return list.stream().findFirst();
    }

    @Override
    public Optional<WorkflowVersion> findLatestPublishedWorkflowVersion(long workflowId) {
        List<WorkflowVersion> list = jdbc.query(
                "SELECT * FROM aop_workflow_version WHERE workflow_id=? AND status='published' "
                        + "ORDER BY version DESC LIMIT 1",
                mapper(WorkflowVersion.class), workflowId);
        return list.stream().findFirst();
    }

    @Override
    public List<WorkflowVersion> listWorkflowVersions(long workflowId) {
        return jdbc.query(
                "SELECT * FROM aop_workflow_version WHERE workflow_id=? ORDER BY version DESC",
                mapper(WorkflowVersion.class), workflowId);
    }

    // ---------- chat（Mapper XML → aop_session / aop_execution / aop_session_message / aop_session_event） ----------

    @Override
    public ChatSession insertChatSession(ChatSession session) {
        // insertSession XML：useGeneratedKeys + keyProperty=id，回填自增主键
        chatSessionMapper.insertSession(session);
        return session;
    }

    @Override
    public void updateChatSession(ChatSession session) {
        chatSessionMapper.updateSession(session);
    }

    @Override
    public Optional<ChatSession> findChatSession(long id) {
        return Optional.ofNullable(chatSessionMapper.selectSessionById(id));
    }

    @Override
    public Optional<ChatSession> findChatSessionByPublicId(String publicId) {
        return Optional.ofNullable(chatSessionMapper.selectByPublicId(publicId));
    }

    @Override
    public Optional<ChatSession> findChatSessionByAgentkId(String agentkSessionId) {
        if (agentkSessionId == null) {
            return Optional.empty();
        }
        return Optional.ofNullable(chatSessionMapper.selectByAgentkSessionId(agentkSessionId));
    }

    @Override
    public List<ChatSession> listChatSessionsByUser(String userId, int limit) {
        return chatSessionMapper.listByUserId(userId, Math.max(limit, 1));
    }

    @Override
    public ChatRun insertChatRun(ChatRun run) {
        // insertExecution XML：useGeneratedKeys + keyProperty=id，回填自增主键
        chatRunMapper.insertExecution(run);
        return run;
    }

    @Override
    public void updateChatRun(ChatRun run) {
        chatRunMapper.updateExecution(run);
    }

    @Override
    public Optional<ChatRun> findChatRun(long id) {
        return Optional.ofNullable(chatRunMapper.selectExecutionById(id));
    }

    @Override
    public Optional<ChatRun> findChatRunByAgentkId(String agentkRunId) {
        if (agentkRunId == null) {
            return Optional.empty();
        }
        return Optional.ofNullable(chatRunMapper.selectByAgentkExecutionId(agentkRunId));
    }

    @Override
    public ChatMessage insertChatMessage(ChatMessage message) {
        // insertMessage XML：useGeneratedKeys + keyProperty=id，回填自增主键
        chatMessageMapper.insertMessage(message);
        return message;
    }

    @Override
    public void updateChatMessage(ChatMessage message) {
        chatMessageMapper.updateMessage(message);
    }

    @Override
    public List<ChatMessage> listChatMessages(long sessionId, int limit) {
        return chatMessageMapper.listBySessionId(sessionId, Math.max(limit, 1));
    }

    @Override
    public Optional<ChatMessage> findChatMessageBySessionAndItemSeq(long sessionId, int agentkItemSeq) {
        return Optional.ofNullable(chatMessageMapper.selectBySessionAndEventSeq(sessionId, agentkItemSeq));
    }

    @Override
    public ChatJob insertChatJob(ChatJob job) {
        long id = insertAndReturnId(
                "INSERT INTO aop_chat_job (session_id, run_id, agentk_job_id, tool_call_id, kind, status, "
                        + "wake_run_id, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)",
                ps -> {
                    ps.setLong(1, job.getSessionId());
                    if (job.getRunId() == null) {
                        ps.setObject(2, null);
                    } else {
                        ps.setLong(2, job.getRunId());
                    }
                    ps.setString(3, job.getAgentkJobId());
                    ps.setString(4, nullToEmpty(job.getToolCallId()));
                    ps.setString(5, nullToEmpty(job.getKind()));
                    ps.setString(6, nullToEmpty(job.getStatus()));
                    ps.setString(7, job.getWakeRunId());
                    ps.setTimestamp(8, ts(job.getCreatedAt() == null ? LocalDateTime.now() : job.getCreatedAt()));
                    ps.setTimestamp(9, ts(job.getUpdatedAt() == null ? LocalDateTime.now() : job.getUpdatedAt()));
                });
        job.setId(id);
        return job;
    }

    @Override
    public void updateChatJob(ChatJob job) {
        jdbc.update(
                "UPDATE aop_chat_job SET run_id=?, agentk_job_id=?, tool_call_id=?, kind=?, status=?, wake_run_id=?, "
                        + "updated_at=? WHERE id=?",
                job.getRunId(), job.getAgentkJobId(), job.getToolCallId(), job.getKind(), job.getStatus(),
                job.getWakeRunId(), ts(job.getUpdatedAt() == null ? LocalDateTime.now() : job.getUpdatedAt()),
                job.getId());
    }

    @Override
    public List<ChatJob> listOpenChatJobs(long sessionId) {
        return jdbc.query(
                "SELECT * FROM aop_chat_job WHERE session_id=? AND status='submitted'",
                mapper(ChatJob.class), sessionId);
    }

    // ---------- dataset ----------

    @Override
    public Dataset insertDataset(Dataset dataset) {
        long id = insertAndReturnId(
                "INSERT INTO aop_dataset (name, description, owner_user_id, tenant_id, key_column, "
                        + "input_mapping_json, latest_snapshot_version, status, created_at, updated_at) "
                        + "VALUES (?,?,?,?,?,?,?,?,?,?)",
                ps -> {
                    ps.setString(1, nullToEmpty(dataset.getName()));
                    ps.setString(2, nullToEmpty(dataset.getDescription()));
                    ps.setString(3, nullToEmpty(dataset.getOwnerUserId()));
                    ps.setString(4, nullToEmpty(dataset.getTenantId()));
                    ps.setString(5, nullToEmpty(dataset.getKeyColumn()));
                    ps.setString(6, dataset.getInputMappingJson());
                    ps.setInt(7, dataset.getLatestSnapshotVersion());
                    ps.setString(8, nullToEmpty(dataset.getStatus()));
                    ps.setTimestamp(9, ts(dataset.getCreatedAt() == null ? LocalDateTime.now() : dataset.getCreatedAt()));
                    ps.setTimestamp(10, ts(dataset.getUpdatedAt() == null ? LocalDateTime.now() : dataset.getUpdatedAt()));
                });
        dataset.setId(id);
        return dataset;
    }

    @Override
    public void updateDataset(Dataset dataset) {
        jdbc.update(
                "UPDATE aop_dataset SET name=?, description=?, owner_user_id=?, tenant_id=?, key_column=?, "
                        + "input_mapping_json=?, latest_snapshot_version=?, status=?, updated_at=? WHERE id=?",
                dataset.getName(), dataset.getDescription(), dataset.getOwnerUserId(), dataset.getTenantId(),
                dataset.getKeyColumn(), dataset.getInputMappingJson(), dataset.getLatestSnapshotVersion(),
                dataset.getStatus(),
                ts(dataset.getUpdatedAt() == null ? LocalDateTime.now() : dataset.getUpdatedAt()),
                dataset.getId());
    }

    @Override
    public Optional<Dataset> findDataset(long id) {
        List<Dataset> list = jdbc.query("SELECT * FROM aop_dataset WHERE id=?", mapper(Dataset.class), id);
        return list.stream().findFirst();
    }

    @Override
    public List<Dataset> listDatasetsByOwner(String userId) {
        return jdbc.query("SELECT * FROM aop_dataset WHERE owner_user_id=? ORDER BY id DESC",
                mapper(Dataset.class), userId);
    }

    @Override
    public DatasetSnapshot insertDatasetSnapshot(DatasetSnapshot snapshot) {
        long id = insertAndReturnId(
                "INSERT INTO aop_dataset_snapshot (dataset_id, version, columns_json, row_count, schema_json, "
                        + "checksum_sha256, source_uri, snapshot_uri, preview_json, created_by, created_at) "
                        + "VALUES (?,?,?,?,?,?,?,?,?,?,?)",
                ps -> {
                    ps.setLong(1, snapshot.getDatasetId());
                    ps.setInt(2, snapshot.getVersion());
                    ps.setString(3, nullToEmpty(snapshot.getColumnsJson()));
                    ps.setInt(4, snapshot.getRowCount());
                    ps.setString(5, snapshot.getSchemaJson());
                    ps.setString(6, nullToEmpty(snapshot.getChecksumSha256()));
                    ps.setString(7, nullToEmpty(snapshot.getSourceUri()));
                    ps.setString(8, nullToEmpty(snapshot.getSnapshotUri()));
                    ps.setString(9, snapshot.getPreviewJson());
                    ps.setString(10, nullToEmpty(snapshot.getCreatedBy()));
                    ps.setTimestamp(11, ts(snapshot.getCreatedAt() == null ? LocalDateTime.now() : snapshot.getCreatedAt()));
                });
        snapshot.setId(id);
        return snapshot;
    }

    @Override
    public Optional<DatasetSnapshot> findDatasetSnapshot(long id) {
        List<DatasetSnapshot> list = jdbc.query("SELECT * FROM aop_dataset_snapshot WHERE id=?",
                mapper(DatasetSnapshot.class), id);
        return list.stream().findFirst();
    }

    @Override
    public List<DatasetSnapshot> listDatasetSnapshots(long datasetId) {
        return jdbc.query(
                "SELECT * FROM aop_dataset_snapshot WHERE dataset_id=? ORDER BY version DESC",
                mapper(DatasetSnapshot.class), datasetId);
    }

    @Override
    public RunEvent insertRunEvent(RunEvent event) {
        runEventMapper.insertEvent(event);
        return event;
    }

    @Override
    public Optional<RunEvent> findRunEvent(String agentkRunId, int seq) {
        if (agentkRunId == null) {
            return Optional.empty();
        }
        return Optional.ofNullable(runEventMapper.selectByAgentkExecutionIdAndSeq(agentkRunId, seq));
    }

    private static String nullToEmpty(String s) {
        return s == null ? "" : s;
    }
}
