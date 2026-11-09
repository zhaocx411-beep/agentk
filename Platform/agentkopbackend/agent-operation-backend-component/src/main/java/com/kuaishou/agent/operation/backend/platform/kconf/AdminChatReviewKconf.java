package com.kuaishou.agent.operation.backend.platform.kconf;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

import org.springframework.util.StringUtils;

import com.kuaishou.kconf.client.Kconf;
import com.kuaishou.kconf.client.Kconfs;

/**
 * 管理回看管理员名单（KConf，预留）。
 * <p>
 * 当前环境暂无 KConf 目录，运行时授权以 DB {@code aop_platform_admin} 为准；
 * 本类暂不参与判定，目录可用后再接入。
 */
public final class AdminChatReviewKconf {

    public static final String KEY = "mmu.agentkop.adminChatReview";

    public static final Kconf<AdminChatReviewConfig> CONFIG = Kconfs
            .ofJson(KEY, defaultConfig(), AdminChatReviewConfig.class)
            .build();

    private AdminChatReviewKconf() {
    }

    public static AdminChatReviewConfig get() {
        AdminChatReviewConfig config = CONFIG.get();
        return config != null ? config : defaultConfig();
    }

    public static boolean isAdmin(String username) {
        if (!StringUtils.hasText(username)) {
            return false;
        }
        AdminChatReviewConfig config = get();
        if (!config.isEnabled()) {
            return false;
        }
        List<String> admins = config.getAdminUsernames();
        if (admins == null || admins.isEmpty()) {
            return false;
        }
        return admins.stream().anyMatch(username::equals);
    }

    public static List<String> listAdmins() {
        AdminChatReviewConfig config = get();
        if (!config.isEnabled() || config.getAdminUsernames() == null) {
            return Collections.emptyList();
        }
        return Collections.unmodifiableList(config.getAdminUsernames());
    }

    static AdminChatReviewConfig defaultConfig() {
        AdminChatReviewConfig config = new AdminChatReviewConfig();
        config.setEnabled(true);
        config.setAdminUsernames(new ArrayList<>());
        return config;
    }
}
