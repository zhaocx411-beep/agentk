package com.kuaishou.agent.operation.backend.agentk.kconf;

// import com.kuaishou.kconf.client.Kconf;
// import com.kuaishou.kconf.client.Kconfs;

/**
 * agent-k 模型目录 KConf 入口。当前暂停订阅 klingai.agentk.models，接口改走硬编码名单。
 */
public final class AgentkModelsKconf {

    public static final String KEY = "klingai.agentk.models";

    /** 原始 YAML/文本；缺省空串，便于本地无 Kess 时退回 yml 兜底。 */
    // public static final Kconf<String> MODELS_RAW =
    //         Kconfs.ofString(KEY, "").build();

    private AgentkModelsKconf() {
    }

    public static String getRaw() {
        // String raw = MODELS_RAW.get();
        // return raw == null ? "" : raw;
        return "";
    }
}
