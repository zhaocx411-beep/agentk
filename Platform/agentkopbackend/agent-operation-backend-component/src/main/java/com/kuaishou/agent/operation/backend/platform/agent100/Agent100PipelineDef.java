package com.kuaishou.agent.operation.backend.platform.agent100;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Agent100 模块内一条 Pipeline 的静态定义（对齐 Demo registry + Narrato contracts）。
 */
public final class Agent100PipelineDef {

    private final String capability;
    private final String name;
    private final String displayName;
    private final Map<String, Object> inputSchema;
    /** Temporal / worker 工作流类型，如 {@code narrato.documentary.create_video.v2}。 */
    private final String temporalType;
    private final String contractVersion;

    public Agent100PipelineDef(String capability, String name, String displayName,
            Map<String, Object> inputSchema) {
        this(capability, name, displayName, inputSchema, null, null);
    }

    public Agent100PipelineDef(String capability, String name, String displayName,
            Map<String, Object> inputSchema, String temporalType, String contractVersion) {
        this.capability = capability;
        this.name = name;
        this.displayName = displayName;
        this.inputSchema = inputSchema == null
                ? Collections.emptyMap()
                : Collections.unmodifiableMap(new LinkedHashMap<>(inputSchema));
        this.temporalType = temporalType;
        this.contractVersion = contractVersion;
    }

    public String getCapability() {
        return capability;
    }

    public String getName() {
        return name;
    }

    public String getDisplayName() {
        return displayName;
    }

    public Map<String, Object> getInputSchema() {
        return inputSchema;
    }

    public String getTemporalType() {
        return temporalType;
    }

    public String getContractVersion() {
        return contractVersion;
    }
}
