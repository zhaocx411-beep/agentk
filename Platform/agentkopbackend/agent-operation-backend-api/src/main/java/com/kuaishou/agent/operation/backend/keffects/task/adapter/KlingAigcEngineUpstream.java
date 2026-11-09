package com.kuaishou.agent.operation.backend.keffects.task.adapter;

import com.kuaishou.agent.operation.backend.common.RegionHelper;

/**
 * kling-aigc-engine-prt 上游根地址（不含 /v1）。
 * <p>
 * 生产 / 线上回归：IDC 内网；测试 / 本地：测试域名。
 */
public final class KlingAigcEngineUpstream {

    public static final String TEST_BASE_URL = "https://kling-aigc-engine-prt.test.gifshow.com";
    public static final String PROD_BASE_URL = "http://kling-aigc-engine.internal";

    private KlingAigcEngineUpstream() {
    }

    public static String baseUrl() {
        return RegionHelper.isProdOrRegressionTest() ? PROD_BASE_URL : TEST_BASE_URL;
    }
}
