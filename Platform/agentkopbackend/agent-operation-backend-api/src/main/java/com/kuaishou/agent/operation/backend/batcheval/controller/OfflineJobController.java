package com.kuaishou.agent.operation.backend.batcheval.controller;

import java.util.List;

import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.ResponseStatus;
import org.springframework.web.bind.annotation.RestController;

import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.AgentConfigView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetExportResult;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.OfflineJobCellView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.OfflineJobView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.SubmitOfflineJobRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.PagedResult;
import com.kuaishou.agent.operation.backend.batcheval.service.OfflineJobService;
import com.kuaishou.agent.operation.backend.common.interceptor.LoginInterceptor;
import com.kuaishou.agent.operation.backend.common.web.ContentDispositions;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.security.accessproxy.v1.model.ApPrincipal;

@RestController
@RequestMapping("/agent/operation/backend")
public class OfflineJobController {

    private final OfflineJobService offlineJobService;

    public OfflineJobController(OfflineJobService offlineJobService) {
        this.offlineJobService = offlineJobService;
    }

    @GetMapping("/agent-config")
    public Message<AgentConfigView> agentConfig() {
        return Message.ok(offlineJobService.getAgentConfig(currentUsername()));
    }

    @PostMapping("/offline-jobs/submit")
    @ResponseStatus(HttpStatus.ACCEPTED)
    public Message<OfflineJobView> submit(@RequestBody SubmitOfflineJobRequest request) {
        return Message.ok(offlineJobService.submit(request, currentUsername()));
    }

    @GetMapping("/offline-jobs/query")
    public Message<PagedResult<List<OfflineJobView>>> query(
            @RequestParam(value = "current", defaultValue = "1") int current,
            @RequestParam(value = "pageSize", defaultValue = "10") int pageSize) {
        return Message.ok(offlineJobService.list(current, pageSize));
    }

    @GetMapping("/offline-jobs/detail")
    public Message<OfflineJobView> detail(@RequestParam("bizId") String bizId) {
        return Message.ok(offlineJobService.get(requireBizId(bizId)));
    }

    @GetMapping("/offline-jobs/cells/query")
    public Message<List<OfflineJobCellView>> queryCells(@RequestParam("bizId") String bizId) {
        return Message.ok(offlineJobService.listCells(requireBizId(bizId)));
    }

    /** 查询单条用例中间过程：摘要 + 分页事件。漫剧事件可能上千条，不能一次打满响应。 */
    @GetMapping("/offline-jobs/cells/trace/query")
    public Message<JsonNode> queryTrace(@RequestParam("bizId") String bizId,
            @RequestParam("taskKey") String taskKey,
            @RequestParam(value = "current", defaultValue = "1") int current,
            @RequestParam(value = "pageSize", defaultValue = "500") int pageSize) {
        if (taskKey == null || taskKey.trim().isEmpty()) {
            throw new IllegalArgumentException("taskKey 不能为空");
        }
        return Message.ok(offlineJobService.getTrace(requireBizId(bizId), taskKey, current, pageSize));
    }

    /** 下载离线任务跑数结果（JSON：metrics、产物、以及每条用例从事件表取出的完整中间过程）。 */
    @GetMapping("/offline-jobs/export")
    public ResponseEntity<byte[]> export(@RequestParam("bizId") String bizId) {
        DatasetExportResult exported = offlineJobService.export(requireBizId(bizId));
        MediaType contentType = MediaType.parseMediaType(exported.getContentType());
        return ResponseEntity.ok()
                .contentType(contentType)
                .header(HttpHeaders.CONTENT_DISPOSITION,
                        ContentDispositions.attachment(exported.getFilename(), "offline-job-results.json"))
                .header(ContentDispositions.HEADER_FILE_NAME,
                        ContentDispositions.fileNameHeader(exported.getFilename(), "offline-job-results.json"))
                .header(HttpHeaders.CACHE_CONTROL, "no-store, no-cache, must-revalidate")
                .header(HttpHeaders.PRAGMA, "no-cache")
                .body(exported.getBody());
    }

    @DeleteMapping("/offline-jobs/delete")
    public Message<?> delete(@RequestParam("bizId") String bizId) {
        offlineJobService.delete(requireBizId(bizId));
        return Message.ok();
    }

    private static String requireBizId(String bizId) {
        if (bizId == null || bizId.trim().isEmpty()) {
            throw new IllegalArgumentException("bizId 不能为空");
        }
        return bizId;
    }

    private static String currentUsername() {
        ApPrincipal principal = LoginInterceptor.getThreadLocal().get();
        return principal == null || principal.getUsername() == null ? "admin" : principal.getUsername();
    }
}
