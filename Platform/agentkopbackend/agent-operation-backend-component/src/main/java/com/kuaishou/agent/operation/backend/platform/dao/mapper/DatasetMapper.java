package com.kuaishou.agent.operation.backend.platform.dao.mapper;

import org.apache.ibatis.annotations.Mapper;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import com.kuaishou.agent.operation.backend.platform.entity.Dataset;

@Mapper
public interface DatasetMapper extends BaseMapper<Dataset> {
}
