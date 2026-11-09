package com.kuaishou.agent.operation.backend.agentk.kconf;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import org.springframework.stereotype.Service;

import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkModelsParser.ParsedCatalog;

/**
 * 硬编码 chat 主模型目录，供 API 与离线评测名单使用。
 * 名单与 attachments 能力对齐 agent-k {@code modelcontract} FamilyChat。
 */
@Service
public class AgentkModelsService {

    public ModelsView listModels() {
        ParsedCatalog catalog = AgentkModelsParser.fromChatModelNames(AgentkChatModelCatalog.names());
        ModelsView view = new ModelsView();
        view.setKconfKey(AgentkModelsKconf.KEY);
        view.setModels(new ArrayList<>(catalog.getModels()));
        view.setNames(catalog.allNames());
        view.setChatModels(catalog.chatModelNames());
        view.setToolModels(new LinkedHashMap<>(catalog.toolModels()));
        return view;
    }

    /** 供 ProfileCompiler 使用的硬编码快照。 */
    public ParsedCatalog snapshot() {
        return AgentkModelsParser.fromChatModelNames(AgentkChatModelCatalog.names());
    }

    public static final class ModelsView {
        private String kconfKey;
        private String version;
        private List<AgentkModelInfo> models = new ArrayList<>();
        private List<String> names = new ArrayList<>();
        private List<String> chatModels = new ArrayList<>();
        private Map<String, List<String>> toolModels = new LinkedHashMap<>();

        public String getKconfKey() {
            return kconfKey;
        }

        public void setKconfKey(String kconfKey) {
            this.kconfKey = kconfKey;
        }

        public String getVersion() {
            return version;
        }

        public void setVersion(String version) {
            this.version = version;
        }

        public List<AgentkModelInfo> getModels() {
            return models;
        }

        public void setModels(List<AgentkModelInfo> models) {
            this.models = models;
        }

        public List<String> getNames() {
            return names;
        }

        public void setNames(List<String> names) {
            this.names = names;
        }

        public List<String> getChatModels() {
            return chatModels;
        }

        public void setChatModels(List<String> chatModels) {
            this.chatModels = chatModels;
        }

        public Map<String, List<String>> getToolModels() {
            return toolModels;
        }

        public void setToolModels(Map<String, List<String>> toolModels) {
            this.toolModels = toolModels;
        }
    }
}
