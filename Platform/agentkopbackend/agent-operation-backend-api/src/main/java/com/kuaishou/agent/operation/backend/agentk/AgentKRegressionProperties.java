package com.kuaishou.agent.operation.backend.agentk;

import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * 在线回归编排超时配置。
 */
@ConfigurationProperties(prefix = "agent.operation.backend.agentk.regression")
public class AgentKRegressionProperties {

    private long agentTimeoutMs = 120_000;
    private long workflowTimeoutMs = 600_000;
    private long pollIntervalMs = 500;
    private long jobWaitTimeoutMs = 600_000;

    public long getAgentTimeoutMs() {
        return agentTimeoutMs;
    }

    public void setAgentTimeoutMs(long agentTimeoutMs) {
        this.agentTimeoutMs = agentTimeoutMs;
    }

    public long getWorkflowTimeoutMs() {
        return workflowTimeoutMs;
    }

    public void setWorkflowTimeoutMs(long workflowTimeoutMs) {
        this.workflowTimeoutMs = workflowTimeoutMs;
    }

    public long getPollIntervalMs() {
        return pollIntervalMs;
    }

    public void setPollIntervalMs(long pollIntervalMs) {
        this.pollIntervalMs = pollIntervalMs;
    }

    public long getJobWaitTimeoutMs() {
        return jobWaitTimeoutMs;
    }

    public void setJobWaitTimeoutMs(long jobWaitTimeoutMs) {
        this.jobWaitTimeoutMs = jobWaitTimeoutMs;
    }
}
