package com.kuaishou.agent.operation.backend.platform.dto;

import lombok.Data;

@Data
public class GrantAdminRequest {
    /** AccessProxy username */
    private String userId;
    /** 默认 chat_reviewer；也可传 ADMIN / SUPER_ADMIN */
    private String role;
}
