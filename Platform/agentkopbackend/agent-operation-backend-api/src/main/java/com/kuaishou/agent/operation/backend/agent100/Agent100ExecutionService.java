package com.kuaishou.agent.operation.backend.agent100;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.AgentKClient;
import com.kuaishou.agent.operation.backend.agentk.AgentKCreateSessionService;
import com.kuaishou.agent.operation.backend.agentk.AgentKIdentityResolver;
import com.kuaishou.agent.operation.backend.agentk.AgentKMessageMapper;
import com.kuaishou.agent.operation.backend.agentk.AgentKOutboundIdentity;
import com.kuaishou.agent.operation.backend.agentk.AgentKPaging;
import com.kuaishou.agent.operation.backend.agentk.ChatAssetSyncService;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCreateSessionRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse.AgentKEventDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionProfileDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionProfileDto.PipelineEntry;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKStartWorkflowRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKWorkflowExecDto;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.agent100.Agent100Constants;
import com.kuaishou.agent.operation.backend.platform.agent100.Agent100Definition;
import com.kuaishou.agent.operation.backend.platform.agent100.Agent100PipelineDef;
import com.kuaishou.agent.operation.backend.platform.agent100.Agent100Registry;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100AgentView;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100CatalogView;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100ExecutionStartRequest;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100ExecutionView;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100ExecutionView.Agent100EventView;
import com.kuaishou.agent.operation.backend.platform.agent100.dto.Agent100HistoryView;
import com.kuaishou.agent.operation.backend.platform.agent100.narrato.NarratoArgsNormalizer;
import com.kuaishou.agent.operation.backend.platform.agent100.narrato.NarratoDefinition;
import com.kuaishou.agent.operation.backend.platform.entity.PipelineExec;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService;
import com.kuaishou.agent.operation.backend.platform.service.PlatformAdminService;
import com.kuaishou.agent.operation.backend.platform.support.ChatSessionKinds;
import com.kuaishou.agent.operation.backend.platform.support.PipelineOnlySessionProfile;

/**
 * Agent100 执行编排：按注册表 {@code agentId} 工作，复用 CreateSession / pipeline-runs / events / cancel。
 */
@Service
public class Agent100ExecutionService {

    private static final Logger LOGGER = LoggerFactory.getLogger(Agent100ExecutionService.class);
    private static final int HISTORY_LIMIT = 50;

    private final AgentKCreateSessionService createSessionService;
    private final AgentKClient agentKClient;
    private final AgentKIdentityResolver identityResolver;
    private final AgentKMessageMapper messageMapper;
    private final ChatAssetSyncService chatAssetSyncService;
    private final ChatPersistService chatPersistService;
    private final PlatformAdminService platformAdminService;
    private final ObjectMapper objectMapper;

    public Agent100ExecutionService(AgentKCreateSessionService createSessionService,
            AgentKClient agentKClient,
            AgentKIdentityResolver identityResolver,
            AgentKMessageMapper messageMapper,
            ChatAssetSyncService chatAssetSyncService,
            ChatPersistService chatPersistService,
            PlatformAdminService platformAdminService,
            ObjectMapper objectMapper) {
        this.createSessionService = createSessionService;
        this.agentKClient = agentKClient;
        this.identityResolver = identityResolver;
        this.messageMapper = messageMapper;
        this.chatAssetSyncService = chatAssetSyncService;
        this.chatPersistService = chatPersistService;
        this.platformAdminService = platformAdminService;
        this.objectMapper = objectMapper;
    }

    public Message<Agent100CatalogView> catalog() {
        Agent100CatalogView view = new Agent100CatalogView();
        List<Agent100AgentView> agents = new ArrayList<>();
        for (Agent100Definition agent : Agent100Registry.all()) {
            Agent100AgentView item = new Agent100AgentView();
            item.setId(agent.getId());
            item.setName(agent.getName());
            item.setIcon(agent.getIcon());
            item.setAccent(agent.getAccent());
            item.setDescription(agent.getDescription());
            if (NarratoDefinition.AGENT_ID.equals(agent.getId())) {
                item.setContractVersion(NarratoDefinition.CONTRACT_VERSION);
            }
            agents.add(item);
        }
        view.setAgents(agents);
        view.setCount(agents.size());
        return Message.ok(view);
    }

    public Message<Agent100ExecutionView> start(String agentId, Agent100ExecutionStartRequest request) {
        Agent100Definition agent;
        try {
            agent = Agent100Registry.require(agentId);
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.NOT_FOUND, e.getMessage());
        }
        if (request == null || !StringUtils.hasText(request.getPipeline())) {
            return Message.error(ResultCode.PARAM_REQUIRED, "pipeline is required");
        }
        Agent100PipelineDef pipeline = agent.findPipeline(request.getPipeline());
        if (pipeline == null) {
            return Message.error(ResultCode.BAD_REQUEST,
                    "unknown pipeline " + request.getPipeline().trim()
                            + "; choose one of: " + agent.pipelineNames());
        }

        JsonNode args;
        try {
            args = normalizeArgs(agent, pipeline, argsOrEmpty(request));
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, e.getMessage());
        }

        String sessionId = StringUtils.hasText(request.getSessionId())
                ? request.getSessionId().trim() : null;
        if (!StringUtils.hasText(sessionId)) {
            Message<AgentKSessionDto> created = createSession(agent, request.getTitle());
            if (created.getCode() != ResultCode.SUCCESS.getCode()
                    || created.getData() == null
                    || !StringUtils.hasText(created.getData().getId())) {
                return Message.error(ResultCode.RELY_SERVICE_BUSY,
                        StringUtils.hasText(created.getMessage())
                                ? created.getMessage()
                                : "create session failed");
            }
            sessionId = created.getData().getId().trim();
        }

        AgentKOutboundIdentity identity = identityResolver.resolve();
        AgentKStartWorkflowRequest start = new AgentKStartWorkflowRequest();
        start.setPipeline(pipeline.getName());
        start.setArgs(args);
        ResponseEntity<?> upstream = agentKClient.exchangeJson("POST",
                "/sessions/" + sessionId + "/pipeline-runs", null, start,
                AgentKWorkflowExecDto.class, identity);
        Message<AgentKWorkflowExecDto> execMessage = messageMapper.toMessage(upstream, AgentKWorkflowExecDto.class);
        if (execMessage.getCode() != ResultCode.SUCCESS.getCode() || execMessage.getData() == null) {
            LOGGER.warn("agent100 start failed agentId={} pipeline={} sessionId={} code={} msg={}",
                    agent.getId(), pipeline.getName(), sessionId, execMessage.getCode(), execMessage.getMessage());
            return AgentKMessageMapper.keepMappedError(execMessage, "start pipeline execution failed");
        }

        AgentKWorkflowExecDto run = execMessage.getData();
        fillPipelineRunFromRequest(run, start);
        if (!StringUtils.hasText(run.getCapabilityId())) {
            run.setCapabilityId(pipeline.getCapability());
        }
        chatAssetSyncService.enrichPipelineRun(run);
        chatAssetSyncService.syncPipelineExecAndAssets(sessionId, run);

        Agent100ExecutionView view = new Agent100ExecutionView();
        view.setAgentId(agent.getId());
        view.setSessionId(sessionId);
        view.setExecutionId(run.getId());
        view.setPipeline(pipeline.getName());
        view.setCapabilityId(pipeline.getCapability());
        view.setStatus(run.getStatus());
        view.setRequest(objectMapper.valueToTree(requestBodySnapshot(agent, pipeline.getName(), request, args)));
        return Message.ok(view);
    }

    /**
     * 历史列表：普通用户只看自己；超管不传 {@code userId} 看全部，传则看指定用户。
     */
    public Message<Agent100HistoryView> listHistory(String agentId, String requestedUserId) {
        Agent100Definition agent;
        try {
            agent = Agent100Registry.require(agentId);
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.NOT_FOUND, e.getMessage());
        }
        String ownerFilter;
        try {
            ownerFilter = resolveHistoryOwnerFilter(requestedUserId);
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.FORBIDDEN, e.getMessage());
        }
        List<PipelineExec> rows = chatPersistService.listPipelineExecsByPipelineNames(
                agent.pipelineNames(), ownerFilter, HISTORY_LIMIT);
        Agent100HistoryView view = new Agent100HistoryView();
        view.setAgentId(agent.getId());
        List<Agent100HistoryView.Item> items = new ArrayList<>();
        for (PipelineExec row : rows) {
            if (row == null) {
                continue;
            }
            Agent100HistoryView.Item item = new Agent100HistoryView.Item();
            item.setUserId(row.getUserId());
            item.setSessionId(row.getAgentkSessionId());
            item.setExecutionId(row.getAgentkExecId());
            item.setPipeline(row.getPipelineName());
            item.setTitle(agent.getDefaultTitle());
            item.setStatus(row.getStatus());
            item.setError(row.getError());
            item.setHasOutput(StringUtils.hasText(row.getOutputJson()));
            item.setCreatedAt(row.getCreatedAt());
            item.setUpdatedAt(row.getUpdatedAt());
            items.add(item);
        }
        view.setItems(items);
        view.setCount(items.size());
        return Message.ok(view);
    }

    /**
     * 普通用户固定看自己；超管 {@code requestedUserId} 空=全部，非空=指定用户。
     */
    private String resolveHistoryOwnerFilter(String requestedUserId) {
        String self = identityResolver.resolve().getUserId();
        if (!StringUtils.hasText(self)) {
            throw new IllegalArgumentException("login required");
        }
        if (platformAdminService.isSuperAdmin(self)) {
            return StringUtils.hasText(requestedUserId) ? requestedUserId.trim() : null;
        }
        if (StringUtils.hasText(requestedUserId) && !self.equals(requestedUserId.trim())) {
            throw new IllegalArgumentException("cannot view other users");
        }
        return self;
    }

    public Message<Agent100ExecutionView> getExecution(String agentId, String sessionId, String executionId) {
        Agent100Definition agent;
        try {
            agent = Agent100Registry.require(agentId);
        } catch (IllegalArgumentException e) {
            return Message.error(ResultCode.NOT_FOUND, e.getMessage());
        }
        if (!StringUtils.hasText(sessionId) || !StringUtils.hasText(executionId)) {
            return Message.error(ResultCode.PARAM_REQUIRED, "sessionId and executionId are required");
        }
        String sid = sessionId.trim();
        String eid = executionId.trim();
        AgentKOutboundIdentity identity = identityResolver.resolve();

        try {
            ResponseEntity<?> upstream = agentKClient.exchangeJson("GET",
                    "/sessions/" + sid + "/pipeline-runs/" + eid, null, null,
                    AgentKWorkflowExecDto.class, identity);
            Message<AgentKWorkflowExecDto> execMessage =
                    messageMapper.toMessage(upstream, AgentKWorkflowExecDto.class);
            if (execMessage.getCode() != ResultCode.SUCCESS.getCode() || execMessage.getData() == null) {
                return fallbackCached(agent.getId(), sid, eid,
                        StringUtils.hasText(execMessage.getMessage())
                                ? execMessage.getMessage()
                                : "get pipeline execution failed");
            }
            AgentKWorkflowExecDto run = execMessage.getData();
            chatAssetSyncService.enrichPipelineRun(run);
            chatAssetSyncService.syncPipelineExecAndAssets(sid, run);

            Agent100ExecutionView view = new Agent100ExecutionView();
            view.setAgentId(agent.getId());
            view.setSessionId(sid);
            view.setExecutionId(eid);
            view.setPipeline(run.getName());
            view.setCapabilityId(run.getCapabilityId());
            view.setStatus(run.getStatus());
            view.setError(run.getError());
            view.setOutput(run.getOutput());
            view.setEvents(listEventsForExecution(sid, eid, identity));
            PipelineExec cached = findCached(sid, eid);
            if (cached != null && StringUtils.hasText(cached.getArgsJson())) {
                view.setRequest(parseJson(cached.getArgsJson()));
            }
            return Message.ok(view);
        } catch (Exception e) {
            LOGGER.warn("agent100 get execution failed agentId={} sessionId={} executionId={}",
                    agent.getId(), sid, eid, e);
            return fallbackCached(agent.getId(), sid, eid,
                    e.getClass().getSimpleName() + ": AgentK request failed");
        }
    }

    private Message<AgentKSessionDto> createSession(Agent100Definition agent, String title) {
        List<PipelineEntry> pipelines = new ArrayList<>();
        for (Agent100PipelineDef pipeline : agent.getPipelines()) {
            PipelineEntry entry = new PipelineEntry();
            entry.setCapability(pipeline.getCapability());
            entry.setName(pipeline.getName());
            entry.setDisplayName(pipeline.getDisplayName());
            pipelines.add(entry);
        }
        AgentKSessionProfileDto profile = PipelineOnlySessionProfile.buildWithPipelines(pipelines);
        profile.setDefaultModel(agent.getDefaultModel());

        AgentKCreateSessionRequest sessionRequest = new AgentKCreateSessionRequest();
        sessionRequest.setTitle(StringUtils.hasText(title) ? title.trim() : agent.getDefaultTitle());
        sessionRequest.setProfile(objectMapper.valueToTree(profile));
        sessionRequest.setSessionKind(ChatSessionKinds.WORKFLOW);
        sessionRequest.setEntrySource(Agent100Constants.ENTRY_SOURCE);
        return createSessionService.createSession(sessionRequest);
    }

    private List<Agent100EventView> listEventsForExecution(String sessionId, String executionId,
            AgentKOutboundIdentity identity) {
        List<Agent100EventView> events = new ArrayList<>();
        ResponseEntity<?> upstream = agentKClient.exchangeJson("GET",
                "/sessions/" + sessionId + "/events", AgentKPaging.allEventsQuery(), null,
                AgentKListEventsResponse.class, identity);
        Message<AgentKListEventsResponse> message =
                messageMapper.toMessage(upstream, AgentKListEventsResponse.class);
        if (message.getCode() != ResultCode.SUCCESS.getCode() || message.getData() == null
                || message.getData().getEvents() == null) {
            return events;
        }
        for (AgentKEventDto event : message.getData().getEvents()) {
            if (event == null || !executionId.equals(event.getExecutionId())) {
                continue;
            }
            events.add(toEventView(event));
        }
        return events;
    }

    /**
     * 对齐 Demo {@code _event_to_dict}；kind 使用平台 wire（点号），兼容 protobuf oneof 下划线名。
     */
    static Agent100EventView toEventView(AgentKEventDto event) {
        Agent100EventView view = new Agent100EventView();
        view.setId(event.getEventId());
        String kind = event.getKind() == null ? "" : event.getKind().trim();
        JsonNode data = event.getData();
        if ("invocation.progress".equals(kind)
                || "invocation_progress".equals(kind)
                || "progress".equalsIgnoreCase(kind)) {
            view.setType("progress");
            if (data != null) {
                view.setStep(text(data, "stepId", "step_id", "step"));
                view.setMessage(text(data, "message"));
                view.setData(data.has("data") ? data.get("data") : data);
            }
            return view;
        }
        if ("execution.finished".equals(kind)
                || "execution_finished".equals(kind)
                || "finished".equalsIgnoreCase(kind)) {
            view.setType("finished");
            if (data != null) {
                view.setStatus(text(data, "status"));
                view.setError(text(data, "error"));
            }
            return view;
        }
        view.setType(StringUtils.hasText(kind) ? kind : "event");
        view.setData(data);
        return view;
    }

    private Message<Agent100ExecutionView> fallbackCached(String agentId, String sessionId,
            String executionId, String syncError) {
        PipelineExec cached = findCached(sessionId, executionId);
        if (cached == null) {
            return Message.error(ResultCode.NOT_FOUND, syncError);
        }
        Agent100ExecutionView view = new Agent100ExecutionView();
        view.setAgentId(agentId);
        view.setSessionId(sessionId);
        view.setExecutionId(executionId);
        view.setPipeline(cached.getPipelineName());
        view.setCapabilityId(cached.getCapabilityId());
        view.setStatus(cached.getStatus());
        view.setError(cached.getError());
        view.setOutput(parseJson(cached.getOutputJson()));
        view.setRequest(parseJson(cached.getArgsJson()));
        view.setCached(true);
        view.setStatusSyncError(syncError);
        return Message.ok(view);
    }

    private PipelineExec findCached(String sessionId, String executionId) {
        List<PipelineExec> rows = chatPersistService.listPipelineExecsByAgentkSessionId(sessionId, HISTORY_LIMIT);
        for (PipelineExec row : rows) {
            if (row != null && executionId.equals(row.getAgentkExecId())) {
                return row;
            }
        }
        return null;
    }

    private JsonNode argsOrEmpty(Agent100ExecutionStartRequest request) {
        if (request == null || request.getArgs() == null || request.getArgs().isNull()
                || request.getArgs().isMissingNode()) {
            return objectMapper.createObjectNode();
        }
        return request.getArgs();
    }

    private static JsonNode normalizeArgs(Agent100Definition agent, Agent100PipelineDef pipeline,
            JsonNode args) {
        if (NarratoDefinition.AGENT_ID.equals(agent.getId())) {
            return NarratoArgsNormalizer.normalize(pipeline.getName(), args);
        }
        return args;
    }

    private Object requestBodySnapshot(Agent100Definition agent, String pipeline,
            Agent100ExecutionStartRequest request, JsonNode args) {
        Map<String, Object> body = new LinkedHashMap<>();
        body.put("pipeline", pipeline);
        body.put("args", args);
        body.put("title", StringUtils.hasText(request.getTitle())
                ? request.getTitle().trim() : agent.getDefaultTitle());
        if (StringUtils.hasText(request.getSessionId())) {
            body.put("sessionId", request.getSessionId().trim());
        }
        return body;
    }

    private JsonNode parseJson(String raw) {
        if (!StringUtils.hasText(raw)) {
            return null;
        }
        try {
            return objectMapper.readTree(raw);
        } catch (Exception e) {
            LOGGER.warn("agent100 json parse failed: {}", e.toString());
            return null;
        }
    }

    private static void fillPipelineRunFromRequest(AgentKWorkflowExecDto run, AgentKStartWorkflowRequest request) {
        if (run == null || request == null) {
            return;
        }
        if (run.getArgs() == null || run.getArgs().isNull() || run.getArgs().isMissingNode()) {
            run.setArgs(request.getArgs());
        }
        if (!StringUtils.hasText(run.getName()) && StringUtils.hasText(request.getPipeline())) {
            run.setName(request.getPipeline().trim());
        }
    }

    private static String text(JsonNode node, String... fields) {
        if (node == null || fields == null) {
            return null;
        }
        for (String field : fields) {
            if (node.hasNonNull(field) && StringUtils.hasText(node.get(field).asText())) {
                return node.get(field).asText();
            }
        }
        return null;
    }
}
