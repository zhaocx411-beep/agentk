package com.kuaishou.agent.operation.backend.agentk;

import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKListEventsResponse;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKRegressionCheckResultDto;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKRegressionChecksDto;

/**
 * 在线回归判定：基于会话事件账本（不再依赖已删除的 /trace）。
 */
@Component
public class AgentKRegressionChecker {

    private static final String STATUS_COMPLETED = "completed";

    public AgentKRegressionCheckResultDto runChecks(AgentKRegressionChecksDto checks, String status,
            List<AgentKListEventsResponse.AgentKEventDto> events) {
        if (checks == null) {
            checks = new AgentKRegressionChecksDto();
        }
        List<String> reasons = new ArrayList<>();

        if (wantCompleted(checks) && !STATUS_COMPLETED.equals(status)) {
            reasons.add("session turn status = " + status + ", want completed");
        }

        if (checks.getContains() != null && !checks.getContains().isEmpty()) {
            String text = finalAssistantText(events);
            for (String keyword : checks.getContains()) {
                if (!text.contains(keyword)) {
                    reasons.add("final answer missing keyword \"" + keyword + "\"");
                }
            }
        }

        if (StringUtils.hasText(checks.getToolCalled()) && !toolCalledOk(events, checks.getToolCalled())) {
            reasons.add("tool \"" + checks.getToolCalled() + "\" not successfully called");
        }

        AgentKRegressionCheckResultDto result = new AgentKRegressionCheckResultDto();
        result.setPassed(reasons.isEmpty());
        result.setReasons(reasons.isEmpty() ? Collections.emptyList() : reasons);
        return result;
    }

    private static boolean wantCompleted(AgentKRegressionChecksDto checks) {
        return checks.getCompleted() == null || Boolean.TRUE.equals(checks.getCompleted());
    }

    private static String finalAssistantText(List<AgentKListEventsResponse.AgentKEventDto> events) {
        if (events == null || events.isEmpty()) {
            return "";
        }
        StringBuilder sb = new StringBuilder();
        for (AgentKListEventsResponse.AgentKEventDto event : events) {
            if (!"assistant.text".equals(event.getKind()) || event.getData() == null) {
                continue;
            }
            String text = event.getData().path("text").asText("");
            if (!StringUtils.hasText(text)) {
                continue;
            }
            if (sb.length() > 0) {
                sb.append('\n');
            }
            sb.append(text);
        }
        return sb.toString();
    }

    private static boolean toolCalledOk(List<AgentKListEventsResponse.AgentKEventDto> events, String toolName) {
        if (events == null || events.isEmpty()) {
            return false;
        }
        Map<String, String> toolCallIdToName = new HashMap<>();
        Set<String> succeeded = new HashSet<>();
        for (AgentKListEventsResponse.AgentKEventDto event : events) {
            JsonNode data = event.getData();
            if (data == null) {
                continue;
            }
            if ("assistant.tool_call".equals(event.getKind())) {
                String id = data.path("toolCallId").asText(null);
                String name = data.path("toolName").asText(null);
                if (StringUtils.hasText(id) && StringUtils.hasText(name)) {
                    toolCallIdToName.put(id, name);
                }
            } else if ("tool.exec_succeeded".equals(event.getKind())) {
                String id = data.path("toolCallId").asText(null);
                if (StringUtils.hasText(id)) {
                    succeeded.add(id);
                }
            }
        }
        for (String id : succeeded) {
            if (toolName.equals(toolCallIdToName.get(id))) {
                return true;
            }
        }
        return false;
    }

    static String inferTurnStatus(List<AgentKListEventsResponse.AgentKEventDto> events) {
        return ChatRunFollowUpService.inferTurnStatus(events);
    }
}
