package com.kuaishou.agent.operation.backend.platform.dto;

import lombok.Data;

@Data
public class StreamCursorUpdateRequest {
    private String agentkRunId;
    private Integer lastEventSeq;
    /** idle / running / waiting_job / completed / failed */
    private String activeRunStatus;
}
