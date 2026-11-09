package com.kuaishou.agent.operation.backend.platform.dto;

import lombok.Data;

@Data
public class UpdateSessionTitleRequest {
    /** 新标题，trim 后 1–120 字。 */
    private String title;
}
