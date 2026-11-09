package com.kuaishou.agent.operation.backend.platform.agent100.dto;

import java.util.ArrayList;
import java.util.List;

import lombok.Data;

/**
 * {@code GET /platform/agent100} 响应。
 */
@Data
public class Agent100CatalogView {

    private List<Agent100AgentView> agents = new ArrayList<>();
    private int count;
}
