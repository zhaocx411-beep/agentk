package com.kuaishou.agent.operation.backend.platform.service;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.ArrayList;
import java.util.List;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.platform.entity.ChatAsset;

class ChatAssetPortfolioServiceTest {

    @Test
    void roundKeyGroupsApiAssetsByWorkflowExecId() {
        ChatAsset a = asset(421L, "api", "ecs-da4f8b18ea15", null, 2879789652L);
        ChatAsset b = asset(422L, "api", "ecs-da4f8b18ea15", null, 2879789652L);
        ChatAsset other = asset(409L, "api", "ecs-784091911740", null, 4235169888L);

        assertThat(ChatAssetPortfolioService.roundKey(a)).isEqualTo("api:ecs-da4f8b18ea15");
        assertThat(ChatAssetPortfolioService.roundKey(b)).isEqualTo(ChatAssetPortfolioService.roundKey(a));
        assertThat(ChatAssetPortfolioService.roundKey(other)).isEqualTo("api:ecs-784091911740");
    }

    @Test
    void roundKeyStillGroupsPipelineByWorkflowExecId() {
        ChatAsset pipeline = asset(420L, "pipeline", "exe_01a01ec8", null, 95L);
        assertThat(ChatAssetPortfolioService.roundKey(pipeline)).isEqualTo("pipeline:exe_01a01ec8");
    }

    @Test
    void roundKeyGroupsCapabilityByWorkflowExecId() {
        ChatAsset capability = asset(430L, "capability", "exe_cap_01", null, 95L);
        ChatAsset sameRun = asset(431L, "capability", "exe_cap_01", null, 95L);
        assertThat(ChatAssetPortfolioService.roundKey(capability)).isEqualTo("capability:exe_cap_01");
        assertThat(ChatAssetPortfolioService.roundKey(sameRun))
                .isEqualTo(ChatAssetPortfolioService.roundKey(capability));
    }

    @Test
    void roundKeyDoesNotScatterWhenApiHasWorkflowExecId() {
        ChatAsset asset = asset(421L, "api", "ecs-da4f8b18ea15", null, 2879789652L);
        assertThat(ChatAssetPortfolioService.roundKey(asset))
                .isNotEqualTo("session:2879789652:asset:421");
    }

    @Test
    void splitAllImagesPutsLastAsResult() {
        List<ChatAsset> chronological = List.of(
                asset(421L, "api", "ecs-da4f8b18ea15", null, 2879789652L),
                asset(422L, "api", "ecs-da4f8b18ea15", null, 2879789652L),
                asset(425L, "api", "ecs-da4f8b18ea15", null, 2879789652L));
        chronological.forEach(a -> a.setAssetType("image"));

        List<ChatAsset> intermediate = new ArrayList<>();
        List<ChatAsset> result = new ArrayList<>();
        ChatAssetPortfolioService.splitIntermediateAndResult(chronological, intermediate, result);

        assertThat(intermediate).extracting(ChatAsset::getId).containsExactly(421L, 422L);
        assertThat(result).extracting(ChatAsset::getId).containsExactly(425L);
    }

    private static ChatAsset asset(long id, String sourceKind, String workflowExecId,
            String agentkRunId, long sessionId) {
        ChatAsset asset = new ChatAsset();
        asset.setId(id);
        asset.setSourceKind(sourceKind);
        asset.setWorkflowExecId(workflowExecId);
        asset.setAgentkRunId(agentkRunId);
        asset.setSessionId(sessionId);
        asset.setAssetType("image");
        return asset;
    }
}
