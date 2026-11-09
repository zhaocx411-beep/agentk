package com.kuaishou.agent.operation.backend.platform.agent100.dto;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import lombok.Data;

/**
 * {@code GET /platform/agent100/{agentId}/config} 响应。
 */
@Data
public class Agent100ConfigView {

    private String agentId;
    /** 业务契约版本，Narrato 纪录片为 {@code 2}。 */
    private String contractVersion;
    private Map<String, Object> models = new LinkedHashMap<>();
    private List<CapabilityStatusView> capabilities = new ArrayList<>();
    private String capabilityError = "";
    /** 第一阶段不改上传链路；提示前端复用现有 media upload。 */
    private String mediaError = "";
    private Map<String, Object> voices = new LinkedHashMap<>();
    /**
     * 带文案的字段枚举：aspect / ttsProvider / emotion / asrLanguage 等。
     * 模型走 {@link #models}，音色走 {@link #voices}，TTS provider 目录走 {@link #tts}。
     */
    private Map<String, Object> fieldOptions = new LinkedHashMap<>();
    /** 对齐 Demo {@code /config.tts}：providers + defaultProvider。 */
    private Map<String, Object> tts = new LinkedHashMap<>();
    private List<PipelineView> pipelines = new ArrayList<>();

    @Data
    public static class CapabilityStatusView {
        private String id;
        private boolean published;
        private boolean required;
        private String group;
        private boolean activityOnly;
    }

    @Data
    public static class PipelineView {
        private String name;
        private String displayName;
        private String capability;
        private String temporalType;
        private String contractVersion;
        private Map<String, Object> inputSchema = new LinkedHashMap<>();
    }
}
