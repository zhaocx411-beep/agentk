package com.kuaishou.agent.operation.backend.batcheval.support;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.TreeSet;

import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;

/**
 * 数据集多轮字段：{@code conversationKey} 分桶，{@code turnIndex} 组内顺序。
 * 空键表示单轮；缺轮次时按该 key 出现顺序补最小未占用正整数。
 */
public final class DatasetConversationFields {

    public static final int MAX_CONVERSATION_KEY_LENGTH = 128;

    private DatasetConversationFields() {
    }

    public static void applyFromTaskMap(DatasetTask task, Map<?, ?> taskMap) {
        if (task == null || taskMap == null) {
            return;
        }
        task.setConversationKey(normalizeKey(firstNonBlank(
                stringOf(taskMap, "conversationKey"),
                stringOf(taskMap, "conversation_key"),
                stringOf(taskMap, "conversation_id"))));
        Object rawTurn = firstPresent(taskMap, "turnIndex", "turn_index", "turn");
        task.setTurnIndex(parseTurnIndex(rawTurn));
    }

    public static String normalizeKey(String raw) {
        if (raw == null) {
            return null;
        }
        String key = raw.trim();
        if (key.isEmpty()) {
            return null;
        }
        if (key.length() > MAX_CONVERSATION_KEY_LENGTH) {
            throw new IllegalArgumentException(
                    "conversationKey 长度不能超过 " + MAX_CONVERSATION_KEY_LENGTH);
        }
        for (int i = 0; i < key.length(); i++) {
            if (key.charAt(i) < 32) {
                throw new IllegalArgumentException("conversationKey 含非法控制字符");
            }
        }
        return key;
    }

    public static Integer parseTurnIndex(Object raw) {
        if (raw == null) {
            return null;
        }
        if (raw instanceof Integer) {
            return requirePositiveTurn((Integer) raw);
        }
        if (raw instanceof Long) {
            long value = (Long) raw;
            if (value > Integer.MAX_VALUE || value < Integer.MIN_VALUE) {
                throw new IllegalArgumentException("turnIndex 非法: " + raw);
            }
            return requirePositiveTurn((int) value);
        }
        if (raw instanceof Number) {
            double value = ((Number) raw).doubleValue();
            if (Double.isNaN(value) || value != Math.rint(value)) {
                throw new IllegalArgumentException("turnIndex 必须是正整数，当前值: " + raw);
            }
            return requirePositiveTurn((int) value);
        }
        String text = String.valueOf(raw).trim();
        if (text.isEmpty()) {
            return null;
        }
        if (text.endsWith(".0")) {
            text = text.substring(0, text.length() - 2);
        }
        try {
            return requirePositiveTurn(Integer.parseInt(text));
        } catch (NumberFormatException e) {
            throw new IllegalArgumentException("turnIndex 必须是正整数，当前值: " + raw, e);
        }
    }

    /**
     * 校验并补齐一组用例的轮次。会改写缺省 {@code turnIndex}。
     */
    public static void normalizeAndValidate(List<DatasetTask> tasks) {
        if (tasks == null || tasks.isEmpty()) {
            return;
        }
        for (DatasetTask task : tasks) {
            if (task == null) {
                continue;
            }
            task.setConversationKey(normalizeKey(task.getConversationKey()));
            if (task.getTurnIndex() != null) {
                requirePositiveTurn(task.getTurnIndex());
                if (task.getConversationKey() == null) {
                    throw new IllegalArgumentException(
                            "填写 turnIndex 时必须同时提供 conversationKey，task id=" + task.getTaskKey());
                }
            }
        }
        Map<String, List<DatasetTask>> groups = new LinkedHashMap<>();
        for (DatasetTask task : tasks) {
            if (task == null || task.getConversationKey() == null) {
                continue;
            }
            groups.computeIfAbsent(task.getConversationKey(), key -> new ArrayList<>()).add(task);
        }
        for (Map.Entry<String, List<DatasetTask>> entry : groups.entrySet()) {
            fillGroupTurns(entry.getKey(), entry.getValue());
        }
    }

    public static void copyToCell(DatasetTask task, OfflineJobCell cell) {
        if (task == null || cell == null) {
            return;
        }
        cell.setConversationKey(task.getConversationKey());
        cell.setTurnIndex(task.getTurnIndex());
    }

    private static void fillGroupTurns(String conversationKey, List<DatasetTask> group) {
        TreeSet<Integer> used = new TreeSet<>();
        for (DatasetTask task : group) {
            Integer turn = task.getTurnIndex();
            if (turn == null) {
                continue;
            }
            if (!used.add(turn)) {
                throw new IllegalArgumentException(
                        "同一 conversationKey 下 turnIndex 重复: " + conversationKey + " / " + turn);
            }
        }
        for (DatasetTask task : group) {
            if (task.getTurnIndex() != null) {
                continue;
            }
            int next = 1;
            while (used.contains(next)) {
                next++;
            }
            task.setTurnIndex(next);
            used.add(next);
        }
    }

    private static int requirePositiveTurn(int turn) {
        if (turn < 1) {
            throw new IllegalArgumentException("turnIndex 必须是 ≥1 的正整数，当前值: " + turn);
        }
        return turn;
    }

    private static String stringOf(Map<?, ?> map, String key) {
        Object value = map.get(key);
        if (value == null) {
            return null;
        }
        String text = String.valueOf(value).trim();
        return text.isEmpty() ? null : text;
    }

    private static String firstNonBlank(String... values) {
        if (values == null) {
            return null;
        }
        for (String value : values) {
            if (value != null && !value.trim().isEmpty()) {
                return value.trim();
            }
        }
        return null;
    }

    private static Object firstPresent(Map<?, ?> map, String... keys) {
        for (String key : keys) {
            if (map.containsKey(key) && map.get(key) != null) {
                return map.get(key);
            }
        }
        return null;
    }
}
