package com.kuaishou.agent.operation.backend.agentk;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKAppendResultDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKPostMessageRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKRegressionAgentTaskRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKRegressionAgentTaskResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKRegressionWorkflowTaskRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKRegressionWorkflowTaskResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKStartWorkflowRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKWorkflowExecDto;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;

/**
 * 在线回归编排：封装 agent-k 多步调用与轮询，统一返回平台 {@link Message}（不透传 agent-k 原始响应）。
 */
@Service
public class AgentKRegressionService {

    private static final String STATUS_COMPLETED = "completed";
    private static final String STATUS_FAILED = "failed";
    private static final String STATUS_RUNNING = "running";

    private final AgentKClient agentKClient;
    private final AgentKRegressionChecker regressionChecker;
    private final AgentKRegressionProperties regressionProperties;
    private final ObjectMapper objectMapper;
    private final AgentKCapabilityCacheService capabilityCacheService;

    public AgentKRegressionService(AgentKClient agentKClient, AgentKRegressionChecker regressionChecker,
            AgentKRegressionProperties regressionProperties, ObjectMapper objectMapper,
            AgentKCapabilityCacheService capabilityCacheService) {
        this.agentKClient = agentKClient;
        this.regressionChecker = regressionChecker;
        this.regressionProperties = regressionProperties;
        this.objectMapper = objectMapper;
        this.capabilityCacheService = capabilityCacheService;
    }

    public Message<AgentKRegressionAgentTaskResponse> runAgentTask(AgentKRegressionAgentTaskRequest request,
            AgentKOutboundIdentity identity) {
        long started = System.currentTimeMillis();
        AgentKRegressionAgentTaskResponse response = new AgentKRegressionAgentTaskResponse();

        if (request == null || !StringUtils.hasText(request.getPrompt())) {
            return Message.error(ResultCode.BAD_REQUEST, "prompt is required");
        }

        AgentKSessionDto session = createSession(request, identity);
        if (session == null) {
            return Message.error(ResultCode.RELY_SERVICE_BUSY, "failed to create session");
        }
        response.setSessionId(session.getId());

        AgentKPostMessageRequest message = new AgentKPostMessageRequest();
        message.setText(request.getPrompt());
        AgentKAppendResultDto append = postMessage(session.getId(), message, identity);
        if (append == null) {
            response.setError("failed to post message");
            response.setDurationMs(System.currentTimeMillis() - started);
            return Message.error(ResultCode.RELY_SERVICE_BUSY, "failed to post message", response);
        }

        long timeoutMs = resolveTimeout(request.getTimeoutMs(), regressionProperties.getAgentTimeoutMs());
        long pollMs = resolvePollInterval(request.getPollIntervalMs());
        List<AgentKListEventsResponse.AgentKEventDto> events =
                pollSessionUntilTerminal(session.getId(), timeoutMs, pollMs, identity,
                        Boolean.TRUE.equals(request.getWaitForJobs()));
        if (events == null) {
            response.setError("agent turn timeout after " + timeoutMs + "ms");
            response.setDurationMs(System.currentTimeMillis() - started);
            return Message.ok(response);
        }
        response.setEvents(events);
        String status = AgentKRegressionChecker.inferTurnStatus(events);
        if (Boolean.TRUE.equals(request.getWaitForJobs()) && hasPendingJobs(events)) {
            status = STATUS_RUNNING;
            response.setError(appendError(response.getError(), "pending jobs still open after timeout"));
        }
        response.setStatus(status);
        response.setCheck(regressionChecker.runChecks(request.getChecks(), status, events));
        response.setDurationMs(System.currentTimeMillis() - started);
        return Message.ok(response);
    }

    public Message<AgentKRegressionWorkflowTaskResponse> runWorkflowTask(AgentKRegressionWorkflowTaskRequest request,
            AgentKOutboundIdentity identity) {
        long started = System.currentTimeMillis();
        AgentKRegressionWorkflowTaskResponse response = new AgentKRegressionWorkflowTaskResponse();

        if (request == null || !StringUtils.hasText(request.getPipeline())) {
            return Message.error(ResultCode.BAD_REQUEST, "pipeline is required");
        }

        AgentKSessionDto session = exchangeSession(
                StringUtils.hasText(request.getTitle()) ? request.getTitle() : "pipeline-regression",
                request.getProfile(),
                identity);
        if (session == null) {
            return Message.error(ResultCode.RELY_SERVICE_BUSY, "failed to create session");
        }
        response.setSessionId(session.getId());

        AgentKStartWorkflowRequest start = new AgentKStartWorkflowRequest();
        start.setPipeline(request.getPipeline());
        start.setInputs(request.getInputs());
        AgentKWorkflowExecDto exec = startPipeline(session.getId(), start, identity);
        if (exec == null) {
            response.setError("failed to start pipeline");
            response.setDurationMs(System.currentTimeMillis() - started);
            return Message.error(ResultCode.RELY_SERVICE_BUSY, "failed to start pipeline", response);
        }
        response.setExecId(exec.getId());

        long timeoutMs = resolveTimeout(request.getTimeoutMs(), regressionProperties.getWorkflowTimeoutMs());
        long pollMs = resolvePollInterval(request.getPollIntervalMs());
        AgentKWorkflowExecDto terminal = pollPipelineUntilTerminal(session.getId(), exec.getId(), timeoutMs, pollMs,
                identity);
        if (terminal == null) {
            response.setError("pipeline exec timeout after " + timeoutMs + "ms");
            response.setDurationMs(System.currentTimeMillis() - started);
            return Message.ok(response);
        }
        response.setStatus(terminal.getStatus());
        response.setExec(terminal);
        response.setDurationMs(System.currentTimeMillis() - started);
        return Message.ok(response);
    }

    private AgentKSessionDto createSession(AgentKRegressionAgentTaskRequest request, AgentKOutboundIdentity identity) {
        return exchangeSession(
                StringUtils.hasText(request.getTitle()) ? request.getTitle() : "agent-regression",
                request.getProfile(),
                identity);
    }

    private AgentKSessionDto exchangeSession(String title, JsonNode profile, AgentKOutboundIdentity identity) {
        Map<String, Object> outbound = AgentKProfileOutbound.toCreateSessionBody(
                title, profile, objectMapper, capabilityCacheService);
        ResponseEntity<?> upstream = agentKClient.exchangeJson("POST", "/sessions", null, outbound,
                AgentKSessionDto.class, identity);
        if (!upstream.getStatusCode().is2xxSuccessful() || !(upstream.getBody() instanceof AgentKSessionDto)) {
            return null;
        }
        return (AgentKSessionDto) upstream.getBody();
    }

    private AgentKAppendResultDto postMessage(String sessionId, AgentKPostMessageRequest message,
            AgentKOutboundIdentity identity) {
        ResponseEntity<?> upstream = agentKClient.exchangeJson("POST", "/sessions/" + sessionId + "/messages", null,
                message, AgentKAppendResultDto.class, identity);
        if (!upstream.getStatusCode().is2xxSuccessful() || !(upstream.getBody() instanceof AgentKAppendResultDto)) {
            return null;
        }
        return (AgentKAppendResultDto) upstream.getBody();
    }

    private AgentKWorkflowExecDto startPipeline(String sessionId, AgentKStartWorkflowRequest start,
            AgentKOutboundIdentity identity) {
        ResponseEntity<?> upstream = agentKClient.exchangeJson("POST",
                "/sessions/" + sessionId + "/pipeline-runs", null, start, AgentKWorkflowExecDto.class, identity);
        if (!upstream.getStatusCode().is2xxSuccessful() || !(upstream.getBody() instanceof AgentKWorkflowExecDto)) {
            return null;
        }
        return (AgentKWorkflowExecDto) upstream.getBody();
    }

    /**
     * 轮询会话事件直到本轮终态：出现 round.completed / interrupted / error；
     * 若 waitForJobs，还要等所有 job.submitted 都有对应 completed/failed。
     */
    private List<AgentKListEventsResponse.AgentKEventDto> pollSessionUntilTerminal(String sessionId, long timeoutMs,
            long pollMs, AgentKOutboundIdentity identity, boolean waitForJobs) {
        long deadline = System.currentTimeMillis() + timeoutMs;
        long after = 0L;
        List<AgentKListEventsResponse.AgentKEventDto> all = new ArrayList<>();
        while (System.currentTimeMillis() < deadline) {
            String query = AgentKPaging.eventsQuery(after, null);
            ResponseEntity<?> upstream = agentKClient.exchangeJson("GET", "/sessions/" + sessionId + "/events",
                    query, null, AgentKListEventsResponse.class, identity);
            if (upstream.getStatusCode().is2xxSuccessful() && upstream.getBody() instanceof AgentKListEventsResponse) {
                AgentKListEventsResponse body = (AgentKListEventsResponse) upstream.getBody();
                if (body.getEvents() != null) {
                    for (AgentKListEventsResponse.AgentKEventDto event : body.getEvents()) {
                        all.add(event);
                        if (event.getSeq() != null) {
                            after = Math.max(after, event.getSeq());
                        }
                    }
                }
                String status = AgentKRegressionChecker.inferTurnStatus(all);
                boolean turnDone = STATUS_COMPLETED.equals(status) || STATUS_FAILED.equals(status);
                boolean jobsDone = !waitForJobs || !hasPendingJobs(all);
                if (turnDone && jobsDone) {
                    return all;
                }
            }
            sleep(pollMs);
        }
        return null;
    }

    private static boolean hasPendingJobs(List<AgentKListEventsResponse.AgentKEventDto> events) {
        Set<String> submitted = new HashSet<>();
        Set<String> finished = new HashSet<>();
        for (AgentKListEventsResponse.AgentKEventDto event : events) {
            if (event.getData() == null) {
                continue;
            }
            String jobId = event.getData().path("jobId").asText(null);
            if (!StringUtils.hasText(jobId)) {
                continue;
            }
            if ("job.submitted".equals(event.getKind())) {
                submitted.add(jobId);
            } else if ("job.completed".equals(event.getKind()) || "job.failed".equals(event.getKind())) {
                finished.add(jobId);
            }
        }
        for (String jobId : submitted) {
            if (!finished.contains(jobId)) {
                return true;
            }
        }
        return false;
    }

    private AgentKWorkflowExecDto pollPipelineUntilTerminal(String sessionId, String execId, long timeoutMs,
            long pollMs, AgentKOutboundIdentity identity) {
        long deadline = System.currentTimeMillis() + timeoutMs;
        while (System.currentTimeMillis() < deadline) {
            ResponseEntity<?> upstream = agentKClient.exchangeJson("GET",
                    "/sessions/" + sessionId + "/pipeline-runs/" + execId, null, null,
                    AgentKWorkflowExecDto.class, identity);
            if (upstream.getStatusCode().is2xxSuccessful() && upstream.getBody() instanceof AgentKWorkflowExecDto) {
                AgentKWorkflowExecDto exec = (AgentKWorkflowExecDto) upstream.getBody();
                if (STATUS_COMPLETED.equals(exec.getStatus()) || STATUS_FAILED.equals(exec.getStatus())) {
                    return exec;
                }
            }
            sleep(pollMs);
        }
        return null;
    }

    private static long resolveTimeout(Long requestTimeout, long defaultTimeout) {
        return requestTimeout != null && requestTimeout > 0 ? requestTimeout : defaultTimeout;
    }

    private long resolvePollInterval(Long requestPoll) {
        return requestPoll != null && requestPoll > 0 ? requestPoll : regressionProperties.getPollIntervalMs();
    }

    private static void sleep(long pollMs) {
        try {
            Thread.sleep(pollMs);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }

    private static String appendError(String existing, String extra) {
        if (!StringUtils.hasText(existing)) {
            return extra;
        }
        return existing + "; " + extra;
    }
}
