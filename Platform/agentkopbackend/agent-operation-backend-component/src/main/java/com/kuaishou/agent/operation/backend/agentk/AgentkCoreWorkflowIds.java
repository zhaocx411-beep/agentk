package com.kuaishou.agent.operation.backend.agentk;

import java.util.Collections;
import java.util.LinkedHashSet;
import java.util.Set;

import org.springframework.util.StringUtils;

/**
 * agent-k CoreManifest 里登记为 workflow 的能力。
 * API proto 尚未带 kind 时，平台用这份名单避免把双通道 workflow 标成 activity。
 */
public final class AgentkCoreWorkflowIds {

    private static final Set<String> IDS;

    static {
        Set<String> ids = new LinkedHashSet<String>();
        ids.add("agentk.generate_brand_ad");
        ids.add("agentk.kling_image_to_video");
        ids.add("agentk.kling_omni_video");
        ids.add("agentk.kling_text_to_video");
        ids.add("agentk.seedance_generate_video");
        IDS = Collections.unmodifiableSet(ids);
    }

    private AgentkCoreWorkflowIds() {
    }

    public static boolean isWorkflow(String capabilityId) {
        return StringUtils.hasText(capabilityId) && IDS.contains(capabilityId.trim());
    }
}
