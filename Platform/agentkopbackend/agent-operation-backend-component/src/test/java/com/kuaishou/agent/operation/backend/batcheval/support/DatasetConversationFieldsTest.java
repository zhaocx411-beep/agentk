package com.kuaishou.agent.operation.backend.batcheval.support;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;

import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;

class DatasetConversationFieldsTest {

    @Test
    void shouldFillMissingTurnIndexInAppearanceOrder() {
        List<DatasetTask> tasks = new ArrayList<>();
        tasks.add(task("1", "conv-a", null));
        tasks.add(task("2", "conv-a", null));
        tasks.add(task("3", null, null));
        DatasetConversationFields.normalizeAndValidate(tasks);
        assertThat(tasks.get(0).getTurnIndex()).isEqualTo(1);
        assertThat(tasks.get(1).getTurnIndex()).isEqualTo(2);
        assertThat(tasks.get(2).getTurnIndex()).isNull();
        assertThat(tasks.get(2).getConversationKey()).isNull();
    }

    @Test
    void shouldFillSmallestUnusedTurnWhenSomeExplicit() {
        List<DatasetTask> tasks = new ArrayList<>();
        tasks.add(task("1", "conv-a", 1));
        tasks.add(task("2", "conv-a", null));
        DatasetConversationFields.normalizeAndValidate(tasks);
        assertThat(tasks.get(1).getTurnIndex()).isEqualTo(2);
    }

    @Test
    void shouldRejectDuplicateTurnInSameGroup() {
        List<DatasetTask> tasks = new ArrayList<>();
        tasks.add(task("1", "conv-a", 1));
        tasks.add(task("2", "conv-a", 1));
        assertThatThrownBy(() -> DatasetConversationFields.normalizeAndValidate(tasks))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("turnIndex 重复");
    }

    @Test
    void shouldRejectTurnWithoutConversationKey() {
        List<DatasetTask> tasks = new ArrayList<>();
        tasks.add(task("1", null, 1));
        assertThatThrownBy(() -> DatasetConversationFields.normalizeAndValidate(tasks))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("必须同时提供 conversationKey");
    }

    @Test
    void shouldReadJsonAliases() {
        Map<String, Object> map = new LinkedHashMap<>();
        map.put("conversation_id", "c1");
        map.put("turn", 2);
        DatasetTask task = new DatasetTask();
        DatasetConversationFields.applyFromTaskMap(task, map);
        assertThat(task.getConversationKey()).isEqualTo("c1");
        assertThat(task.getTurnIndex()).isEqualTo(2);
    }

    private static DatasetTask task(String key, String conversationKey, Integer turnIndex) {
        DatasetTask task = new DatasetTask();
        task.setTaskKey(key);
        task.setConversationKey(conversationKey);
        task.setTurnIndex(turnIndex);
        return task;
    }
}
