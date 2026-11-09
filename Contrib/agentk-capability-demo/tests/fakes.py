from __future__ import annotations

from agentk.api.v1 import model_pb, service_pb


class FakeAgentK:
    def __init__(self) -> None:
        self.started: list[object] = []
        self.created: list[object] = []
        self.cancelled: list[str] = []
        self.capability_pages = 0
        self.execution = model_pb.Execution.from_json(
            '{"id":"pex_1","sessionId":"ses_1","status":"EXECUTION_STATUS_COMPLETED",'
            '"pipeline":{"name":"capability_test","output":{"ok":true}}}'
        )

    async def who_am_i(self, _request):
        return service_pb.WhoAmIResponse(id="usr_1", display_name="Local Tester")

    async def list_capabilities(self, request):
        self.capability_pages += 1
        capability = model_pb.Capability.from_json(
            '{"id":"agentk.echo","source":"CAPABILITY_SOURCE_CORE",'
            '"description":"Echo input","inputSchema":{"type":"object",'
            '"properties":{"text":{"type":"string"}},"required":["text"]},'
            '"outputSchema":{"type":"object"}}'
        )
        if not request.page_token:
            return service_pb.ListCapabilitiesResponse(
                capabilities=[capability], next_page_token="next"
            )
        return service_pb.ListCapabilitiesResponse(capabilities=[])

    async def create_session(self, request):
        self.created.append(request)
        return service_pb.CreateSessionResponse(
            session=model_pb.Session(id="ses_1", title=request.title)
        )

    async def start_execution(self, request):
        self.started.append(request)
        return service_pb.StartExecutionResponse(execution=self.execution)

    async def get_execution(self, _request):
        return service_pb.GetExecutionResponse(execution=self.execution)

    async def list_events(self, _request):
        return service_pb.ListEventsResponse(events=[])

    async def cancel_execution(self, request):
        self.cancelled.append(request.session_id)
        return service_pb.CancelExecutionResponse()

    async def aclose(self):
        return None
