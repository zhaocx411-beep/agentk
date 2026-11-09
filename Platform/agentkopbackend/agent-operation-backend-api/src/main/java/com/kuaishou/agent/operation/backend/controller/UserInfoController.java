package com.kuaishou.agent.operation.backend.controller;

import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import com.kuaishou.agent.operation.backend.common.interceptor.LoginInterceptor;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.dto.UserInfoResponse;
import com.kuaishou.agent.operation.backend.platform.service.UserInfoService;
import com.kuaishou.security.accessproxy.v1.model.ApPrincipal;

/**
 * 当前登录用户信息。
 */
@RestController
@RequestMapping("/agent/operation/backend/user")
public class UserInfoController {

    private final UserInfoService userInfoService;

    public UserInfoController(UserInfoService userInfoService) {
        this.userInfoService = userInfoService;
    }

    @GetMapping("/info")
    public Message<UserInfoResponse> info() {
        ApPrincipal principal = LoginInterceptor.getThreadLocal().get();
        String username;
        String name;
        if (principal == null) {
            username = "admin";
            name = "admin";
        } else {
            username = principal.getUsername() != null ? principal.getUsername() : "admin";
            name = principal.getName() != null ? principal.getName() : username;
        }
        return Message.ok(userInfoService.build(username, name));
    }
}
