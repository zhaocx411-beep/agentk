package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import lombok.Data;

/** 与 agentk-eval checks 语义对齐的在线判定规则。 */
@Data
public class AgentKRegressionChecksDto {

    /** 缺省 true：要求 run.status = completed。 */
    private Boolean completed;
    /** 最后一条 assistant 文本须包含的子串（区分大小写）。 */
    private List<String> contains;
    /** 指定工具至少成功调用一次。 */
    private String toolCalled;
}
