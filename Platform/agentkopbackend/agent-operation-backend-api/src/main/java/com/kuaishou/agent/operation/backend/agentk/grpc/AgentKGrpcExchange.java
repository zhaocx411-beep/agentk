package com.kuaishou.agent.operation.backend.agentk.grpc;

import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.agentk.AgentKOutboundIdentity;
import com.kuaishou.agent.operation.backend.agentk.AgentKPaging;
import com.kuaishou.agent.operation.backend.agentk.AgentKProperties;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKAnswerRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKAppendResultDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKErrorDto;
import com.kuaishou.agent.operation.backend.agentk.dto.CapabilityInvokeOutputDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListSessionsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListWorkflowRunsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListWorkflowsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMeDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKPostMessageRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKProfileDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKStartWorkflowRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKToolsCatalogDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKWorkflowExecDto;
import com.kuaishou.agent.operation.backend.platform.service.BlobStoreOfficeUrlService;
import com.kuaishou.klingai.agentk.api.v1.AnswerInput;
import com.kuaishou.klingai.agentk.api.v1.AgentAnswersExecutionInput;
import com.kuaishou.klingai.agentk.api.v1.CancelExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.Capability;
import com.kuaishou.klingai.agentk.api.v1.CreateSessionRequest;
import com.kuaishou.klingai.agentk.api.v1.CreateSessionResponse;
import com.kuaishou.klingai.agentk.api.v1.Event;
import com.kuaishou.klingai.agentk.api.v1.GetExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.GetExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.InvokeCapabilityRequest;
import com.kuaishou.klingai.agentk.api.v1.InvokeCapabilityResponse;
import com.kuaishou.klingai.agentk.api.v1.ListCapabilitiesRequest;
import com.kuaishou.klingai.agentk.api.v1.ListCapabilitiesResponse;
import com.kuaishou.klingai.agentk.api.v1.ListEventsRequest;
import com.kuaishou.klingai.agentk.api.v1.ListEventsResponse;
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsRequest;
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsResponse;
import com.kuaishou.klingai.agentk.api.v1.ListPipelinesRequest;
import com.kuaishou.klingai.agentk.api.v1.ListPipelinesResponse;
import com.kuaishou.klingai.agentk.api.v1.ListSessionsRequest;
import com.kuaishou.klingai.agentk.api.v1.ListSessionsResponse;
import com.kuaishou.klingai.agentk.api.v1.PipelineExecutionInput;
import com.kuaishou.klingai.agentk.api.v1.Profile;
import com.kuaishou.klingai.agentk.api.v1.StartExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.StartExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.WhoAmIRequest;
import com.kuaishou.klingai.agentk.api.v1.WhoAmIResponse;
import com.google.protobuf.Struct;
import com.google.protobuf.Value;
import com.google.protobuf.util.JsonFormat;

/**
 * 将原有 {@code AgentKClient.exchangeJson} 路径映射到 gRPC SDK RPC。
 */
@Component
public class AgentKGrpcExchange {

    private static final Logger log = LoggerFactory.getLogger(AgentKGrpcExchange.class);

    private static final Pattern SESSION_MESSAGES = Pattern.compile("^/sessions/([^/]+)/messages$");
    private static final Pattern SESSION_ANSWERS = Pattern.compile("^/sessions/([^/]+)/answers$");
    private static final Pattern SESSION_CANCEL = Pattern.compile("^/sessions/([^/]+)/cancel$");
    private static final Pattern SESSION_EVENTS = Pattern.compile("^/sessions/([^/]+)/events$");
    private static final Pattern SESSION_PIPELINES = Pattern.compile("^/sessions/([^/]+)/pipelines$");
    private static final Pattern SESSION_PIPELINE_RUNS = Pattern.compile("^/sessions/([^/]+)/pipeline-runs$");
    private static final Pattern SESSION_PIPELINE_RUN = Pattern.compile("^/sessions/([^/]+)/pipeline-runs/([^/]+)$");

    private final AgentKSdkHolder sdkHolder;
    private final AgentKProperties properties;
    private final ObjectMapper objectMapper;
    private final BlobStoreOfficeUrlService officeUrlService;

    public AgentKGrpcExchange(AgentKSdkHolder sdkHolder, AgentKProperties properties, ObjectMapper objectMapper,
            BlobStoreOfficeUrlService officeUrlService) {
        this.sdkHolder = sdkHolder;
        this.properties = properties;
        this.objectMapper = objectMapper;
        this.officeUrlService = officeUrlService;
    }

  public <T> ResponseEntity<?> exchangeJson(String method, String upstreamPath, String query, Object requestBody,
            Class<T> successType, AgentKOutboundIdentity identity) {
        try {
            Object body = dispatch(method, upstreamPath, query, requestBody);
            if (body == null && Void.class.equals(successType)) {
                return ResponseEntity.status(HttpStatus.ACCEPTED).build();
            }
            if (body == null) {
                return ResponseEntity.ok().build();
            }
            if (successType.isInstance(body)) {
                HttpStatus status = resolveSuccessStatus(method, upstreamPath);
                return ResponseEntity.status(status).body(successType.cast(body));
            }
            AgentKErrorDto error = new AgentKErrorDto();
            error.setError("unexpected agent-k response type");
            return ResponseEntity.status(HttpStatus.INTERNAL_SERVER_ERROR).body(error);
        } catch (ConnectStyleException e) {
            AgentKErrorDto error = new AgentKErrorDto();
            error.setError(e.getMessage());
            return ResponseEntity.status(e.status).body(error);
        } catch (IllegalArgumentException e) {
            AgentKErrorDto error = new AgentKErrorDto();
            error.setError(e.getMessage());
            return ResponseEntity.status(HttpStatus.BAD_REQUEST).body(error);
        } catch (Exception e) {
            AgentKConnectErrors.Failure failure = AgentKConnectErrors.describe(e);
            log.warn("[AgentK] grpc {} {} grpcCode={} grpcMessage={} wrappedHttp={} cause={}",
                    method, upstreamPath, failure.grpcCode, failure.grpcMessage,
                    failure.httpStatus.value(), e.toString());
            return ResponseEntity.status(failure.httpStatus).body(failure.toDto());
        }
    }

    public ResponseEntity<String> probeHealth() {
        try {
            WhoAmIResponse response = await(sdkHolder.client().whoAmI(WhoAmIRequest.getDefaultInstance()));
            if (response != null && StringUtils.hasText(response.getId())) {
                return ResponseEntity.ok("ok");
            }
            return ResponseEntity.status(HttpStatus.BAD_GATEWAY).body("");
        } catch (Exception e) {
            return ResponseEntity.status(HttpStatus.BAD_GATEWAY).body("");
        }
    }

    private Object dispatch(String method, String upstreamPath, String query, Object requestBody) throws Exception {
        String path = normalizePath(upstreamPath);
        String upperMethod = method == null ? "GET" : method.toUpperCase(Locale.ROOT);

        if ("GET".equals(upperMethod) && "/me".equals(path)) {
            return AgentKProtoMapper.toMeDto(await(sdkHolder.client().whoAmI(WhoAmIRequest.getDefaultInstance())));
        }
        if ("GET".equals(upperMethod) && "/capabilities".equals(path)) {
            return listAllCapabilities();
        }
        if ("POST".equals(upperMethod) && "/capabilities/invoke".equals(path)) {
            return invokeCapability(requestBody);
        }
        if ("GET".equals(upperMethod) && "/profile".equals(path)) {
            AgentKErrorDto error = new AgentKErrorDto();
            error.setError("GET /profile is not available on agent-k gRPC API");
            throw new ConnectStyleException(HttpStatus.NOT_FOUND, error.getError());
        }
        if ("GET".equals(upperMethod) && "/tools/catalog".equals(path)) {
            return AgentKProtoMapper.toToolsCatalogDto(listAllCapabilityProtos(), objectMapper);
        }
        if ("POST".equals(upperMethod) && "/sessions".equals(path)) {
            return createSession(requestBody);
        }
        if ("GET".equals(upperMethod) && "/sessions".equals(path)) {
            return listSessions(query);
        }

        Matcher messages = SESSION_MESSAGES.matcher(path);
        if ("POST".equals(upperMethod) && messages.matches()) {
            return postMessage(messages.group(1), requestBody);
        }
        Matcher answers = SESSION_ANSWERS.matcher(path);
        if ("POST".equals(upperMethod) && answers.matches()) {
            return postAnswers(answers.group(1), requestBody);
        }
        Matcher cancel = SESSION_CANCEL.matcher(path);
        if ("POST".equals(upperMethod) && cancel.matches()) {
            cancelSession(cancel.group(1));
            return null;
        }
        Matcher events = SESSION_EVENTS.matcher(path);
        if ("GET".equals(upperMethod) && events.matches()) {
            return listEvents(events.group(1), query);
        }
        Matcher pipelines = SESSION_PIPELINES.matcher(path);
        if ("GET".equals(upperMethod) && pipelines.matches()) {
            return listPipelines(pipelines.group(1));
        }
        Matcher pipelineRuns = SESSION_PIPELINE_RUNS.matcher(path);
        if ("GET".equals(upperMethod) && pipelineRuns.matches()) {
            return listPipelineRuns(pipelineRuns.group(1));
        }
        if ("POST".equals(upperMethod) && pipelineRuns.matches()) {
            return startPipeline(pipelineRuns.group(1), requestBody);
        }
        Matcher pipelineRun = SESSION_PIPELINE_RUN.matcher(path);
        if ("GET".equals(upperMethod) && pipelineRun.matches()) {
            return getPipelineRun(pipelineRun.group(1), pipelineRun.group(2));
        }

        AgentKErrorDto error = new AgentKErrorDto();
        error.setError("unsupported agent-k path: " + path);
        throw new ConnectStyleException(HttpStatus.NOT_FOUND, error.getError());
    }

    private AgentKSessionDto createSession(Object requestBody) throws Exception {
        Map<String, Object> body = asMap(requestBody);
        String title = body.get("title") == null ? "" : String.valueOf(body.get("title"));
        Object profileObj = body.get("profile");
        Profile profile = Profile.getDefaultInstance();
        if (profileObj instanceof Map) {
            profile = AgentKProfileProtoConverter.toProfile((Map<String, Object>) profileObj, objectMapper);
        }
        CreateSessionRequest request = CreateSessionRequest.newBuilder()
                .setTitle(title)
                .setProfile(profile)
                .build();
        CreateSessionResponse response = await(sdkHolder.client().createSession(request));
        return AgentKProtoMapper.toSessionDto(response.getSession());
    }

    private AgentKListSessionsResponse listSessions(String query) throws Exception {
        Map<String, String> params = parseQuery(query);
        Integer limit = parseInt(params.get("limit"));
        Integer pageSize = AgentKPaging.resolveLimit(limit, AgentKPaging.SESSIONS_DEFAULT_LIMIT,
                AgentKPaging.SESSIONS_MAX_LIMIT);
        if (pageSize == null) {
            throw new IllegalArgumentException("limit must be a positive integer");
        }
        String before = params.get("before");
        ListSessionsRequest.Builder builder = ListSessionsRequest.newBuilder().setPageSize(pageSize);
        if (StringUtils.hasText(before)) {
            builder.setPageToken(before.trim());
        }
        ListSessionsResponse response = await(sdkHolder.client().listSessions(builder.build()));
        return AgentKProtoMapper.toListSessionsResponse(response.getSessionsList(), response.getNextPageToken());
    }

    private AgentKAppendResultDto postMessage(String sessionId, Object requestBody) throws Exception {
        AgentKPostMessageRequest message = objectMapper.convertValue(requestBody, AgentKPostMessageRequest.class);
        StartExecutionRequest request = StartExecutionRequest.newBuilder()
                .setSessionId(sessionId)
                .setMessage(AgentKMessageExecutionInputs.from(message))
                .build();
        StartExecutionResponse response = await(sdkHolder.client().startExecution(request));
        return buildAppendResult(sessionId, response.getExecution(), "user.input",
                message != null ? message.getText() : null, null);
    }

    private AgentKAppendResultDto postAnswers(String sessionId, Object requestBody) throws Exception {
        AgentKAnswerRequest answerRequest = objectMapper.convertValue(requestBody, AgentKAnswerRequest.class);
        AgentAnswersExecutionInput.Builder answers = AgentAnswersExecutionInput.newBuilder();
        if (answerRequest != null && StringUtils.hasText(answerRequest.getToolCallId())) {
            answers.setToolCallId(answerRequest.getToolCallId().trim());
        }
        if (answerRequest != null && answerRequest.getAnswers() != null) {
            for (AgentKAnswerRequest.AnswerItem item : answerRequest.getAnswers()) {
                if (item == null) {
                    continue;
                }
                AnswerInput.Builder input = AnswerInput.newBuilder();
                if (StringUtils.hasText(item.getQuestionId())) {
                    input.setQuestionId(item.getQuestionId().trim());
                }
                if (item.getSelected() != null) {
                    input.addAllSelected(item.getSelected());
                }
                answers.addAnswers(input);
            }
        }
        StartExecutionRequest request = StartExecutionRequest.newBuilder()
                .setSessionId(sessionId)
                .setAnswers(answers)
                .build();
        StartExecutionResponse response = await(sdkHolder.client().startExecution(request));
        return buildAppendResult(sessionId, response.getExecution(), "ask_user.responded", null,
                answerRequest != null ? answerRequest.getToolCallId() : null);
    }

    private void cancelSession(String sessionId) throws Exception {
        CancelExecutionRequest request = CancelExecutionRequest.newBuilder()
                .setSessionId(sessionId)
                .build();
        await(sdkHolder.client().cancelExecution(request));
    }

    private AgentKListEventsResponse listEvents(String sessionId, String query) throws Exception {
        Map<String, String> params = parseQuery(query);
        String afterRaw = params.get("after");
        Long parsedAfter = parseLong(afterRaw);
        if (StringUtils.hasText(afterRaw) && parsedAfter == null) {
            throw new IllegalArgumentException("after/limit invalid");
        }
        Long after = AgentKPaging.resolveAfter(parsedAfter);
        Integer limit = AgentKPaging.resolveLimit(parseInt(params.get("limit")),
                AgentKPaging.EVENTS_DEFAULT_LIMIT, AgentKPaging.EVENTS_MAX_LIMIT);
        if (after == null || limit == null) {
            throw new IllegalArgumentException("after/limit invalid");
        }
        boolean includeAll = "true".equalsIgnoreCase(params.get("all"));
        /*
         * gRPC ListEvents 按新→旧返回，pageToken 是 opaque token。
         * Event 没有 seq，禁止从 eventId 或列表位置伪造序号。
         * 完整拉回后按 agent-k created_at + eventId 排序；seq 仅在上游真有时透传。
         */
        List<AgentKListEventsResponse.AgentKEventDto> newestFirst = new ArrayList<>();
        Set<String> seenEventIds = new HashSet<>();
        Set<String> seenPageTokens = new HashSet<>();
        String pageToken = "";
        while (seenPageTokens.add(pageToken)) {
            ListEventsRequest.Builder builder = ListEventsRequest.newBuilder()
                    .setSessionId(sessionId)
                    .setPageSize(100);
            if (StringUtils.hasText(pageToken)) {
                builder.setPageToken(pageToken);
            }
            ListEventsResponse response = await(sdkHolder.client().listEvents(builder.build()));
            if (response.getEventsList().isEmpty()) {
                break;
            }
            for (Event event : response.getEventsList()) {
                String eventId = event.getEventId();
                if (StringUtils.hasText(eventId) && !seenEventIds.add(eventId)) {
                    continue;
                }
                AgentKListEventsResponse.AgentKEventDto dto =
                        rewriteEvent(AgentKEventWireConverter.toEventDto(event, objectMapper));
                if (dto != null) {
                    newestFirst.add(dto);
                }
            }
            if (!StringUtils.hasText(response.getNextPageToken())) {
                break;
            }
            pageToken = response.getNextPageToken();
        }
        return normalizeListedEvents(newestFirst, after, includeAll ? Integer.MAX_VALUE : limit);
    }

    static AgentKListEventsResponse normalizeListedEvents(
            List<AgentKListEventsResponse.AgentKEventDto> newestFirst, long after, int limit) {
        List<AgentKListEventsResponse.AgentKEventDto> chronological =
                newestFirst == null ? new ArrayList<>() : new ArrayList<>(newestFirst);
        chronological.sort(Comparator
                .comparing(AgentKGrpcExchange::createdAtKey)
                .thenComparing(AgentKGrpcExchange::eventIdKey));
        boolean hasNativeSeq = false;
        for (AgentKListEventsResponse.AgentKEventDto event : chronological) {
            if (event != null && event.getSeq() != null && event.getSeq() > 0) {
                hasNativeSeq = true;
                break;
            }
        }
        List<AgentKListEventsResponse.AgentKEventDto> page = new ArrayList<>();
        boolean hasMore = false;
        for (AgentKListEventsResponse.AgentKEventDto event : chronological) {
            if (event == null) {
                continue;
            }
            if (hasNativeSeq && event.getSeq() != null && event.getSeq() <= after) {
                continue;
            }
            if (page.size() < limit) {
                page.add(event);
            } else {
                hasMore = true;
            }
        }
        AgentKListEventsResponse result = new AgentKListEventsResponse();
        result.setEvents(page);
        result.setHasMore(hasMore);
        return result;
    }

    private static String createdAtKey(AgentKListEventsResponse.AgentKEventDto event) {
        if (event == null || !StringUtils.hasText(event.getCreatedAt())) {
            return "\uFFFF";
        }
        return event.getCreatedAt().trim();
    }

    private static String eventIdKey(AgentKListEventsResponse.AgentKEventDto event) {
        return event == null || event.getEventId() == null ? "" : event.getEventId();
    }

    private AgentKListWorkflowsResponse listPipelines(String sessionId) throws Exception {
        ListPipelinesResponse response = await(sdkHolder.client().listPipelines(
                ListPipelinesRequest.newBuilder().setSessionId(sessionId).build()));
        return AgentKProtoMapper.toListPipelinesResponse(response.getPipelinesList(), objectMapper);
    }

    private AgentKListWorkflowRunsResponse listPipelineRuns(String sessionId) throws Exception {
        ListExecutionsResponse response = await(sdkHolder.client().listExecutions(
                ListExecutionsRequest.newBuilder().setSessionId(sessionId).build()));
        return AgentKProtoMapper.toListExecutionsResponse(response.getExecutionsList(), objectMapper);
    }

    private AgentKWorkflowExecDto startPipeline(String sessionId, Object requestBody) throws Exception {
        AgentKStartWorkflowRequest start = objectMapper.convertValue(requestBody, AgentKStartWorkflowRequest.class);
        PipelineExecutionInput.Builder pipeline = PipelineExecutionInput.newBuilder();
        if (start != null && StringUtils.hasText(start.getPipeline())) {
            pipeline.setPipeline(start.getPipeline().trim());
        }
        if (start != null && start.getArgs() != null && !start.getArgs().isNull()) {
            Struct.Builder struct = Struct.newBuilder();
            JsonFormat.parser().ignoringUnknownFields().merge(start.getArgs().toString(), struct);
            pipeline.setArgs(struct.build());
        }
        StartExecutionRequest request = StartExecutionRequest.newBuilder()
                .setSessionId(sessionId)
                .setPipeline(pipeline)
                .build();
        StartExecutionResponse response = await(sdkHolder.client().startExecution(request));
        return AgentKProtoMapper.toWorkflowExecDto(response.getExecution(), objectMapper);
    }

    private AgentKWorkflowExecDto getPipelineRun(String sessionId, String execId) throws Exception {
        GetExecutionResponse response = await(sdkHolder.client().getExecution(
                GetExecutionRequest.newBuilder()
                        .setSessionId(sessionId)
                        .setExecutionId(execId)
                        .build()));
        return AgentKProtoMapper.toWorkflowExecDto(response.getExecution(), objectMapper);
    }

    private CapabilityInvokeOutputDto invokeCapability(Object requestBody) throws Exception {
        Map<String, Object> body = asMap(requestBody);
        Object capabilityObj = body.get("capabilityId");
        if (capabilityObj == null) {
            capabilityObj = body.get("capability_id");
        }
        String capabilityId = capabilityObj == null ? "" : String.valueOf(capabilityObj).trim();
        if (!StringUtils.hasText(capabilityId)) {
            throw new IllegalArgumentException("capabilityId is required");
        }
        InvokeCapabilityRequest.Builder request = InvokeCapabilityRequest.newBuilder()
                .setCapabilityId(capabilityId);
        Object inputObj = body.get("input");
        if (inputObj == null) {
            inputObj = body.get("args");
        }
        if (inputObj != null) {
            Struct.Builder struct = Struct.newBuilder();
            JsonFormat.parser().ignoringUnknownFields().merge(
                    objectMapper.valueToTree(inputObj).toString(), struct);
            request.setInput(struct.build());
        }
        InvokeCapabilityResponse response = await(
                sdkHolder.client().invokeCapability(request.build()),
                properties.getGrpcInvokeTimeoutMs());
        CapabilityInvokeOutputDto dto = new CapabilityInvokeOutputDto();
        dto.setCapabilityId(capabilityId);
        Value output = response.getOutput();
        dto.setOutput(output == null || output.getKindCase() == Value.KindCase.KIND_NOT_SET
                ? objectMapper.createObjectNode()
                : AgentKProtoMapper.valueToJsonNode(output, objectMapper));
        return dto;
    }

    private AgentKCapabilitiesDto listAllCapabilities() throws Exception {
        return AgentKProtoMapper.toCapabilitiesDto(listAllCapabilityProtos(), objectMapper);
    }

    private List<Capability> listAllCapabilityProtos() throws Exception {
        List<Capability> all = new ArrayList<>();
        Set<String> seenTokens = new HashSet<>();
        String pageToken = "";
        while (seenTokens.add(pageToken)) {
            ListCapabilitiesRequest.Builder builder = ListCapabilitiesRequest.newBuilder().setPageSize(100);
            if (StringUtils.hasText(pageToken)) {
                builder.setPageToken(pageToken);
            }
            ListCapabilitiesResponse response = await(sdkHolder.client().listCapabilities(builder.build()));
            all.addAll(response.getCapabilitiesList());
            String next = response.getNextPageToken();
            pageToken = next == null ? "" : next;
        }
        return all;
    }

    private AgentKAppendResultDto buildAppendResult(String sessionId,
            com.kuaishou.klingai.agentk.api.v1.Execution execution, String kind, String text, String toolCallId)
            throws Exception {
        String executionId = execution != null ? execution.getId() : "";
        String status = execution != null
                ? AgentKProtoMapper.mapExecutionStatus(execution.getStatus())
                : "running";
        ListEventsResponse events = await(sdkHolder.client().listEvents(
                ListEventsRequest.newBuilder().setSessionId(sessionId).setPageSize(20).build()));
        List<AgentKListEventsResponse.AgentKEventDto> matched = new ArrayList<>();
        for (Event event : events.getEventsList()) {
            AgentKListEventsResponse.AgentKEventDto dto =
                    rewriteEvent(AgentKEventWireConverter.toEventDto(event, objectMapper));
            if (dto == null) {
                continue;
            }
            if (kind.equals(dto.getKind())) {
                if ("user.input".equals(kind) && text != null && dto.getData() != null
                        && dto.getData().path("text").asText("").equals(text)) {
                    matched.add(dto);
                } else if ("ask_user.responded".equals(kind)) {
                    matched.add(dto);
                }
            }
        }
        if (matched.isEmpty() && events.getEventsList().size() > 0) {
            Event latest = events.getEventsList().get(0);
            AgentKListEventsResponse.AgentKEventDto dto =
                    rewriteEvent(AgentKEventWireConverter.toEventDto(latest, objectMapper));
            if (dto != null) {
                matched.add(dto);
            }
        }
        return AgentKProtoMapper.toAppendResult(sessionId, executionId, status, matched);
    }

    private HttpStatus resolveSuccessStatus(String method, String path) {
        if ("POST".equalsIgnoreCase(method) && "/sessions".equals(normalizePath(path))) {
            return HttpStatus.CREATED;
        }
        if ("POST".equalsIgnoreCase(method) && (path.contains("/messages")
                || path.contains("/answers") || path.contains("/cancel")
                || path.contains("/pipeline-runs"))) {
            return HttpStatus.ACCEPTED;
        }
        return HttpStatus.OK;
    }

    @SuppressWarnings("unchecked")
    private Map<String, Object> asMap(Object body) {
        if (body == null) {
            return Collections.emptyMap();
        }
        if (body instanceof Map) {
            return (Map<String, Object>) body;
        }
        return objectMapper.convertValue(body, new TypeReference<Map<String, Object>>() { });
    }

    private static String normalizePath(String path) {
        if (path == null) {
            return "";
        }
        return path.startsWith("/") ? path : "/" + path;
    }

    private static Map<String, String> parseQuery(String query) {
        Map<String, String> map = new LinkedHashMap<>();
        if (!StringUtils.hasText(query)) {
            return map;
        }
        for (String part : query.split("&")) {
            int eq = part.indexOf('=');
            if (eq > 0) {
                map.put(part.substring(0, eq), part.substring(eq + 1));
            }
        }
        return map;
    }

    private static Integer parseInt(String raw) {
        if (!StringUtils.hasText(raw)) {
            return null;
        }
        return Integer.parseInt(raw.trim());
    }

    private static Long parseLong(String raw) {
        if (!StringUtils.hasText(raw)) {
            return null;
        }
        try {
            return Long.parseLong(raw.trim());
        } catch (NumberFormatException e) {
            return null;
        }
    }

    private AgentKListEventsResponse.AgentKEventDto rewriteEvent(
            AgentKListEventsResponse.AgentKEventDto dto) {
        if (dto != null) {
            officeUrlService.rewriteJson(dto.getData());
        }
        return dto;
    }

    private <T> T await(CompletionStage<T> stage) throws Exception {
        return await(stage, properties.getGrpcReadTimeoutMs());
    }

    private <T> T await(CompletionStage<T> stage, int timeoutMs) throws Exception {
        try {
            return stage.toCompletableFuture().get(timeoutMs, TimeUnit.MILLISECONDS);
        } catch (TimeoutException e) {
            throw new ExecutionException(e);
        } catch (ExecutionException e) {
            Throwable cause = e.getCause() != null ? e.getCause() : e;
            if (cause instanceof Exception) {
                throw (Exception) cause;
            }
            if (cause instanceof Error) {
                throw (Error) cause;
            }
            throw e;
        }
    }

    /** 用于在 exchange 内抛出可映射的 HTTP 状态。 */
    static final class ConnectStyleException extends RuntimeException {
        final HttpStatus status;

        ConnectStyleException(HttpStatus status, String message) {
            super(message);
            this.status = status;
        }
    }
}
