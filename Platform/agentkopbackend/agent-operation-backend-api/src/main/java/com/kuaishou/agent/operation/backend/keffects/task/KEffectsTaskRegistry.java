package com.kuaishou.agent.operation.backend.keffects.task;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

/**
 * taskType 到 adapter 的注册表。
 */
@Component
public class KEffectsTaskRegistry {

    private final Map<String, KEffectsTaskAdapter> adapters;

    public KEffectsTaskRegistry(List<KEffectsTaskAdapter> adapters) {
        Map<String, KEffectsTaskAdapter> map = new LinkedHashMap<>();
        if (adapters != null) {
            for (KEffectsTaskAdapter adapter : adapters) {
                if (adapter == null || !StringUtils.hasText(adapter.taskType())) {
                    continue;
                }
                String key = adapter.taskType().trim();
                if (map.containsKey(key)) {
                    throw new IllegalStateException("duplicate KEffects task adapter: " + key);
                }
                map.put(key, adapter);
            }
        }
        this.adapters = map;
    }

    public KEffectsTaskAdapter require(String taskType) {
        if (!StringUtils.hasText(taskType)) {
            throw new IllegalArgumentException("taskType is required");
        }
        KEffectsTaskAdapter adapter = adapters.get(taskType.trim());
        if (adapter == null) {
            throw new IllegalArgumentException("unsupported taskType: " + taskType.trim());
        }
        return adapter;
    }

    public KEffectsTaskAdapter find(String taskType) {
        if (!StringUtils.hasText(taskType)) {
            return null;
        }
        return adapters.get(taskType.trim());
    }
}
