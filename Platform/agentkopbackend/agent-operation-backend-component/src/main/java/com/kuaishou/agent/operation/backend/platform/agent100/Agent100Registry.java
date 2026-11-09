package com.kuaishou.agent.operation.backend.platform.agent100;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Optional;

import com.kuaishou.agent.operation.backend.platform.agent100.narrato.NarratoDefinition;

/**
 * Agent100 代码注册表（对齐 Demo {@code agent100/registry.py}）。
 * <p>
 * 第一阶段仅内存常量；第二阶段可改为读 DB 分组表。
 */
public final class Agent100Registry {

    private static final List<Agent100Definition> AGENTS = Collections.unmodifiableList(
            Collections.singletonList(NarratoDefinition.definition()));

    private Agent100Registry() {
    }

    public static List<Agent100Definition> all() {
        return AGENTS;
    }

    public static Optional<Agent100Definition> find(String agentId) {
        if (agentId == null) {
            return Optional.empty();
        }
        String key = agentId.trim();
        for (Agent100Definition agent : AGENTS) {
            if (key.equals(agent.getId())) {
                return Optional.of(agent);
            }
        }
        return Optional.empty();
    }

    public static Agent100Definition require(String agentId) {
        return find(agentId).orElseThrow(() -> new IllegalArgumentException(
                "unknown Agent100 id " + agentId + "; choose one of: " + knownIds()));
    }

    public static List<String> knownIds() {
        List<String> ids = new ArrayList<>(AGENTS.size());
        for (Agent100Definition agent : AGENTS) {
            ids.add(agent.getId());
        }
        return ids;
    }
}
