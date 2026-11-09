package com.kuaishou.agent.operation.backend.platform.service.impl;

import java.time.LocalDateTime;
import java.util.List;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.WorkflowMapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.WorkflowVersionMapper;
import com.kuaishou.agent.operation.backend.platform.entity.Workflow;
import com.kuaishou.agent.operation.backend.platform.entity.WorkflowVersion;
import com.kuaishou.agent.operation.backend.platform.service.WorkflowVersionService;
import com.kuaishou.agent.operation.backend.platform.support.PlatformIds;
import com.kuaishou.agent.operation.backend.platform.support.WorkflowPipelineRefs;

/**
 * Workflow 版本实现。
 */
@Service
public class WorkflowVersionServiceImpl implements WorkflowVersionService {

    private static final Logger LOGGER = LoggerFactory.getLogger(WorkflowVersionServiceImpl.class);

    private final WorkflowMapper workflowMapper;
    private final WorkflowVersionMapper workflowVersionMapper;
    private final ObjectMapper objectMapper;

    public WorkflowVersionServiceImpl(WorkflowMapper workflowMapper, WorkflowVersionMapper workflowVersionMapper,
            ObjectMapper objectMapper) {
        this.workflowMapper = workflowMapper;
        this.workflowVersionMapper = workflowVersionMapper;
        this.objectMapper = objectMapper;
    }

    @Override
    public Workflow createWorkflow(String name, String description, String userId, String tenantId) {
        LocalDateTime now = LocalDateTime.now();
        Workflow wf = new Workflow();
        wf.setName(require(name, "name"));
        wf.setDescription(empty(description));
        wf.setOwnerUserId(empty(userId));
        wf.setTenantId(empty(tenantId));
        wf.setLatestVersion(0);
        wf.setStatus("active");
        wf.setIsDefault(Boolean.FALSE);
        wf.setVisibility("private");
        wf.setCreatedAt(now);
        wf.setUpdatedAt(now);
        workflowMapper.insert(wf);
        LOGGER.info("workflow created id={} name={} createdBy={} createdAt={}",
                wf.getId(), wf.getName(), wf.getOwnerUserId(), wf.getCreatedAt());
        return wf;
    }

    @Override
    public WorkflowVersion saveDraft(long workflowId, String definitionJson, String userId) {
        Workflow wf = workflowMapper.selectById(workflowId);
        if (wf == null) {
            throw new IllegalArgumentException("workflow not found: " + workflowId);
        }
        int next = listVersions(workflowId).stream()
                .map(WorkflowVersion::getVersion)
                .mapToInt(v -> v == null ? 0 : v)
                .max()
                .orElse(0) + 1;
        LocalDateTime now = LocalDateTime.now();
        WorkflowVersion version = new WorkflowVersion();
        version.setWorkflowId(workflowId);
        version.setVersion(next);
        version.setDefinitionJson(require(definitionJson, "definitionJson"));
        version.setContentHash(PlatformIds.sha256(definitionJson));
        version.setStatus("draft");
        version.setCreatedBy(empty(userId));
        version.setCreatedAt(now);
        workflowVersionMapper.insert(version);
        syncAgentkId(wf, definitionJson, now);
        LOGGER.info("workflow version draft created id={} workflowId={} version={} createdBy={} createdAt={} agentkId={}",
                version.getId(), workflowId, next, version.getCreatedBy(), version.getCreatedAt(), wf.getAgentkId());
        return version;
    }

    @Override
    public WorkflowVersion publish(long workflowVersionId) {
        return publish(workflowVersionId, "");
    }

    @Override
    public WorkflowVersion publish(long workflowVersionId, String userId) {
        WorkflowVersion version = workflowVersionMapper.selectById(workflowVersionId);
        if (version == null) {
            throw new IllegalArgumentException("workflow version not found: " + workflowVersionId);
        }
        LocalDateTime now = LocalDateTime.now();
        version.setStatus("published");
        version.setPublishedAt(now);
        version.setPublishedBy(empty(userId));
        workflowVersionMapper.updateById(version);

        Workflow wf = workflowMapper.selectById(version.getWorkflowId());
        if (wf == null) {
            throw new IllegalArgumentException("workflow not found: " + version.getWorkflowId());
        }
        int ver = version.getVersion() == null ? 0 : version.getVersion();
        int latest = wf.getLatestVersion() == null ? 0 : wf.getLatestVersion();
        if (ver > latest) {
            wf.setLatestVersion(ver);
        }
        syncAgentkId(wf, version.getDefinitionJson(), now);
        LOGGER.info("workflow version published id={} workflowId={} version={} publishedBy={} publishedAt={} "
                        + "createdBy={} createdAt={} agentkId={}",
                version.getId(), version.getWorkflowId(), version.getVersion(), version.getPublishedBy(),
                version.getPublishedAt(), version.getCreatedBy(), version.getCreatedAt(), wf.getAgentkId());
        return version;
    }

    private void syncAgentkId(Workflow wf, String definitionJson, LocalDateTime now) {
        String agentkId = WorkflowPipelineRefs.extractAgentkId(definitionJson, objectMapper);
        wf.setAgentkId(agentkId);
        wf.setUpdatedAt(now);
        workflowMapper.updateById(wf);
    }

    @Override
    public WorkflowVersion requirePublished(long workflowVersionId) {
        WorkflowVersion version = workflowVersionMapper.selectById(workflowVersionId);
        if (version == null) {
            throw new IllegalArgumentException("workflow version not found: " + workflowVersionId);
        }
        if (!"published".equals(version.getStatus())) {
            throw new IllegalStateException("workflow version not published: " + workflowVersionId);
        }
        return version;
    }

    @Override
    public Workflow findWorkflow(long workflowId) {
        return workflowMapper.selectById(workflowId);
    }

    @Override
    public WorkflowVersion findVersion(long workflowVersionId) {
        return workflowVersionMapper.selectById(workflowVersionId);
    }

    @Override
    public List<Workflow> listMine(String userId) {
        return workflowMapper.selectList(new LambdaQueryWrapper<Workflow>()
                .eq(Workflow::getOwnerUserId, empty(userId))
                .orderByDesc(Workflow::getId));
    }

    @Override
    public List<WorkflowVersion> listVersions(long workflowId) {
        return workflowVersionMapper.selectList(new LambdaQueryWrapper<WorkflowVersion>()
                .eq(WorkflowVersion::getWorkflowId, workflowId)
                .orderByDesc(WorkflowVersion::getVersion));
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
