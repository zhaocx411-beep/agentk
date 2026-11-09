package com.kuaishou.agent.operation.backend.controller;

import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkModelsService;
import com.kuaishou.agent.operation.backend.agentk.kconf.AgentkModelsService.ModelsView;
import com.kuaishou.agent.operation.backend.model.Message;

/**
 * agent-k 模型目录接口。当前 listModels 硬编码 FamilyChat 名单及 attachments 能力，
 * KConf 读取已注释。
 */
@RestController
@RequestMapping("/agent/operation/backend")
public class AgentkModelsController {

    private final AgentkModelsService agentkModelsService;

    public AgentkModelsController(AgentkModelsService agentkModelsService) {
        this.agentkModelsService = agentkModelsService;
    }

    /**
     * 获取 chat 主模型列表。条目含 name 与 attachments（none / image / all）。
     */
    @GetMapping("/agentk/models")
    public Message<ModelsView> listModels() {
        return Message.ok(agentkModelsService.listModels());
    }
}
