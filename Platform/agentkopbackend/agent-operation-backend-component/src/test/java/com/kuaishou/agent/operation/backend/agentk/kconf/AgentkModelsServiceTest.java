package com.kuaishou.agent.operation.backend.agentk.kconf;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkModelsService.ModelsView;

class AgentkModelsServiceTest {

    @Test
    void listModelsExposesChatAttachmentCapabilities() {
        ModelsView view = new AgentkModelsService().listModels();

        assertThat(view.getChatModels()).isEqualTo(AgentkChatModelCatalog.names());
        assertThat(view.getModels()).hasSize(12);

        AgentkModelInfo imageOnly = find(view, "qwen3.5-plus");
        assertThat(imageOnly.getAttachments().getMode()).isEqualTo(AgentkChatAttachments.MODE_IMAGE);

        AgentkModelInfo allMedia = find(view, "gemini-3.1-pro-preview");
        assertThat(allMedia.getAttachments().getMode()).isEqualTo(AgentkChatAttachments.MODE_ALL);
        assertThat(allMedia.getAttachments().isVideo()).isTrue();
        assertThat(allMedia.getAttachments().isAudio()).isTrue();

        AgentkModelInfo textOnly = find(view, "deepseek-v4-pro");
        assertThat(textOnly.getAttachments().getMode()).isEqualTo(AgentkChatAttachments.MODE_NONE);
        assertThat(textOnly.getAttachments().isImage()).isFalse();
    }

    private static AgentkModelInfo find(ModelsView view, String name) {
        return view.getModels().stream()
                .filter(model -> name.equals(model.getName()))
                .findFirst()
                .orElseThrow();
    }
}
