package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.ArrayList;
import java.util.List;

import lombok.Data;

@Data
public class AgentKTtsEngineView {

    private String engine;

    private String displayName;

    private String tool;

    private String model;

    private String defaultVoiceId;

    private List<AgentKTtsVoiceView> voices = new ArrayList<>();
}
