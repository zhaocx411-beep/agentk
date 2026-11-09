package com.kuaishou.agent.operation.backend.agentk.grpc;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import org.springframework.util.StringUtils;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.google.protobuf.Timestamp;
import com.google.protobuf.util.JsonFormat;
import com.kuaishou.klingai.agentk.api.v1.Capability;
import com.kuaishou.klingai.agentk.api.v1.CapabilitySource;
import com.kuaishou.klingai.agentk.api.v1.Execution;
import com.kuaishou.klingai.agentk.api.v1.ExecutionStatus;
import com.kuaishou.klingai.agentk.api.v1.Pipeline;
import com.kuaishou.klingai.agentk.api.v1.PipelineExecution;
import com.kuaishou.klingai.agentk.api.v1.Session;
import com.kuaishou.klingai.agentk.api.v1.StartExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.WhoAmIResponse;
import com.kuaishou.agent.operation.backend.agentk.AgentkCoreWorkflowIds;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKAppendResultDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto.AgentKCapabilityItemDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKCapabilitiesDto.AgentKCapabilityToolDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListSessionsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListWorkflowRunsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListWorkflowsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMeDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKSessionDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKToolsCatalogDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKWorkflowExecDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKWorkflowSummaryDto;

/**
 * protobuf 响应 → 平台现有 DTO（保持前端契约不变）。
 */
final class AgentKProtoMapper {

    private AgentKProtoMapper() {
    }

    static AgentKSessionDto toSessionDto(Session session) {
        if (session == null) {
            return null;
        }
        AgentKSessionDto dto = new AgentKSessionDto();
        dto.setId(session.getId());
        dto.setTenantId(session.getAppId());
        dto.setTitle(session.getTitle());
        dto.setCreatedAt(AgentKEventWireConverter.formatTimestamp(session.getCreatedAt()));
        return dto;
    }

    static AgentKMeDto toMeDto(WhoAmIResponse response) {
        AgentKMeDto dto = new AgentKMeDto();
        dto.setId(response.getId());
        dto.setTenantId(response.getAppId());
        dto.setDisplay(response.getDisplayName());
        dto.setKind("service");
        return dto;
    }

    static AgentKCapabilitiesDto toCapabilitiesDto(List<Capability> capabilities, ObjectMapper objectMapper) {
        AgentKCapabilitiesDto dto = new AgentKCapabilitiesDto();
        List<AgentKCapabilityItemDto> items = new ArrayList<>();
        if (capabilities != null) {
            for (Capability cap : capabilities) {
                if (cap == null || !StringUtils.hasText(cap.getId())) {
                    continue;
                }
                AgentKCapabilityItemDto item = new AgentKCapabilityItemDto();
                item.setId(cap.getId());
                item.setDescription(cap.getDescription());
                String kind = inferCapabilityKind(cap);
                item.setKind(kind);
                item.setInputSchema(structToJsonNode(cap.getInputSchema(), objectMapper));
                item.setOutputSchema(structToJsonNode(cap.getOutputSchema(), objectMapper));
                if (cap.hasTimeout()) {
                    item.setTimeoutSeconds(cap.getTimeout().getSeconds());
                }
                String leaf = leafName(cap.getId());
                if (cap.hasTool()) {
                    AgentKCapabilityToolDto tool = new AgentKCapabilityToolDto();
                    tool.setName(leaf);
                    tool.setDescription(cap.getTool().getDescription());
                    tool.setMode("blocking");
                    item.setTool(tool);
                    item.setToolRef("tool:" + cap.getId());
                }
                // workflow 即使也声明了 tool（双通道），Lab 名单仍要走 pipeline。
                if ("workflow".equals(kind) || !cap.hasTool()) {
                    AgentKCapabilitiesDto.AgentKCapabilityPipelineDto pipeline =
                            new AgentKCapabilitiesDto.AgentKCapabilityPipelineDto();
                    pipeline.setName(leaf);
                    pipeline.setDescription(cap.getDescription());
                    item.setPipeline(pipeline);
                    item.setPipelineRef("pipeline:" + cap.getId());
                }
                items.add(item);
            }
        }
        dto.setCapabilities(items);
        return dto;
    }

    static AgentKToolsCatalogDto toToolsCatalogDto(List<Capability> capabilities, ObjectMapper objectMapper) {
        AgentKToolsCatalogDto dto = new AgentKToolsCatalogDto();
        if (capabilities == null) {
            return dto;
        }
        for (Capability cap : capabilities) {
            if (cap == null || !cap.hasTool() || !StringUtils.hasText(cap.getId())) {
                continue;
            }
            AgentKToolsCatalogDto.AgentKToolCatalogItemDto tool = new AgentKToolsCatalogDto.AgentKToolCatalogItemDto();
            tool.setName(leafName(cap.getId()));
            tool.setDescription(cap.getTool().getDescription());
            tool.setInputSchema(structToJsonNode(cap.getInputSchema(), objectMapper));
            tool.setBindingKind("activity");
            dto.getTools().add(tool);
        }
        return dto;
    }

    static AgentKListSessionsResponse toListSessionsResponse(List<Session> sessions, String nextPageToken) {
        AgentKListSessionsResponse dto = new AgentKListSessionsResponse();
        List<AgentKSessionDto> list = new ArrayList<>();
        if (sessions != null) {
            for (Session session : sessions) {
                AgentKSessionDto item = toSessionDto(session);
                if (item != null) {
                    list.add(item);
                }
            }
        }
        dto.setSessions(list);
        dto.setHasMore(StringUtils.hasText(nextPageToken));
        return dto;
    }

    static AgentKListWorkflowsResponse toListPipelinesResponse(List<Pipeline> pipelines, ObjectMapper objectMapper) {
        AgentKListWorkflowsResponse dto = new AgentKListWorkflowsResponse();
        List<AgentKWorkflowSummaryDto> list = new ArrayList<>();
        if (pipelines != null) {
            for (Pipeline pipeline : pipelines) {
                if (pipeline == null || !StringUtils.hasText(pipeline.getName())) {
                    continue;
                }
                AgentKWorkflowSummaryDto summary = new AgentKWorkflowSummaryDto();
                summary.setName(pipeline.getName());
                summary.setDescription(pipeline.getDescription());
                JsonNode schema = structToJsonNode(pipeline.getInputSchema(), objectMapper);
                if (schema != null && schema.isObject()) {
                    Map<String, Object> inputs = objectMapper.convertValue(schema,
                            new TypeReference<Map<String, Object>>() { });
                    summary.setInputs(inputs);
                }
                list.add(summary);
            }
        }
        dto.setPipelines(list);
        return dto;
    }

    static AgentKListWorkflowRunsResponse toListExecutionsResponse(List<Execution> executions,
            ObjectMapper objectMapper) {
        AgentKListWorkflowRunsResponse dto = new AgentKListWorkflowRunsResponse();
        List<AgentKWorkflowExecDto> runs = new ArrayList<>();
        if (executions != null) {
            for (Execution execution : executions) {
                AgentKWorkflowExecDto run = toWorkflowExecDto(execution, objectMapper);
                if (run != null) {
                    runs.add(run);
                }
            }
        }
        dto.setPipelineRuns(runs);
        return dto;
    }

    static AgentKWorkflowExecDto toWorkflowExecDto(Execution execution, ObjectMapper objectMapper) {
        if (execution == null) {
            return null;
        }
        AgentKWorkflowExecDto dto = new AgentKWorkflowExecDto();
        dto.setId(execution.getId());
        dto.setSessionId(execution.getSessionId());
        dto.setStatus(mapExecutionStatus(execution.getStatus()));
        dto.setProfileVersion(execution.getProfileVersion());
        dto.setError(execution.getError());
        dto.setCreatedAt(AgentKEventWireConverter.formatTimestamp(execution.getCreatedAt()));
        if (execution.hasFinishedAt()) {
            dto.setFinishedAt(AgentKEventWireConverter.formatTimestamp(execution.getFinishedAt()));
        }
        if (execution.getKindCase() == Execution.KindCase.PIPELINE) {
            PipelineExecution pipeline = execution.getPipeline();
            dto.setName(pipeline.getName());
            if (StringUtils.hasText(pipeline.getCapabilityId())) {
                dto.setCapabilityId(pipeline.getCapabilityId().trim());
            }
            dto.setArgs(structToJsonNode(pipeline.getArgs(), objectMapper));
            dto.setOutput(valueToJsonNode(pipeline.getOutput(), objectMapper));
        }
        return dto;
    }

    static AgentKAppendResultDto toAppendResult(String sessionId, String executionId, String status,
            List<AgentKListEventsResponse.AgentKEventDto> events) {
        AgentKAppendResultDto dto = new AgentKAppendResultDto();
        dto.setSessionId(sessionId);
        dto.setExecutionId(executionId);
        dto.setStatus(status);
        dto.setEvents(events);
        return dto;
    }

    static String mapExecutionStatus(ExecutionStatus status) {
        switch (status) {
            case EXECUTION_STATUS_RUNNING:
                return "running";
            case EXECUTION_STATUS_COMPLETED:
                return "completed";
            case EXECUTION_STATUS_FAILED:
                return "failed";
            case EXECUTION_STATUS_CANCELLED:
                return "cancelled";
            case EXECUTION_STATUS_UNSPECIFIED:
            default:
                return "running";
        }
    }

    /**
     * 优先读 proto {@code kind}（含 sdk 尚未生成该字段时的 unknown field 9），
     * 再按 CoreManifest 已知 workflow id，最后才回退：有 tool 当 activity，否则 workflow。
     * <p>
     * 不能只看 {@code hasTool()}：brand_ad / 可灵等 workflow 也会带 tool 声明。
     */
    static String inferCapabilityKind(Capability cap) {
        String declared = kindFromProto(cap);
        if (StringUtils.hasText(declared)) {
            return declared;
        }
        if (cap != null && AgentkCoreWorkflowIds.isWorkflow(cap.getId())) {
            return "workflow";
        }
        if (cap != null && cap.hasTool()) {
            return "activity";
        }
        return "workflow";
    }

    /** api.v1.Capability.kind = 9；1=workflow，2=activity。 */
    private static final int CAPABILITY_KIND_FIELD = 9;

    private static String kindFromProto(Capability cap) {
        try {
            Object kind = cap.getClass().getMethod("getKind").invoke(cap);
            if (kind != null) {
                String name = kind.toString();
                if (name.contains("WORKFLOW") || "workflow".equalsIgnoreCase(name)) {
                    return "workflow";
                }
                if (name.contains("ACTIVITY") && !name.contains("UNSPECIFIED")
                        || "activity".equalsIgnoreCase(name)) {
                    return "activity";
                }
            }
        } catch (ReflectiveOperationException ignored) {
            // sdk 0.2.2 无 getKind()
        }
        try {
            com.google.protobuf.UnknownFieldSet unknown = cap.getUnknownFields();
            if (unknown.hasField(CAPABILITY_KIND_FIELD)) {
                java.util.List<java.lang.Long> varints =
                        unknown.getField(CAPABILITY_KIND_FIELD).getVarintList();
                if (varints != null && !varints.isEmpty()) {
                    long value = varints.get(0);
                    if (value == 1L) {
                        return "workflow";
                    }
                    if (value == 2L) {
                        return "activity";
                    }
                }
            }
        } catch (RuntimeException ignored) {
            return null;
        }
        return null;
    }

    private static String leafName(String capabilityId) {
        if (!StringUtils.hasText(capabilityId)) {
            return "";
        }
        int dot = capabilityId.lastIndexOf('.');
        return dot >= 0 ? capabilityId.substring(dot + 1) : capabilityId;
    }

    static JsonNode structToJsonNode(com.google.protobuf.Struct struct, ObjectMapper objectMapper) {
        if (struct == null) {
            return null;
        }
        try {
            return objectMapper.readTree(JsonFormat.printer().print(struct));
        } catch (Exception e) {
            return objectMapper.createObjectNode();
        }
    }

    static JsonNode valueToJsonNode(com.google.protobuf.Value value, ObjectMapper objectMapper) {
        if (value == null) {
            return null;
        }
        try {
            return objectMapper.readTree(JsonFormat.printer().print(value));
        } catch (Exception e) {
            return objectMapper.createObjectNode();
        }
    }
}
