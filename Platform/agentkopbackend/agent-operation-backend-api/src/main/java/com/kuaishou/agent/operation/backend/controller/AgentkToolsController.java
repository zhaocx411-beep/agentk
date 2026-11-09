package com.kuaishou.agent.operation.backend.controller;

import java.util.List;

import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import com.kuaishou.agent.operation.backend.agentk.tools.AgentkToolInfo;
import com.kuaishou.agent.operation.backend.agentk.tools.AgentkToolsService;
import com.kuaishou.agent.operation.backend.agentk.tools.AgentkToolsService.ToolsSnapshot;
import com.kuaishou.agent.operation.backend.model.Message;

import lombok.Data;

/**
 * 中后台 / Capability Lab 工具名单：只查 agent-k ListCapabilities
 * （实时失败则读 {@code aop_agentk_capability}），不补 mock。
 * 返回 activity 与 workflow（每条带 {@code kind}）。
 */
@RestController
@RequestMapping("/agent/operation/backend")
public class AgentkToolsController {

    private final AgentkToolsService toolsService;

    public AgentkToolsController(AgentkToolsService toolsService) {
        this.toolsService = toolsService;
    }

    @GetMapping("/agentk/tools")
    public Message<ToolsView> listTools() {
        ToolsSnapshot snapshot = toolsService.catalogFromCapabilities();
        ToolsView view = new ToolsView();
        view.setSource(snapshot.getSource());
        view.setTools(snapshot.catalogItems());
        return Message.ok(view);
    }

    @Data
    public static class ToolsView {
        /** agentk / cache：均来自 ListCapabilities，无 mock */
        private String source;
        /** activity + workflow，每条带 {@code kind}。 */
        private List<AgentkToolInfo> tools;
    }
}
