package com.kuaishou.agent.operation.backend.agentk;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.agentk.ChatAssetSyncService.SessionAssetMeta;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKWorkflowExecDto;

class ChatAssetSyncServicePipelineAgentNameTest {

    @Test
    void capabilityRunKeepsCapabilitySourceKind() {
        AgentKWorkflowExecDto run = new AgentKWorkflowExecDto();
        run.setName(CapabilityRunService.PIPELINE_NAME);
        SessionAssetMeta meta = new SessionAssetMeta("ignored-agent", "some-workflow", false);

        assertThat(ChatAssetSyncService.isCapabilityAsset(run, meta)).isTrue();
    }

    @Test
    void workflowSessionMetaCarriesWorkflowName() {
        SessionAssetMeta meta = new SessionAssetMeta("", "纪录片成片", false);
        assertThat(meta.workflowName).isEqualTo("纪录片成片");
        assertThat(meta.agentName).isEmpty();
        assertThat(SessionAssetMeta.EMPTY.workflowName).isEmpty();
    }
}
