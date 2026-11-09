package com.kuaishou.agent.operation.backend.agentk.tools;

import lombok.Data;

/**
 * agent-k 可选工具投影（UI 勾选 + 编译 Profile 用）。
 * <p>
 * 能力工具带 {@code ref=tool:&lt;capabilityId&gt;}；进程内本地工具（echo 等）
 * {@code ref} 为空，创建会话时只传 {@code {"name":"..."}}。
 */
@Data
public class AgentkToolInfo {

    /** 会话内工具名（喂给模型的 canonical name）。 */
    private String name;
    /** Profile 引用串，如 {@code tool:agentk.generate_image}；本地工具为 null。 */
    private String ref;
    private String description;
    /** blocking / background；mock 或缺省时可空。 */
    private String mode;
    /** activity / workflow；Lab 名单会两种都返回。 */
    private String kind;
    /** capability id；仅动态工具有。 */
    private String capabilityId;
    /** agentk = 来自 GET /v1/capabilities；cache = aop_agentk_capability；mock = 中后台兜底。 */
    private String source;
}
