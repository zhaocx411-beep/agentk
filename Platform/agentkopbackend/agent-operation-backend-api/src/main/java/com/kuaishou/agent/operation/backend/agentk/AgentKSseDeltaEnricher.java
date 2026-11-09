package com.kuaishou.agent.operation.backend.agentk;

import java.util.Arrays;
import java.util.Collections;
import java.util.HashSet;
import java.util.Set;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;

/**
 * 在 BFF 转发 SSE 帧时写入自增 {@code id}；对瞬态 delta 额外写入毫秒时间戳 {@code ts}。
 * 不改写非对象 JSON；解析失败则原样返回。
 */
final class AgentKSseDeltaEnricher {

    static final String FIELD_TS = "ts";
    static final String FIELD_ID = "id";

    private static final Set<String> DELTA_EVENTS = Collections.unmodifiableSet(
            new HashSet<>(Arrays.asList("reasoning.delta", "text.delta")));

    private AgentKSseDeltaEnricher() {
    }

    static boolean isDeltaEvent(String event) {
        return event != null && DELTA_EVENTS.contains(event);
    }

    /**
     * @param dataJson 单行 data 载荷（不含 {@code data:} 前缀）
     * @param id 本连接内自增序号（从 1 起）
     * @param tsMillis delta 事件写入 {@link #FIELD_TS}；非 delta 传 {@code null} 则不写
     */
    static String enrichData(String dataJson, long id, Long tsMillis, ObjectMapper objectMapper) {
        if (dataJson == null || dataJson.isEmpty() || objectMapper == null) {
            return dataJson;
        }
        try {
            JsonNode node = objectMapper.readTree(dataJson);
            if (!(node instanceof ObjectNode)) {
                return dataJson;
            }
            ObjectNode obj = (ObjectNode) node;
            obj.put(FIELD_ID, id);
            if (tsMillis != null) {
                obj.put(FIELD_TS, tsMillis);
            }
            return objectMapper.writeValueAsString(obj);
        } catch (Exception ignored) {
            return dataJson;
        }
    }
}
