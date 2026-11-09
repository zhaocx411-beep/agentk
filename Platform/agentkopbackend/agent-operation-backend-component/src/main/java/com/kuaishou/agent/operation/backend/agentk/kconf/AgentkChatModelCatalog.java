package com.kuaishou.agent.operation.backend.agentk.kconf;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * 平台硬编码的 chat 主模型名单，对齐 agent-k {@code internal/modelcontract/contract.go}
 * 中 {@code FamilyChat} 条目。
 */
public final class AgentkChatModelCatalog {

    public static final class Entry {
        private final String name;
        private final AgentkChatAttachments attachments;

        private Entry(String name, AgentkChatAttachments attachments) {
            this.name = name;
            this.attachments = attachments;
        }

        public String getName() {
            return name;
        }

        public AgentkChatAttachments getAttachments() {
            return attachments;
        }
    }

    private static final List<Entry> ENTRIES = Collections.unmodifiableList(Arrays.asList(
            none("deepseek-v4-pro"),
            none("deepseek-v4-flash"),
            image("qwen3.5-plus"),
            none("qwen3.7-max"),
            image("gpt-5.4"),
            image("claude-sonnet-4-6"),
            image("claude-haiku-4-5"),
            none("glm-5.1"),
            image("kimi-k2.6"),
            all("gemini-3.1-pro-preview"),
            all("gemini-3-flash-preview"),
            all("gemini-3.1-flash-lite")));

    private static final Map<String, Entry> BY_NAME = byName();

    private AgentkChatModelCatalog() {
    }

    public static List<Entry> entries() {
        return ENTRIES;
    }

    public static List<String> names() {
        List<String> names = new ArrayList<>(ENTRIES.size());
        for (Entry entry : ENTRIES) {
            names.add(entry.getName());
        }
        return names;
    }

    public static AgentkChatAttachments attachmentsOf(String name) {
        Entry entry = BY_NAME.get(name);
        return entry == null ? AgentkChatAttachments.none() : copy(entry.getAttachments());
    }

    private static Entry none(String name) {
        return new Entry(name, AgentkChatAttachments.none());
    }

    private static Entry image(String name) {
        return new Entry(name, AgentkChatAttachments.imageOnly());
    }

    private static Entry all(String name) {
        return new Entry(name, AgentkChatAttachments.all());
    }

    private static Map<String, Entry> byName() {
        Map<String, Entry> map = new LinkedHashMap<>();
        for (Entry entry : ENTRIES) {
            map.put(entry.getName(), entry);
        }
        return Collections.unmodifiableMap(map);
    }

    private static AgentkChatAttachments copy(AgentkChatAttachments source) {
        if (source == null) {
            return AgentkChatAttachments.none();
        }
        if (AgentkChatAttachments.MODE_ALL.equals(source.getMode())) {
            return AgentkChatAttachments.all();
        }
        if (AgentkChatAttachments.MODE_IMAGE.equals(source.getMode())) {
            return AgentkChatAttachments.imageOnly();
        }
        return AgentkChatAttachments.none();
    }
}
