package com.kuaishou.agent.operation.backend.platform.dao.mapper;

import java.util.List;
import java.util.Map;

import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Param;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import com.kuaishou.agent.operation.backend.platform.entity.ChatAsset;

@Mapper
public interface ChatAssetMapper extends BaseMapper<ChatAsset> {

    /**
     * 作品集筛选项：直接读 {@code aop_asset.agent_name} 去重。
     * {@code userId} 非空时仅本人资产；空则全站。
     * {@code publicOnly} 为 true 时只保留：public 智能体名、public Workflow 名/agentk_id、
     * 以及 {@code source_kind=api} 的 taskType（如 e-commerce-goods-studio）。
     */
    List<String> listDistinctBoundAgentNames(@Param("userId") String userId,
            @Param("publicOnly") boolean publicOnly);

    /**
     * 作品集筛选项：仅 session_kind=workflow 时联查 workflow 去重名。
     * {@code userId} 非空时仅本人资产；空则全站。
     */
    List<String> listDistinctBoundWorkflowNames(@Param("userId") String userId);

    /**
     * 按主键批量回写预签名 URL。{@code urls} 的 key 为 asset id、value 为新 URL。
     */
    int batchRefreshAssetUrls(@Param("urls") Map<Long, String> urls);
}
