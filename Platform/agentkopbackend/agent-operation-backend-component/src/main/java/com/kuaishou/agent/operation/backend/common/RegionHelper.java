package com.kuaishou.agent.operation.backend.common;

import com.kuaishou.env.util.EnvUtils;

/**
 * Agent 运维后台统一的区域/环境判定。
 */
public final class RegionHelper {

    public static final String REGION_DOMESTIC = "domestic";
    public static final String REGION_OVERSEA = "oversea";
    public static final String ENV_DEV = "dev";
    public static final String ENV_PROD = "prod";

    private static final String ENV_KWS_SERVICE_REGION = "KWS_SERVICE_REGION";
    private static final String ENV_KWS_SERVICE_STAGE = "KWS_SERVICE_STAGE";
    private static final String PROD_STAGE = "PROD";
    private static final String STAGING_OVERSEA_IDC = "HB2";
    private static final String PROD_OVERSEA_IDC = "SGP";

    private RegionHelper() {
    }

    public static String getCurrentRegion() {
        String idc = System.getenv(ENV_KWS_SERVICE_REGION);
        if (isProdOrRegressionTest()) {
            return PROD_OVERSEA_IDC.equalsIgnoreCase(idc) ? REGION_OVERSEA : REGION_DOMESTIC;
        }
        return STAGING_OVERSEA_IDC.equalsIgnoreCase(idc) ? REGION_OVERSEA : REGION_DOMESTIC;
    }

    public static String getCurrentEnv() {
        return isProdOrRegressionTest() ? ENV_PROD : ENV_DEV;
    }

    public static boolean isOversea() {
        return REGION_OVERSEA.equals(getCurrentRegion());
    }

    public static boolean isDomestic() {
        return REGION_DOMESTIC.equals(getCurrentRegion());
    }

    public static boolean isProd() {
        return PROD_STAGE.equalsIgnoreCase(System.getenv(ENV_KWS_SERVICE_STAGE));
    }

    /**
     * 线上 prod 与线上回归（PRT）共用 prod 档位配置；staging 使用 dev。
     */
    public static boolean isProdOrRegressionTest() {
        return isProd() || EnvUtils.isProdRegressionTest();
    }

    public static String getCurrentConfigFileName() {
        return String.format("config/application-%s-%s.yml", getCurrentRegion(), getCurrentEnv());
    }
}
