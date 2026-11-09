package com.kuaishou.agent.operation.backend.agentk.tools;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

/**
 * agent-k 第一方工具的本地兜底名单。
 * <p>
 * {@link AgentkToolsService} 优先用 {@code GET /v1/capabilities}；
 * 仅当接口失败/空列表时整表退回本 mock（对齐 Core catalog 工具面，带 {@code tool:agentk.*}）。
 */
final class AgentkToolsCatalog {

    private AgentkToolsCatalog() {
    }

    /** capabilities 不可达时的第一方 catalog 兜底。 */
    static List<AgentkToolInfo> firstPartyMock() {
        List<AgentkToolInfo> out = new ArrayList<>();

        // CoreManifest 固定媒体工具
        out.add(capability("compose_video", "blocking", "拼接多段视频，可叠加音轨与字幕"));
        out.add(capability("probe_video", "blocking", "探测视频技术元信息"));
        out.add(capability("process_video", "blocking", "单视频裁剪/缩放/变速等处理"));
        out.add(capability("extract_audio", "blocking", "从视频提取音轨"));
        out.add(capability("extract_frame", "blocking", "从视频指定时间点抽帧"));

        // 生视频（后台 job）
        out.add(capability("kling_text_to_video", "background", "可灵文生视频"));
        out.add(capability("kling_image_to_video", "background", "可灵图生视频"));
        out.add(capability("kling_omni_video", "background", "可灵 Omni 视频"));
        out.add(capability("seedance_generate_video", "background", "Seedance 视频生成"));

        // Tool + Pipeline 双通道
        out.add(capability("generate_brand_ad", "background", "根据商品图与提示词生成品牌广告视频"));

        // 图 / 理解 / 检索 / 音频
        out.add(capability("generate_image", "blocking", "文生图"));
        out.add(capability("edit_image", "blocking", "参考图生图"));
        out.add(capability("understand_media", "blocking", "理解图片/视频并回答问题"));
        out.add(capability("understand_video_segment", "blocking", "按时间轴区间理解视频"));
        out.add(capability("web_search", "blocking", "网页搜索"));
        out.add(capability("generate_speech_minimax", "blocking", "MiniMax 文本转语音"));
        out.add(capability("generate_speech_qwen", "blocking", "Qwen 文本转语音"));
        out.add(capability("generate_speech_cosyvoice", "blocking", "CosyVoice 文本转语音"));
        out.add(capability("transcribe_audio", "blocking", "语音转文本"));
        out.add(capability("fetch_product_detail", "blocking", "抓取淘宝/天猫/Amazon 商品详情"));
        return Collections.unmodifiableList(out);
    }

    /** yml 声明的工具名并入名单（保序、去重）。 */
    static List<AgentkToolInfo> mergeConfigured(List<AgentkToolInfo> base, List<String> configuredNames) {
        List<AgentkToolInfo> out = new ArrayList<>(base);
        if (configuredNames == null || configuredNames.isEmpty()) {
            return out;
        }
        for (String raw : configuredNames) {
            if (raw == null || raw.trim().isEmpty()) {
                continue;
            }
            String name = raw.trim();
            if (containsName(out, name)) {
                continue;
            }
            out.add(local(name, "本地 batch-eval.tools 声明"));
        }
        return out;
    }

    /** capabilities 不可达时：完整 mock + yml 额外声明。 */
    static List<AgentkToolInfo> mergeConfigured(List<String> configuredNames) {
        return mergeConfigured(firstPartyMock(), configuredNames);
    }

    static List<String> namesOf(List<AgentkToolInfo> tools) {
        List<String> names = new ArrayList<>(tools.size());
        for (AgentkToolInfo tool : tools) {
            names.add(tool.getName());
        }
        return names;
    }

    static boolean containsName(List<AgentkToolInfo> tools, String name) {
        for (AgentkToolInfo tool : tools) {
            if (name.equals(tool.getName())) {
                return true;
            }
        }
        return false;
    }

    private static AgentkToolInfo local(String name, String description) {
        return mock(name, description, null, null, null);
    }

    private static AgentkToolInfo capability(String name, String mode, String description) {
        String capabilityId = "agentk." + name;
        return mock(name, description, "tool:" + capabilityId, capabilityId, mode);
    }

    private static AgentkToolInfo mock(String name, String description, String ref,
            String capabilityId, String mode) {
        AgentkToolInfo info = new AgentkToolInfo();
        info.setName(name);
        info.setRef(ref);
        info.setCapabilityId(capabilityId);
        info.setMode(mode);
        info.setDescription(description);
        info.setSource("mock");
        info.setKind("activity");
        return info;
    }

    /** 仅供单测断言用的固定子集。 */
    static List<String> coreVideoToolNames() {
        return Arrays.asList(
                "kling_text_to_video", "kling_image_to_video", "kling_omni_video",
                "seedance_generate_video");
    }
}
