package com.kuaishou.agent.operation.backend.platform.support;

/**
 * 平台官方示范资产约定（种子 SQL / OfficialDemo 接口共用）。
 */
public final class OfficialDemoAssets {

    /** 默认资产归属账号，不复制给每个用户。 */
    public static final String SYSTEM_OWNER = "platform_system";

    public static final String VISIBILITY_PRIVATE = "private";
    public static final String VISIBILITY_PUBLIC = "public";

    public static final String MODE_DEFAULT_AGENT = "default_agent";
    public static final String MODE_DEFAULT_WORKFLOW = "default_workflow";

    /**
     * 历史官方快速入口曾写入此值。{@code entry_source} 现在只区分 online / offline_batch，
     * 新会话一律走 {@link ChatEntrySources#ONLINE}。
     */
    @Deprecated
    public static final String ENTRY_SOURCE_OFFICIAL_QUICK = "official_quick";

    /** 落库 aop_chat_session.entry_source：Capability Lab 单次执行。 */
    public static final String ENTRY_SOURCE_CAPABILITY_LAB = "capability_lab";

    private OfficialDemoAssets() {
    }
}
