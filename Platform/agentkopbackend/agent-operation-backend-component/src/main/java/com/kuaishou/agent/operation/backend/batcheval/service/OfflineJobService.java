package com.kuaishou.agent.operation.backend.batcheval.service;

import java.util.List;

import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.AgentConfigView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetExportResult;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.OfflineJobCellView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.OfflineJobView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.SubmitOfflineJobRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.PagedResult;

public interface OfflineJobService {

    OfflineJobView submit(SubmitOfflineJobRequest request, String creator);

    PagedResult<List<OfflineJobView>> list(int current, int pageSize);

    OfflineJobView get(String bizId);

    List<OfflineJobCellView> listCells(String bizId);

    /**
     * cell 的 trace 详情：摘要 + 分页事件（按写入顺序），事件来自 {@code aop_session_event}。
     * 分页是给页面展示用的；一次导出完整中间过程请走 {@link #export(String)}。
     */
    JsonNode getTrace(String bizId, String taskKey, int current, int pageSize);

    /** 导出指定离线任务的跑数结果为 JSON（含每条用例从 {@code aop_session_event} 取出的完整 events）。 */
    DatasetExportResult export(String bizId);

    /** 离线页可选 agent：官方 + 当前用户个人（须有已发布版本）。 */
    AgentConfigView getAgentConfig(String userId);

    void delete(String bizId);
}
