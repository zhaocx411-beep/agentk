package com.kuaishou.agent.operation.backend.platform.mapper;

import java.util.List;
import java.util.Optional;

import com.kuaishou.agent.operation.backend.platform.entity.ChatMessage;
import com.kuaishou.agent.operation.backend.platform.entity.ChatRun;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.entity.RunEvent;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.Agent;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.AgentkCallLog;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.ChatJob;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.Dataset;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.DatasetSnapshot;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.Workflow;
import com.kuaishou.agent.operation.backend.platform.model.PlatformModels.WorkflowVersion;

/**
 * 平台持久化 Mapper（MySQL 实现见 MysqlPlatformMapper）。
 */
public interface PlatformMapper {

    AgentkCallLog insertCallLog(AgentkCallLog log);

    void updateCallLog(AgentkCallLog log);

    Agent insertAgent(Agent agent);

    void updateAgent(Agent agent);

    Optional<Agent> findAgent(long id);

    List<Agent> listAgentsByOwner(String userId);

    AgentVersion insertAgentVersion(AgentVersion version);

    void updateAgentVersion(AgentVersion version);

    Optional<AgentVersion> findAgentVersion(long id);

    Optional<AgentVersion> findLatestPublishedAgentVersion(long agentId);

    List<AgentVersion> listAgentVersions(long agentId);

    Workflow insertWorkflow(Workflow workflow);

    void updateWorkflow(Workflow workflow);

    Optional<Workflow> findWorkflow(long id);

    List<Workflow> listWorkflowsByOwner(String userId);

    WorkflowVersion insertWorkflowVersion(WorkflowVersion version);

    void updateWorkflowVersion(WorkflowVersion version);

    Optional<WorkflowVersion> findWorkflowVersion(long id);

    Optional<WorkflowVersion> findLatestPublishedWorkflowVersion(long workflowId);

    List<WorkflowVersion> listWorkflowVersions(long workflowId);

    ChatSession insertChatSession(ChatSession session);

    void updateChatSession(ChatSession session);

    Optional<ChatSession> findChatSession(long id);

    Optional<ChatSession> findChatSessionByPublicId(String publicId);

    Optional<ChatSession> findChatSessionByAgentkId(String agentkSessionId);

    List<ChatSession> listChatSessionsByUser(String userId, int limit);

    ChatRun insertChatRun(ChatRun run);

    void updateChatRun(ChatRun run);

    Optional<ChatRun> findChatRun(long id);

    Optional<ChatRun> findChatRunByAgentkId(String agentkRunId);

    ChatMessage insertChatMessage(ChatMessage message);

    void updateChatMessage(ChatMessage message);

    List<ChatMessage> listChatMessages(long sessionId, int limit);

    Optional<ChatMessage> findChatMessageBySessionAndItemSeq(long sessionId, int agentkItemSeq);

    ChatJob insertChatJob(ChatJob job);

    void updateChatJob(ChatJob job);

    List<ChatJob> listOpenChatJobs(long sessionId);

    Dataset insertDataset(Dataset dataset);

    void updateDataset(Dataset dataset);

    Optional<Dataset> findDataset(long id);

    List<Dataset> listDatasetsByOwner(String userId);

    DatasetSnapshot insertDatasetSnapshot(DatasetSnapshot snapshot);

    Optional<DatasetSnapshot> findDatasetSnapshot(long id);

    List<DatasetSnapshot> listDatasetSnapshots(long datasetId);

    RunEvent insertRunEvent(RunEvent event);

    Optional<RunEvent> findRunEvent(String agentkRunId, int seq);
}
