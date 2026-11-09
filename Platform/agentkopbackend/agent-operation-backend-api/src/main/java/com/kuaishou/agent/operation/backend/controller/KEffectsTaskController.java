package com.kuaishou.agent.operation.backend.controller;

import java.util.List;

import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.keffects.dto.EcommerceOutfitReplicationVoiceView;
import com.kuaishou.agent.operation.backend.keffects.dto.KEffectsCreateTaskRequest;
import com.kuaishou.agent.operation.backend.keffects.ecommerce.EcommerceOutfitReplicationVoicePack;
import com.kuaishou.agent.operation.backend.keffects.task.KEffectsTaskFacade;
import com.kuaishou.agent.operation.backend.model.Message;

/**
 * KEffects 统一任务入口：创建 / 查询 / 取消均通过 {@code taskType} 区分业务能力。
 * <p>
 * 支持的 {@code taskType}：
 * <ul>
 *   <li>{@code e-commerce-goods-scraper} — 商品爬取（kling-aigc-engine）</li>
 *   <li>{@code e-commerce-goods-studio} — 商品 studio（kling-aigc-engine；平台入参 {@code contents} 对象，出站转数组）</li>
 *   <li>{@code e-commerce-clothing-dupe} — 服装一键复刻（kling-aigc-engine，{@code contents} 为数组）</li>
 * </ul>
 * 创建 / 查询 / 取消均打 {@code kling-aigc-engine}。
 * 查询：按 taskId 路由（{@code ecs-*} / {@code egs-*} / clothing-dupe UUID 均 → kling-aigc-engine）。
 * 取消：仅 UUID（clothing-dupe）支持；{@code ecs-*} / {@code egs-*} 上游不支持取消。
 */
@RestController
@RequestMapping("/agent/operation/backend/platform/keffects")
public class KEffectsTaskController {

    private final KEffectsTaskFacade taskFacade;

    public KEffectsTaskController(KEffectsTaskFacade taskFacade) {
        this.taskFacade = taskFacade;
    }

    @PostMapping("/tasks")
    public Message<JsonNode> createTask(@RequestBody KEffectsCreateTaskRequest request) {
        return taskFacade.createTask(request);
    }

    @GetMapping("/tasks/{taskId}")
    public Message<JsonNode> getTask(@PathVariable("taskId") String taskId,
            @RequestParam(value = "taskType", required = false) String taskType) {
        return taskFacade.getTask(taskId, taskType);
    }

    @DeleteMapping("/tasks/{taskId}")
    public Message<JsonNode> cancelTask(@PathVariable("taskId") String taskId,
            @RequestParam(value = "taskType", required = false) String taskType) {
        return taskFacade.cancelTask(taskId, taskType);
    }

    /**
     * 服装复刻配音包选项（{@code displayName} + {@code voiceId}），供 {@code e-commerce-clothing-dupe} 选用。
     */
    @GetMapping("/clothing-dupe/voice-packs")
    public Message<List<EcommerceOutfitReplicationVoiceView>> listClothingDupeVoicePacks() {
        return Message.ok(EcommerceOutfitReplicationVoicePack.options());
    }
}
