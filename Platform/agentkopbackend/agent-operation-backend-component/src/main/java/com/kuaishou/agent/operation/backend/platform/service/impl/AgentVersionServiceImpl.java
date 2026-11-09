package com.kuaishou.agent.operation.backend.platform.service.impl;

import java.time.LocalDateTime;
import java.util.List;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.core.metadata.IPage;
import com.baomidou.mybatisplus.extension.plugins.pagination.Page;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.AgentMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.AgentVersionMapper;
import com.kuaishou.agent.operation.backend.platform.entity.Agent;
import com.kuaishou.agent.operation.backend.platform.entity.AgentVersion;
import com.kuaishou.agent.operation.backend.platform.service.AgentVersionService;
import com.kuaishou.agent.operation.backend.platform.support.PlatformIds;

/**
 * Agent 版本实现。
 */
@Service
public class AgentVersionServiceImpl implements AgentVersionService {

    private static final Logger LOGGER = LoggerFactory.getLogger(AgentVersionServiceImpl.class);

    private final AgentMapper agentMapper;
    private final AgentVersionMapper agentVersionMapper;

    public AgentVersionServiceImpl(AgentMapper agentMapper, AgentVersionMapper agentVersionMapper) {
        this.agentMapper = agentMapper;
        this.agentVersionMapper = agentVersionMapper;
    }

    @Override
    public Agent createAgent(String name, String description, String userId, String tenantId) {
        LocalDateTime now = LocalDateTime.now();
        Agent agent = new Agent();
        agent.setName(require(name, "name"));
        agent.setDescription(empty(description));
        agent.setOwnerUserId(empty(userId));
        agent.setTenantId(empty(tenantId));
        agent.setLatestVersion(0);
        agent.setStatus("active");
        agent.setIsDefault(Boolean.FALSE);
        agent.setVisibility("private");
        agent.setCreatedAt(now);
        agent.setUpdatedAt(now);
        agentMapper.insert(agent);
        LOGGER.info("agent created id={} name={} createdBy={} createdAt={}",
                agent.getId(), agent.getName(), agent.getOwnerUserId(),
                agent.getCreatedAt());
        return agent;
    }

    @Override
    public AgentVersion saveDraft(long agentId, String profileYaml, String userId) {
        Agent agent = agentMapper.selectById(agentId);
        if (agent == null) {
            throw new IllegalArgumentException("agent not found: " + agentId);
        }
        int next = listVersions(agentId).stream()
                .map(AgentVersion::getVersion)
                .mapToInt(v -> v == null ? 0 : v)
                .max()
                .orElse(0) + 1;
        LocalDateTime now = LocalDateTime.now();
        AgentVersion version = new AgentVersion();
        version.setAgentId(agentId);
        version.setVersion(next);
        version.setProfileYaml(require(profileYaml, "profileYaml"));
        version.setContentHash(PlatformIds.sha256(profileYaml));
        version.setStatus("draft");
        version.setCreatedBy(empty(userId));
        version.setCreatedAt(now);
        agentVersionMapper.insert(version);
        agent.setUpdatedAt(now);
        agentMapper.updateById(agent);
        LOGGER.info("agent version draft created id={} agentId={} version={} createdBy={} createdAt={}",
                version.getId(), agentId, next, version.getCreatedBy(), version.getCreatedAt());
        return version;
    }

    @Override
    public AgentVersion publish(long agentVersionId) {
        return publish(agentVersionId, "");
    }

    @Override
    public AgentVersion publish(long agentVersionId, String userId) {
        AgentVersion version = agentVersionMapper.selectById(agentVersionId);
        if (version == null) {
            throw new IllegalArgumentException("agent version not found: " + agentVersionId);
        }
        if (!"draft".equals(version.getStatus()) && !"published".equals(version.getStatus())) {
            throw new IllegalStateException("cannot publish status=" + version.getStatus());
        }
        LocalDateTime now = LocalDateTime.now();
        version.setStatus("published");
        version.setPublishedAt(now);
        version.setPublishedBy(empty(userId));
        agentVersionMapper.updateById(version);

        Agent agent = agentMapper.selectById(version.getAgentId());
        if (agent == null) {
            throw new IllegalArgumentException("agent not found: " + version.getAgentId());
        }
        int ver = version.getVersion() == null ? 0 : version.getVersion();
        int latest = agent.getLatestVersion() == null ? 0 : agent.getLatestVersion();
        if (ver > latest) {
            agent.setLatestVersion(ver);
        }
        agent.setUpdatedAt(now);
        agentMapper.updateById(agent);
        LOGGER.info("agent version published id={} agentId={} version={} publishedBy={} "
                        + "publishedAt={} createdBy={} createdAt={}",
                version.getId(), version.getAgentId(), version.getVersion(),
                version.getPublishedBy(), version.getPublishedAt(), version.getCreatedBy(),
                version.getCreatedAt());
        return version;
    }

    @Override
    public AgentVersion requirePublished(long agentVersionId) {
        AgentVersion version = agentVersionMapper.selectById(agentVersionId);
        if (version == null) {
            throw new IllegalArgumentException("agent version not found: " + agentVersionId);
        }
        if (!"published".equals(version.getStatus())) {
            throw new IllegalStateException("agent version not published: " + agentVersionId);
        }
        return version;
    }

    @Override
    public Agent findAgent(long agentId) {
        return agentMapper.selectById(agentId);
    }

    @Override
    public AgentVersion findVersion(long agentVersionId) {
        return agentVersionMapper.selectById(agentVersionId);
    }

    @Override
    public List<Agent> listMine(String userId) {
        return agentMapper.selectList(new LambdaQueryWrapper<Agent>()
                .eq(Agent::getOwnerUserId, empty(userId))
                .orderByDesc(Agent::getId));
    }

    @Override
    public IPage<Agent> pageMine(String userId, long pageNum, long pageSize) {
        long pageNo = pageNum <= 0 ? 1 : pageNum;
        long size = pageSize <= 0 ? 20 : Math.min(pageSize, 100);
        return agentMapper.selectPage(new Page<>(pageNo, size), new LambdaQueryWrapper<Agent>()
                .eq(Agent::getOwnerUserId, empty(userId))
                .orderByDesc(Agent::getId));
    }

    @Override
    public List<AgentVersion> listVersions(long agentId) {
        return agentVersionMapper.selectList(new LambdaQueryWrapper<AgentVersion>()
                .eq(AgentVersion::getAgentId, agentId)
                .orderByDesc(AgentVersion::getVersion));
    }

    private static String require(String v, String field) {
        if (!StringUtils.hasText(v)) {
            throw new IllegalArgumentException(field + " required");
        }
        return v;
    }

    private static String empty(String s) {
        return s == null ? "" : s;
    }
}
