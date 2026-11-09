package com.kuaishou.agent.operation.backend.platform.dto;

import java.util.ArrayList;
import java.util.List;

import com.kuaishou.agent.operation.backend.platform.entity.AgentkCallLog;
import com.kuaishou.agent.operation.backend.platform.entity.ChatAsset;
import com.kuaishou.agent.operation.backend.platform.entity.ChatMessage;
import com.kuaishou.agent.operation.backend.platform.entity.ChatRun;
import com.kuaishou.agent.operation.backend.platform.entity.PipelineExec;
import com.kuaishou.agent.operation.backend.platform.entity.RunEvent;

import lombok.Data;

/**
 * 管理回看 Case：会话绑定 + 过程 + 产出。
 * <p>
 * 过程：{@code events} 为会话全部 {@code aop_session_event}（seq 升序）；
 * {@code agent_loop} 另含 runs / messages；pipelineExecs 与 assets 始终返回。
 * 产出：{@code assets}（{@code aop_asset}）以及 pipelineExecs.outputJson。
 */
@Data
public class ChatCaseReviewView {
    private ChatSessionDetailView session;
    private List<ChatRun> runs = new ArrayList<>();
    private List<ChatMessage> messages = new ArrayList<>();
    private List<RunEvent> events = new ArrayList<>();
    private List<PipelineExec> pipelineExecs = new ArrayList<>();
    private List<ChatAsset> assets = new ArrayList<>();
    private List<AgentkCallLog> callLogs = new ArrayList<>();
}
