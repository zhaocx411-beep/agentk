package com.kuaishou.agent.operation.backend.platform.dto;

import lombok.Data;

/**
 * 我的上传历史分页查询（body）。
 * <p>
 * {@code POST .../media/my-uploads/query}：忽略 body 中的 userId（若有），强制当前登录用户。
 */
@Data
public class MediaAttachmentHistoryQuery {

    /** image / video / audio / doc，可选 */
    private String mediaType;
    /** success / failed，可选；不传则不过滤 */
    private String status;
    /** 从 1 开始，默认 1 */
    private Integer page;
    /** 默认 20，上限 100 */
    private Integer pageSize;
}
