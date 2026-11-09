package com.kuaishou.agent.operation.backend.agentk.tts;

import java.util.ArrayList;
import java.util.List;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKTtsCatalogView;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKTtsEngineView;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKTtsVoiceView;

/**
 * 前端 TTS 下拉数据。
 */
public final class AgentKTtsCatalog {

    private AgentKTtsCatalog() {
    }

    public static AgentKTtsCatalogView view() {
        AgentKTtsCatalogView catalog = new AgentKTtsCatalogView();
        List<AgentKTtsEngineView> engines = new ArrayList<>();
        for (AgentKTtsEngine engine : AgentKTtsEngine.values()) {
            engines.add(toView(engine));
        }
        catalog.setEngines(engines);
        return catalog;
    }

    private static AgentKTtsEngineView toView(AgentKTtsEngine engine) {
        AgentKTtsEngineView view = new AgentKTtsEngineView();
        view.setEngine(engine.getId());
        view.setDisplayName(engine.getDisplayName());
        view.setTool(engine.getTool());
        view.setModel(engine.getModel());
        view.setDefaultVoiceId(engine.getDefaultVoiceId());
        List<AgentKTtsVoiceView> voices = new ArrayList<>();
        for (AgentKTtsEngine.Voice voice : engine.getVoices()) {
            AgentKTtsVoiceView item = new AgentKTtsVoiceView();
            item.setVoiceId(voice.getVoiceId());
            item.setDisplayName(voice.getDisplayName());
            voices.add(item);
        }
        view.setVoices(voices);
        return view;
    }
}
