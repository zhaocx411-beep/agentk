package com.kuaishou.agent.operation.backend.agentk.kconf;

import lombok.Data;

/**
 * 模型目录条目的<strong>可回显</strong>视图（对齐 krow-pay {@code PlanInfo} 角色）。
 * <p>
 * 只含 name / kind / api / description / 派生 tool / attachments，不含 apiKey、headers、accessKey 等凭证。
 */
@Data
public class AgentkModelInfo {

    /** 目录条目名，也是 profile / toolDefaults 里引用的名字。 */
    private String name;

    /**
     * 能力类别：chat（缺省）/ video / image / image_edit / media_understand /
     * search / tts / asr。
     */
    private String kind;

    /** 协议方言；video 缺省 kling。chat 常为空。 */
    private String api;

    /** 给 LLM 看的候选差异描述；可空。 */
    private String description;

    /**
     * 由 kind+api 派生的能力工具名；chat 条目为 null。
     * 例如 video+kling-t2v → kling_text_to_video。
     */
    private String tool;

    /**
     * 主对话 {@code attachments} 能力。仅 chat 条目有值；其它 kind 为 null。
     * {@code mode=none} 不能带附件；{@code image} 只能图片；{@code all} 图/视频/音频都可用。
     */
    private AgentkChatAttachments attachments;
}
