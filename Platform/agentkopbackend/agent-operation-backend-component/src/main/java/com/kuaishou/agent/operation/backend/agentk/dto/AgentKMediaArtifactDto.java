package com.kuaishou.agent.operation.backend.agentk.dto;

import lombok.Data;

/**
 * 展示层可直接消费的媒体产物 URL。
 */
@Data
public class AgentKMediaArtifactDto {

    /** image / video */
    private String type;
    private String url;
    /** 提取来源字段，如 video_url / imageUrl。 */
    private String source;
}
