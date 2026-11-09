package com.kuaishou.agent.operation.backend.platform.service;

import java.util.List;

import com.baomidou.mybatisplus.core.metadata.IPage;
import com.kuaishou.agent.operation.backend.platform.entity.Agent;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;

/**
 * Agent 版本：创建 → 编译草稿 → 发布。
 */
public interface AgentVersionService {

    Agent createAgent(String name, String description, String userId, String tenantId);

    AgentVersion saveDraft(long agentId, String profileYaml, String userId);

    AgentVersion publish(long agentVersionId);

    AgentVersion publish(long agentVersionId, String userId);

    AgentVersion requirePublished(long agentVersionId);

    Agent findAgent(long agentId);

    AgentVersion findVersion(long agentVersionId);

    List<Agent> listMine(String userId);

    IPage<Agent> pageMine(String userId, long pageNum, long pageSize);

    List<AgentVersion> listVersions(long agentId);

    /** 从 YAML 的 version: 行提取；没有则用 contentHash 前 12 位，对齐 agent-k。 */
    static String extractProfileVersionLabel(String profileYaml, String contentHash) {
        return AgentVersionServiceSupport.extractProfileVersionLabel(profileYaml, contentHash);
    }

    static String resolveProfileVersionLabel(AgentVersion version) {
        if (version == null) {
            return "";
        }
        return extractProfileVersionLabel(version.getProfileYaml(), version.getContentHash());
    }
}
