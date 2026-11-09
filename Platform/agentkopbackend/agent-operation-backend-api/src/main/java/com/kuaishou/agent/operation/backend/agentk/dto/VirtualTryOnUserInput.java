package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import lombok.Data;

/**
 * 虚拟试穿结构化输入。前端两个上传位拿到 URL 后填这里，不要把角色混进 text。
 */
@Data
public class VirtualTryOnUserInput {

    /** 商品图 URL，必填（可多张）。 */
    private List<String> productImageUrls;

    /** 模特图 URL，可选。 */
    private String modelImageUrl;

    /** 用户补充要求（模特气质、场景、画幅等）。空则回退到请求体 {@code text}。 */
    private String notes;

    /** 可选。image 或 video。 */
    private String outputType;
}
