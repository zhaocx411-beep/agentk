package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.ArrayList;
import java.util.List;

import lombok.Data;

@Data
public class AgentKTtsCatalogView {

    private List<AgentKTtsEngineView> engines = new ArrayList<>();
}
