package com.kuaishou.klingai.agentk.storyboard.apiserver;

import com.kuaishou.klingai.agentk.api.v1.CancelExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.CancelExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.CreateSessionRequest;
import com.kuaishou.klingai.agentk.api.v1.CreateSessionResponse;
import com.kuaishou.klingai.agentk.api.v1.GetExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.GetExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.ListCapabilitiesRequest;
import com.kuaishou.klingai.agentk.api.v1.ListCapabilitiesResponse;
import com.kuaishou.klingai.agentk.api.v1.ListEventsRequest;
import com.kuaishou.klingai.agentk.api.v1.ListEventsResponse;
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsRequest;
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsResponse;
import com.kuaishou.klingai.agentk.api.v1.ListPipelinesRequest;
import com.kuaishou.klingai.agentk.api.v1.ListPipelinesResponse;
import com.kuaishou.klingai.agentk.api.v1.ListSessionsRequest;
import com.kuaishou.klingai.agentk.api.v1.ListSessionsResponse;
import com.kuaishou.klingai.agentk.api.v1.StartExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.StartExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.WatchEventsRequest;
import com.kuaishou.klingai.agentk.api.v1.WhoAmIRequest;
import com.kuaishou.klingai.agentk.api.v1.WhoAmIResponse;
import com.kuaishou.klingai.agentk.client.AgentKClient;
import com.kuaishou.klingai.agentk.client.EventObserver;
import com.kuaishou.klingai.agentk.client.EventSubscription;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CompletionStage;
import java.util.function.Function;

final class FakeAgentKClient implements AgentKClient {
    Function<CreateSessionRequest, CreateSessionResponse> createSession;
    Function<StartExecutionRequest, StartExecutionResponse> startExecution;
    Function<GetExecutionRequest, GetExecutionResponse> getExecution;
    boolean closed;

    @Override
    public CompletionStage<CreateSessionResponse> createSession(CreateSessionRequest request) {
        return CompletableFuture.completedFuture(createSession.apply(request));
    }

    @Override
    public CompletionStage<StartExecutionResponse> startExecution(StartExecutionRequest request) {
        return CompletableFuture.completedFuture(startExecution.apply(request));
    }

    @Override
    public CompletionStage<GetExecutionResponse> getExecution(GetExecutionRequest request) {
        return CompletableFuture.completedFuture(getExecution.apply(request));
    }

    @Override
    public void close() {
        closed = true;
    }

    @Override
    public CompletionStage<ListSessionsResponse> listSessions(ListSessionsRequest request) {
        throw unsupported();
    }

    @Override
    public CompletionStage<CancelExecutionResponse> cancelExecution(CancelExecutionRequest request) {
        throw unsupported();
    }

    @Override
    public CompletionStage<ListEventsResponse> listEvents(ListEventsRequest request) {
        throw unsupported();
    }

    @Override
    public EventSubscription watchEvents(WatchEventsRequest request, EventObserver observer) {
        throw unsupported();
    }

    @Override
    public CompletionStage<ListPipelinesResponse> listPipelines(ListPipelinesRequest request) {
        throw unsupported();
    }

    @Override
    public CompletionStage<ListExecutionsResponse> listExecutions(ListExecutionsRequest request) {
        throw unsupported();
    }

    @Override
    public CompletionStage<ListCapabilitiesResponse> listCapabilities(ListCapabilitiesRequest request) {
        throw unsupported();
    }

    @Override
    public CompletionStage<WhoAmIResponse> whoAmI(WhoAmIRequest request) {
        throw unsupported();
    }

    private static UnsupportedOperationException unsupported() {
        return new UnsupportedOperationException("not used by this test");
    }
}
