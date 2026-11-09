package com.kuaishou.klingai.agentk.client;

import java.util.concurrent.CompletionStage;

import com.kuaishou.klingai.agentk.api.v1.CancelExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.CancelExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.CreateSessionRequest;
import com.kuaishou.klingai.agentk.api.v1.CreateSessionResponse;
import com.kuaishou.klingai.agentk.api.v1.GetExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.GetExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.InvokeCapabilityRequest;
import com.kuaishou.klingai.agentk.api.v1.InvokeCapabilityResponse;
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

/** Authenticated, transport-independent access to the AgentK API. */
public interface AgentKClient extends AutoCloseable {
    CompletionStage<CreateSessionResponse> createSession(CreateSessionRequest request);

    CompletionStage<ListSessionsResponse> listSessions(ListSessionsRequest request);

    CompletionStage<StartExecutionResponse> startExecution(StartExecutionRequest request);

    CompletionStage<CancelExecutionResponse> cancelExecution(CancelExecutionRequest request);

    CompletionStage<ListEventsResponse> listEvents(ListEventsRequest request);

    EventSubscription watchEvents(WatchEventsRequest request, EventObserver observer);

    CompletionStage<ListPipelinesResponse> listPipelines(ListPipelinesRequest request);

    CompletionStage<ListExecutionsResponse> listExecutions(ListExecutionsRequest request);

    CompletionStage<GetExecutionResponse> getExecution(GetExecutionRequest request);

    CompletionStage<ListCapabilitiesResponse> listCapabilities(ListCapabilitiesRequest request);

    CompletionStage<InvokeCapabilityResponse> invokeCapability(InvokeCapabilityRequest request);

    CompletionStage<WhoAmIResponse> whoAmI(WhoAmIRequest request);

    @Override
    void close();
}
