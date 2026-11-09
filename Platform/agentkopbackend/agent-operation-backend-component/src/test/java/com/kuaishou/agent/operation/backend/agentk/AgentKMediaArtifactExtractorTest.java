package com.kuaishou.agent.operation.backend.agentk;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;

import org.junit.jupiter.api.Test;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMediaArtifactDto;

class AgentKMediaArtifactExtractorTest {

    private final ObjectMapper objectMapper = new ObjectMapper();

    @Test
    void sanitizeUrlStripsTrailingChinese() {
        assertThat(AgentKMediaArtifactExtractor.sanitizeUrl(
                "https://bs3.example.com/a.png和提示词"))
                .isEqualTo("https://bs3.example.com/a.png");
    }

    @Test
    void sanitizeUrlStripsTrailingBacktick() {
        assertThat(AgentKMediaArtifactExtractor.sanitizeUrl(
                "https://bs3-hb1.staging.kuaishou.com/user-zhangyuchen05/capabilities/images/"
                        + "256ded1e76b1d2773768df15c715f102.png`"))
                .isEqualTo("https://bs3-hb1.staging.kuaishou.com/user-zhangyuchen05/capabilities/images/"
                        + "256ded1e76b1d2773768df15c715f102.png");
    }

    @Test
    void extractDoesNotKeepMarkdownBacktickAfterPng() throws Exception {
        ObjectNode data = objectMapper.createObjectNode();
        data.put("text", "产物：`https://bs3.example.com/cap.png` 请查看");

        List<AgentKMediaArtifactDto> artifacts = AgentKMediaArtifactExtractor.extract(data);

        assertThat(artifacts).hasSize(1);
        assertThat(artifacts.get(0).getUrl()).isEqualTo("https://bs3.example.com/cap.png");
        assertThat(artifacts.get(0).getUrl()).doesNotContain("`");
    }

    @Test
    void extractDoesNotGluePromptOntoUrlFromAssistantText() throws Exception {
        ObjectNode data = objectMapper.createObjectNode();
        data.put("text", "海报已生成！\n\n"
                + "![海报](https://bs3-hb1.staging.kuaishou.com/user-x/images/511ef0.png)\n\n"
                + "**海报设计说明：**\n- 构图竖版");

        List<AgentKMediaArtifactDto> artifacts = AgentKMediaArtifactExtractor.extract(data);

        assertThat(artifacts).hasSize(1);
        assertThat(artifacts.get(0).getUrl())
                .isEqualTo("https://bs3-hb1.staging.kuaishou.com/user-x/images/511ef0.png");
        assertThat(artifacts.get(0).getUrl()).doesNotContain("海报");
    }

    @Test
    void extractDoesNotTreatWholeHttpPrefixedBlobAsUrl() throws Exception {
        ObjectNode data = objectMapper.createObjectNode();
        data.put("text", "https://bs3.example.com/cap.png和提示词还很长");

        List<AgentKMediaArtifactDto> artifacts = AgentKMediaArtifactExtractor.extract(data);

        assertThat(artifacts).hasSize(1);
        assertThat(artifacts.get(0).getUrl()).isEqualTo("https://bs3.example.com/cap.png");
    }
}
