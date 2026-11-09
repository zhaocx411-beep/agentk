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
import com.kuaishou.klingai.agentk.api.v1.ListPipelinesRequest;
import com.kuaishou.klingai.agentk.api.v1.ListPipelinesResponse;
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsRequest;
import com.kuaishou.klingai.agentk.api.v1.ListExecutionsResponse;
import com.kuaishou.klingai.agentk.api.v1.ListSessionsRequest;
import com.kuaishou.klingai.agentk.api.v1.ListSessionsResponse;
import com.kuaishou.klingai.agentk.api.v1.StartExecutionRequest;
import com.kuaishou.klingai.agentk.api.v1.StartExecutionResponse;
import com.kuaishou.klingai.agentk.api.v1.WatchEventsRequest;
import com.kuaishou.klingai.agentk.api.v1.WhoAmIRequest;
import com.kuaishou.klingai.agentk.api.v1.WhoAmIResponse;

/**
 * Authenticated, transport-independent access to the AgentK API.
 *
 * <p>Unary operations complete exceptionally when the transport or AgentK rejects a request.
 * Cancelling a returned stage cancels the corresponding in-flight operation. Close the client
 * when it is no longer needed.
 */
public interface AgentKClient extends AutoCloseable {
    /**
     * Creates a Session from the complete Profile in the request.
     *
     * <p>The Profile is fixed for the lifetime of the Session.
     *
     * @param request the Session title and complete Profile
     * @return a stage containing the created Session
     */
    CompletionStage<CreateSessionResponse> createSession(CreateSessionRequest request);

    /**
     * Lists Sessions from newest to oldest.
     *
     * @param request pagination settings
     * @return a stage containing one page of Sessions and its next-page token
     */
    CompletionStage<ListSessionsResponse> listSessions(ListSessionsRequest request);

    /**
     * Starts an Execution in an existing Session.
     *
     * @param request the Session ID and message, answers, or Pipeline input
     * @return a stage containing the new Execution
     */
    CompletionStage<StartExecutionResponse> startExecution(StartExecutionRequest request);

    /**
     * Requests cancellation of the running Execution in a Session.
     *
     * @param request the Session whose running Execution should be cancelled
     * @return a stage containing the cancellation response
     */
    CompletionStage<CancelExecutionResponse> cancelExecution(CancelExecutionRequest request);

    /**
     * Lists persisted Session events from newest to oldest.
     *
     * @param request the Session ID and pagination settings
     * @return a stage containing one page of events and its next-page token
     */
    CompletionStage<ListEventsResponse> listEvents(ListEventsRequest request);

    /**
     * Watches live event frames for one Session and Execution.
     *
     * <p>The observer receives {@link EventObserver#onComplete()} after a normal stream end and
     * {@link EventObserver#onError(Throwable)} after a stream or observer failure. Cancelling the
     * returned subscription stops the stream without a terminal callback.
     *
     * @param request  the Session and Execution IDs to watch
     * @param observer receives frames and terminal notifications
     * @return a subscription that can cancel the stream
     */
    EventSubscription watchEvents(WatchEventsRequest request, EventObserver observer);

    /**
     * Lists Pipelines available to a Session, ordered by name.
     *
     * @param request the Session ID and pagination settings
     * @return a stage containing one page of Pipelines and its next-page token
     */
    CompletionStage<ListPipelinesResponse> listPipelines(ListPipelinesRequest request);

    /**
     * Lists Executions for a Session from newest to oldest.
     *
     * @param request the Session ID and pagination settings
     * @return a stage containing one page of Executions and its next-page token
     */
    CompletionStage<ListExecutionsResponse> listExecutions(ListExecutionsRequest request);

    /**
     * Gets one Execution by its Session and Execution IDs.
     *
     * @param request the Session and Execution IDs
     * @return a stage containing the requested Execution
     */
    CompletionStage<GetExecutionResponse> getExecution(GetExecutionRequest request);

    /**
     * Lists registered capabilities ordered by capability ID.
     *
     * @param request pagination settings
     * @return a stage containing one page of capabilities and its next-page token
     */
    CompletionStage<ListCapabilitiesResponse> listCapabilities(ListCapabilitiesRequest request);

    /**
     * Invokes a capability without creating a Session or Execution.
     *
     * @param request the capability ID and input object
     * @return a stage containing the capability output
     */
    CompletionStage<InvokeCapabilityResponse> invokeCapability(InvokeCapabilityRequest request);

    /**
     * Gets the service and App identity resolved by AgentK.
     *
     * @param request an empty identity request
     * @return a stage containing the caller Service and App identity
     */
    CompletionStage<WhoAmIResponse> whoAmI(WhoAmIRequest request);

    /**
     * Cancels in-flight operations and releases transport resources.
     *
     * <p>Closing is idempotent. Operations started after close fail.
     */
    @Override
    void close();
}
