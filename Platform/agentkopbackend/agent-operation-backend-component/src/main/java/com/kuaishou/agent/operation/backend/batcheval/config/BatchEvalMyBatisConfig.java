package com.kuaishou.agent.operation.backend.batcheval.config;

import org.mybatis.spring.annotation.MapperScan;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.context.annotation.Configuration;
import org.springframework.transaction.annotation.EnableTransactionManagement;

@Configuration
@ConditionalOnProperty(name = "batcheval.store", havingValue = "mybatis", matchIfMissing = true)
@EnableTransactionManagement
@MapperScan("com.kuaishou.agent.operation.backend.batcheval.mapper.persistence")
public class BatchEvalMyBatisConfig {
}
