package com.kuaishou.agent.operation.backend.platform.service;

import java.util.List;

import com.baomidou.mybatisplus.extension.plugins.pagination.Page;
import com.kuaishou.agent.operation.backend.platform.entity.WhiteListUser;

/**
 * 菜单白名单用户 Service。
 */
public interface WhiteListUserService {
    boolean isUserInWhiteList(String username);

    boolean isUserHasRole(String username, String requiredRole);

    List<String> getUserRoles(String username);

    String getUserMenu(String username);

    WhiteListUser addWhiteListUser(WhiteListUser request);

    WhiteListUser updateWhiteListUser(WhiteListUser request);

    int deleteWhiteListUsers(List<Long> ids);

    WhiteListUser getWhiteListUserById(Long id);

    Page<WhiteListUser> listWhiteListUsers(String username, Integer status, Integer pageNum, Integer pageSize);

    WhiteListUser addValidated(WhiteListUser request);

    WhiteListUser updateValidated(WhiteListUser request);

    WhiteListUser getByIdWithMenuNames(Long id);

    List<Long> parseIds(String ids);
}
