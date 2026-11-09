package com.kuaishou.agent.operation.backend.batcheval.controller;

import java.io.IOException;
import java.util.List;

import org.springframework.http.HttpHeaders;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PatchMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.multipart.MultipartFile;

import com.kuaishou.agent.operation.backend.agentk.AgentKIdentityResolver;
import com.kuaishou.agent.operation.backend.auth.AgentOperationAuthProperties;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.CreateDatasetTaskRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetExportResult;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetSummaryView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetTaskView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetUploadRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.UpdateDatasetRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.UpdateDatasetTaskRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetListFilter;
import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetTaskListFilter;
import com.kuaishou.agent.operation.backend.batcheval.dto.PagedResult;
import com.kuaishou.agent.operation.backend.batcheval.service.DatasetService;
import com.kuaishou.agent.operation.backend.batcheval.support.DatasetUsages;
import com.kuaishou.agent.operation.backend.common.interceptor.LoginInterceptor;
import com.kuaishou.agent.operation.backend.common.web.ContentDispositions;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.platform.service.PlatformAdminService;
import com.kuaishou.security.accessproxy.v1.model.ApPrincipal;

@RestController
@RequestMapping("/agent/operation/backend/datasets")
public class DatasetController {

    private final DatasetService datasetService;
    private final PlatformAdminService platformAdminService;
    private final AgentKIdentityResolver identityResolver;
    private final AgentOperationAuthProperties authProperties;

    public DatasetController(DatasetService datasetService,
            PlatformAdminService platformAdminService,
            AgentKIdentityResolver identityResolver,
            AgentOperationAuthProperties authProperties) {
        this.datasetService = datasetService;
        this.platformAdminService = platformAdminService;
        this.identityResolver = identityResolver;
        this.authProperties = authProperties;
    }

    /**
     * {@code usage} 必填：{@code EVAL}=离线跑数，{@code TEMPLATE}=素材/模版。
     * TEMPLATE 必须带 {@code pipelineName}，且需要平台管理员。
     */
    @PostMapping(value = "/upload", consumes = MediaType.MULTIPART_FORM_DATA_VALUE)
    public Message<DatasetSummaryView> upload(@RequestParam("file") MultipartFile file,
            @RequestParam("datasetType") String datasetType,
            @RequestParam("name") String name,
            @RequestParam(value = "description", required = false) String description,
            @RequestParam(value = "version", required = false) String version,
            @RequestParam(value = "taskInstruction", required = false) String taskInstruction,
            @RequestParam("usage") String usage,
            @RequestParam(value = "pipelineName", required = false) String pipelineName) throws IOException {
        if (DatasetUsages.isTemplate(usage)) {
            assertPlatformAdmin();
        }
        DatasetUploadRequest request = new DatasetUploadRequest();
        request.setContent(file.getBytes());
        request.setFilename(file.getOriginalFilename());
        request.setDescription(description);
        request.setDatasetType(datasetType);
        request.setCreator(currentUsername());
        request.setName(name);
        request.setVersion(version);
        request.setTaskInstruction(taskInstruction);
        request.setUsage(usage);
        request.setPipelineName(pipelineName);
        return Message.ok(datasetService.upload(request));
    }

    /**
     * 管理员上传素材/模版 JSON。强制 {@code usage=TEMPLATE}，必须带 {@code pipelineName}。
     * 落库复用 {@code POST /datasets/upload}。
     */
    @PostMapping(value = "/templates/upload", consumes = MediaType.MULTIPART_FORM_DATA_VALUE)
    public Message<DatasetSummaryView> uploadTemplate(@RequestParam("file") MultipartFile file,
            @RequestParam("datasetType") String datasetType,
            @RequestParam("name") String name,
            @RequestParam("pipelineName") String pipelineName,
            @RequestParam(value = "description", required = false) String description,
            @RequestParam(value = "version", required = false) String version) throws IOException {
        assertPlatformAdmin();
        DatasetUploadRequest request = new DatasetUploadRequest();
        request.setContent(file.getBytes());
        request.setFilename(file.getOriginalFilename());
        request.setDescription(description);
        request.setDatasetType(datasetType);
        request.setCreator(identityResolver.resolve().getUserId());
        request.setName(name);
        request.setVersion(version);
        request.setPipelineName(pipelineName);
        return Message.ok(datasetService.uploadTemplate(request));
    }

    /**
     * 离线跑数页传 {@code usage=EVAL}；pipeline 入口的模版列表传
     * {@code usage=TEMPLATE&pipelineName=xxx}。
     */
    @GetMapping("/query")
    public Message<PagedResult<List<DatasetSummaryView>>> query(
            @RequestParam(value = "current", defaultValue = "1") int current,
            @RequestParam(value = "pageSize", defaultValue = "10") int pageSize,
            @RequestParam(value = "name", required = false) String name,
            @RequestParam(value = "datasetType", required = false) String datasetType,
            @RequestParam(value = "creator", required = false) String creator,
            @RequestParam(value = "usage", required = false) String usage,
            @RequestParam(value = "pipelineName", required = false) String pipelineName) {
        DatasetListFilter filter = new DatasetListFilter();
        filter.setName(name);
        filter.setDatasetType(datasetType);
        filter.setCreator(creator);
        filter.setUsage(usage);
        filter.setPipelineName(pipelineName);
        return Message.ok(datasetService.list(filter, current, pageSize));
    }

    @PatchMapping("/{id}")
    public Message<DatasetSummaryView> update(@PathVariable("id") long id,
            @RequestBody UpdateDatasetRequest request) {
        return Message.ok(datasetService.update(id, request));
    }

    /**
     * 查用例列表。{@code datasetId} / {@code usage} / {@code pipelineName} 至少传一个。
     * 素材库按主类目筛：{@code GET /tasks/query?datasetId=10&category=product_image}
     * <p>
     * 在线 Pipeline 入口可直接：
     * {@code GET /tasks/query?usage=TEMPLATE&pipelineName=generate_brand_ad}
     * <p>
     * 以下 AgentLoop 模版 pipeline 会自动随机返回 6 条用例（忽略 current/pageSize）：
     * 漫剧动态漫Agent、完整创作助手、图文视频创作、视频剪辑与生成。
     */
    @GetMapping("/tasks/query")
    public Message<PagedResult<List<DatasetTaskView>>> queryTasks(
            @RequestParam(value = "datasetId", required = false) Long datasetId,
            @RequestParam(value = "usage", required = false) String usage,
            @RequestParam(value = "pipelineName", required = false) String pipelineName,
            @RequestParam(value = "category", required = false) String category,
            @RequestParam(value = "current", defaultValue = "1") int current,
            @RequestParam(value = "pageSize", defaultValue = "10") int pageSize) {
        DatasetTaskListFilter filter = new DatasetTaskListFilter();
        filter.setDatasetId(datasetId);
        filter.setUsage(usage);
        filter.setPipelineName(pipelineName);
        filter.setCategory(category);
        return Message.ok(datasetService.listTasks(filter, current, pageSize));
    }

    @PostMapping("/{id}/tasks")
    public Message<DatasetTaskView> createTask(@PathVariable("id") long id,
            @RequestBody CreateDatasetTaskRequest request) {
        return Message.ok(datasetService.createTask(id, request));
    }

    @PatchMapping("/tasks/{taskId}")
    public Message<DatasetTaskView> updateTask(@PathVariable("taskId") long taskId,
            @RequestBody UpdateDatasetTaskRequest request) {
        return Message.ok(datasetService.updateTask(taskId, request));
    }

    @DeleteMapping("/tasks/{taskId}")
    public Message<?> deleteTask(@PathVariable("taskId") long taskId) {
        datasetService.deleteTask(taskId);
        return Message.ok();
    }

    /**
     * 下载数据集：导出当前库内最新内容（含 referImages / extraFields）。
     * {@code format=json|xlsx}，缺省 json。
     */
    @GetMapping("/{id}/export")
    public ResponseEntity<byte[]> export(@PathVariable("id") long id,
            @RequestParam(value = "format", required = false) String format) {
        DatasetExportResult exported = datasetService.export(id, format);
        MediaType contentType = MediaType.parseMediaType(exported.getContentType());
        return ResponseEntity.ok()
                .contentType(contentType)
                .header(HttpHeaders.CONTENT_DISPOSITION,
                        ContentDispositions.attachment(exported.getFilename(), "dataset.bin"))
                .header(ContentDispositions.HEADER_FILE_NAME,
                        ContentDispositions.fileNameHeader(exported.getFilename(), "dataset.bin"))
                .header(HttpHeaders.CACHE_CONTROL, "no-store, no-cache, must-revalidate")
                .header(HttpHeaders.PRAGMA, "no-cache")
                .body(exported.getBody());
    }

    @DeleteMapping("/delete/{id}")
    public Message<?> delete(@PathVariable("id") long id) {
        datasetService.delete(id);
        return Message.ok();
    }

    private void assertPlatformAdmin() {
        platformAdminService.assertPlatformAdmin(
                identityResolver.resolve().getUserId(), authProperties.isPermissionStub());
    }

    private static String currentUsername() {
        ApPrincipal principal = LoginInterceptor.getThreadLocal().get();
        return principal == null || principal.getUsername() == null ? "admin" : principal.getUsername();
    }
}
