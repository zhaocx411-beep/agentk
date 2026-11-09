package com.kuaishou.agent.operation.backend.platform.kconf;

import java.util.ArrayList;
import java.util.List;

import lombok.Data;

/**
 * 管理回看全局管理员白名单（KConf JSON）。
 * <p>
 * KConf key：{@code mmu.agentkop.adminChatReview}
 * <pre>
 * {
 *   "enabled": true,
 *   "adminUsernames": ["zhangsan", "lisi"]
 * }
 * </pre>
 */
@Data
public class AdminChatReviewConfig {
    /** false 时忽略本 KConf 名单（仍可读 DB / owner）。 */
    private boolean enabled = true;
    /** AccessProxy username 列表。 */
    private List<String> adminUsernames = new ArrayList<>();
}
