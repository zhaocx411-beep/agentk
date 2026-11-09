package com.kuaishou.agent.operation.backend.agentk.tryon;

import java.util.ArrayList;
import java.util.List;

import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.agentk.dto.AgentKPostMessageRequest;
import com.kuaishou.agent.operation.backend.agentk.dto.VirtualTryOnUserInput;

/**
 * 把商品图 / 模特图角色写进发给 agent-k 的 text，避免模型猜测哪张图是什么。
 * 用户原文 {@code text} 仍单独落库。
 */
public final class VirtualTryOnUserInputComposer {

    private VirtualTryOnUserInputComposer() {
    }

    public static boolean hasStructuredInput(AgentKPostMessageRequest request) {
        if (request == null) {
            return false;
        }
        return !productUrls(request).isEmpty() || StringUtils.hasText(modelUrl(request));
    }

    public static AgentKPostMessageRequest apply(AgentKPostMessageRequest request) {
        if (!hasStructuredInput(request)) {
            return request;
        }
        AgentKPostMessageRequest outbound = new AgentKPostMessageRequest();
        outbound.setText(compose(request));
        outbound.setTts(request.getTts());
        outbound.setAttachments(request.getAttachments());
        return outbound;
    }

    public static String compose(AgentKPostMessageRequest request) {
        List<String> products = productUrls(request);
        String model = modelUrl(request);
        String notes = notes(request);
        String outputType = outputType(request);

        StringBuilder body = new StringBuilder();
        body.append("请执行虚拟试穿。\n");
        body.append("【结构化输入】\n");
        body.append("商品图 productImageUrls:\n");
        if (products.isEmpty()) {
            body.append("（无）\n");
        } else {
            for (int i = 0; i < products.size(); i++) {
                body.append(i + 1).append(". ").append(products.get(i)).append('\n');
            }
        }
        body.append("模特图 modelImageUrl:\n");
        body.append(model.isEmpty() ? "（无）" : model).append('\n');
        if (!outputType.isEmpty()) {
            body.append("输出类型 outputType:\n");
            body.append(outputType).append('\n');
        }
        body.append("用户补充要求:\n");
        body.append(notes.isEmpty() ? "（无）" : notes).append('\n');
        body.append("【执行要求】\n");
        body.append("先识别商品图，再让模特穿上该商品；如果没有模特图，则先生成一个自然模特图。");
        return body.toString();
    }

    private static List<String> productUrls(AgentKPostMessageRequest request) {
        VirtualTryOnUserInput nested = request == null ? null : request.getUserInput();
        List<String> fromNested = collectUrls(nested == null ? null : nested.getProductImageUrls());
        if (!fromNested.isEmpty()) {
            return fromNested;
        }
        return collectUrls(request == null ? null : request.getProductImageUrls());
    }

    private static String modelUrl(AgentKPostMessageRequest request) {
        VirtualTryOnUserInput nested = request == null ? null : request.getUserInput();
        if (nested != null && StringUtils.hasText(nested.getModelImageUrl())) {
            return nested.getModelImageUrl().trim();
        }
        if (request != null && StringUtils.hasText(request.getModelImageUrl())) {
            return request.getModelImageUrl().trim();
        }
        return "";
    }

    private static String notes(AgentKPostMessageRequest request) {
        VirtualTryOnUserInput nested = request == null ? null : request.getUserInput();
        if (nested != null && StringUtils.hasText(nested.getNotes())) {
            return nested.getNotes().trim();
        }
        if (request != null && StringUtils.hasText(request.getText())) {
            return request.getText().trim();
        }
        return "";
    }

    private static String outputType(AgentKPostMessageRequest request) {
        VirtualTryOnUserInput nested = request == null ? null : request.getUserInput();
        if (nested != null && StringUtils.hasText(nested.getOutputType())) {
            return nested.getOutputType().trim();
        }
        if (request != null && StringUtils.hasText(request.getOutputType())) {
            return request.getOutputType().trim();
        }
        return "";
    }

    private static List<String> collectUrls(List<String> raw) {
        List<String> urls = new ArrayList<>();
        if (raw == null) {
            return urls;
        }
        for (String url : raw) {
            if (StringUtils.hasText(url)) {
                urls.add(url.trim());
            }
        }
        return urls;
    }
}
