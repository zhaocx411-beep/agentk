package com.kuaishou.klingai.agentk.storyboard.apiserver;

import com.kuaishou.klingai.agentk.api.v1.Profile;

interface StoryboardAgentKClient extends AutoCloseable {
    String createSession(String title, Profile profile) throws AgentKCallException;

    String startPipeline(String sessionId, StoryboardArgs args) throws AgentKCallException;

    PipelineSnapshot getPipelineExecution(String sessionId, String executionId)
            throws AgentKCallException;

    @Override
    void close();
}

final class AgentKCallException extends Exception {
    AgentKCallException(String message) {
        super(message);
    }

    AgentKCallException(String message, Throwable cause) {
        super(message, cause);
    }
}
