from __future__ import annotations

from collections.abc import AsyncIterator, Mapping
from dataclasses import dataclass
from typing import Self
from urllib.parse import urlparse

from agentk.api.v1.service_pb import (
    CancelExecutionRequest,
    CancelExecutionResponse,
    CreateSessionRequest,
    CreateSessionResponse,
    GetExecutionRequest,
    GetExecutionResponse,
    InvokeCapabilityRequest,
    InvokeCapabilityResponse,
    ListCapabilitiesRequest,
    ListCapabilitiesResponse,
    ListEventsRequest,
    ListEventsResponse,
    ListExecutionsRequest,
    ListExecutionsResponse,
    ListPipelinesRequest,
    ListPipelinesResponse,
    ListSessionsRequest,
    ListSessionsResponse,
    StartExecutionRequest,
    StartExecutionResponse,
    WatchEventsRequest,
    WatchEventsResponse,
    WhoAmIRequest,
    WhoAmIResponse,
)

from ._identity import identity_headers
from ._provider import RPCProvider


@dataclass(frozen=True, slots=True)
class Options:
    """Options shared by AgentK client transports.

    Attributes:
        endpoint: An absolute AgentK URL that overrides the transport default.
            When empty, HTTP uses the environment gateway and gRPC uses KESS.
    """

    endpoint: str = ""


def _validate_endpoint(endpoint: str) -> str:
    parsed = urlparse(endpoint)
    if not parsed.scheme or not parsed.netloc:
        raise ValueError(f"agentk client: invalid endpoint {endpoint!r}")
    return endpoint


class AsyncClient:
    """Authenticated asynchronous access to the AgentK API.

    Obtain an instance from a transport factory such as
    agentk.client.grpc.create_client. RPC failures are raised by the transport.
    Use the client as an asynchronous context manager or call aclose when it is
    no longer needed. The request lane set with ksresolve.rpc.with_lane is
    forwarded by the gRPC transports.
    """

    def __init__(self, provider: RPCProvider) -> None:
        self._provider = provider
        self._headers: Mapping[str, str] = identity_headers()

    def _request_headers(self) -> Mapping[str, str]:
        return dict(self._headers)

    async def create_session(
        self, request: CreateSessionRequest
    ) -> CreateSessionResponse:
        """Create a Session from the complete Profile in the request.

        The Profile is fixed for the lifetime of the Session.

        Args:
            request: The Session title and complete Profile.

        Returns:
            The created Session.
        """
        headers = self._request_headers()
        rpc = await self._provider.acquire()
        return await rpc.create_session(request, headers=headers)

    async def list_sessions(self, request: ListSessionsRequest) -> ListSessionsResponse:
        """Return a page of Sessions ordered from newest to oldest.

        Args:
            request: Pagination settings.

        Returns:
            The matching Sessions and the next-page token.
        """
        headers = self._request_headers()
        rpc = await self._provider.acquire()
        return await rpc.list_sessions(request, headers=headers)

    async def start_execution(
        self, request: StartExecutionRequest
    ) -> StartExecutionResponse:
        """Start an Execution in an existing Session.

        Args:
            request: The Session ID and message, answers, or Pipeline input.

        Returns:
            The new Execution.
        """
        headers = self._request_headers()
        rpc = await self._provider.acquire()
        return await rpc.start_execution(request, headers=headers)

    async def cancel_execution(
        self, request: CancelExecutionRequest
    ) -> CancelExecutionResponse:
        """Request cancellation of the running Execution in a Session.

        Args:
            request: The Session whose running Execution should be cancelled.

        Returns:
            The cancellation response.
        """
        headers = self._request_headers()
        rpc = await self._provider.acquire()
        return await rpc.cancel_execution(request, headers=headers)

    async def list_events(self, request: ListEventsRequest) -> ListEventsResponse:
        """Return a page of persisted Session events, newest first.

        Args:
            request: The Session ID and pagination settings.

        Returns:
            The matching events and the next-page token.
        """
        headers = self._request_headers()
        rpc = await self._provider.acquire()
        return await rpc.list_events(request, headers=headers)

    async def watch_events(
        self, request: WatchEventsRequest
    ) -> AsyncIterator[WatchEventsResponse]:
        """Watch live event frames for one Session and Execution.

        The underlying stream is closed when iteration completes, fails, or is
        explicitly closed by the caller.

        Args:
            request: The Session and Execution IDs to watch.

        Yields:
            Committed events, transient deltas, and the terminal stream frame.
        """
        headers = self._request_headers()
        rpc = await self._provider.acquire()
        events = rpc.watch_events(request, headers=headers)
        try:
            async for frame in events:
                yield frame
        finally:
            aclose = getattr(events, "aclose", None)
            if aclose is not None:
                await aclose()

    async def list_pipelines(
        self, request: ListPipelinesRequest
    ) -> ListPipelinesResponse:
        """Return a page of Pipelines available to a Session, ordered by name.

        Args:
            request: The Session ID and pagination settings.

        Returns:
            The matching Pipelines and the next-page token.
        """
        headers = self._request_headers()
        rpc = await self._provider.acquire()
        return await rpc.list_pipelines(request, headers=headers)

    async def list_executions(
        self, request: ListExecutionsRequest
    ) -> ListExecutionsResponse:
        """Return a page of Session Executions ordered from newest to oldest.

        Args:
            request: The Session ID and pagination settings.

        Returns:
            The matching Executions and the next-page token.
        """
        headers = self._request_headers()
        rpc = await self._provider.acquire()
        return await rpc.list_executions(request, headers=headers)

    async def get_execution(self, request: GetExecutionRequest) -> GetExecutionResponse:
        """Return one Execution by its Session and Execution IDs.

        Args:
            request: The Session and Execution IDs.

        Returns:
            The requested Execution.
        """
        headers = self._request_headers()
        rpc = await self._provider.acquire()
        return await rpc.get_execution(request, headers=headers)

    async def list_capabilities(
        self, request: ListCapabilitiesRequest
    ) -> ListCapabilitiesResponse:
        """Return a page of registered capabilities ordered by capability ID.

        Args:
            request: Pagination settings.

        Returns:
            The matching capabilities and the next-page token.
        """
        headers = self._request_headers()
        rpc = await self._provider.acquire()
        return await rpc.list_capabilities(request, headers=headers)

    async def invoke_capability(
        self, request: InvokeCapabilityRequest
    ) -> InvokeCapabilityResponse:
        """Invoke a capability without creating a Session or Execution.

        The call waits for the capability to complete. Cancelling the caller's
        task cancels the in-flight RPC.

        Args:
            request: The capability ID and JSON-compatible input object.

        Returns:
            The capability output.
        """
        headers = self._request_headers()
        rpc = await self._provider.acquire()
        return await rpc.invoke_capability(request, headers=headers)

    async def who_am_i(self, request: WhoAmIRequest) -> WhoAmIResponse:
        """Return the service and App identity resolved by AgentK.

        Args:
            request: An empty identity request.

        Returns:
            The caller Service and App identity.
        """
        headers = self._request_headers()
        rpc = await self._provider.acquire()
        return await rpc.who_am_i(request, headers=headers)

    async def aclose(self) -> None:
        """Close transport resources owned by this client."""
        await self._provider.aclose()

    async def __aenter__(self) -> Self:
        """Return this client from an asynchronous context manager."""
        return self

    async def __aexit__(self, *_exc: object) -> None:
        """Close the client when leaving an asynchronous context manager."""
        await self.aclose()
