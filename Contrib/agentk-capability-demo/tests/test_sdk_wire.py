from __future__ import annotations

import httpx
import pyqwest
import pytest
from agentk.api.v1 import event_pb, model_pb, service_pb
from agentk.api.v1.service_connect import AgentKServiceASGIApplication
from connectrpc.errors import ConnectError
from connectrpc.request import RequestContext
from protobuf import Oneof
from protobuf.wkt import Struct, Value
from pyqwest.testing import ASGITransport

from agentk_capability_demo.agentk_client import create_client
from agentk_capability_demo.app import create_app
from agentk_capability_demo.config import Settings


class WireAgentKService:
    def __init__(self) -> None:
        self.identities: list[tuple[str, str]] = []
        self.lanes: list[str] = []
        self.inputs: list[str] = []
        self.cancelled: list[str] = []
        self.execution = model_pb.Execution(
            id="pex_wire",
            session_id="ses_wire",
            status=model_pb.ExecutionStatus.COMPLETED,
            kind=Oneof(
                "pipeline",
                model_pb.PipelineExecution(
                    name="capability_test", output=Value.from_python({"wire": "ok"})
                ),
            ),
        )

    def __getattr__(self, name: str):
        async def unimplemented(_request, _ctx):
            raise ConnectError.unimplemented(f"{name} is not used in this test")

        return unimplemented

    def authorize(self, ctx: RequestContext) -> None:
        self.lanes.append(ctx.request_headers.get("agentk-lane-id", ""))
        self.identities.append(
            (ctx.request_headers["kws-bin"], ctx.request_headers["kwsCredential"])
        )

    async def who_am_i(self, _request, ctx):
        self.authorize(ctx)
        return service_pb.WhoAmIResponse(id="wire-user", display_name="Wire Tester")

    async def list_capabilities(self, _request, ctx):
        self.authorize(ctx)
        return service_pb.ListCapabilitiesResponse(
            capabilities=[
                model_pb.Capability(
                    id="agentk.wire_echo",
                    source=model_pb.CapabilitySource.CORE,
                    description="Wire echo",
                    input_schema=Struct.from_python(
                        {
                            "type": "object",
                            "properties": {"text": {"type": "string"}},
                            "required": ["text"],
                        }
                    ),
                    output_schema=Struct.from_python({"type": "object"}),
                )
            ]
        )

    async def create_session(self, request, ctx):
        self.authorize(ctx)
        assert request.profile is not None
        return service_pb.CreateSessionResponse(
            session=model_pb.Session(id="ses_wire", title=request.title)
        )

    async def start_execution(self, request, ctx):
        self.authorize(ctx)
        assert request.input is not None
        self.inputs.append(request.input.field)
        return service_pb.StartExecutionResponse(execution=self.execution)

    async def get_execution(self, _request, ctx):
        self.authorize(ctx)
        return service_pb.GetExecutionResponse(execution=self.execution)

    async def list_events(self, _request, ctx):
        self.authorize(ctx)
        return service_pb.ListEventsResponse(
            events=[
                event_pb.Event(
                    event_id="evt_wire",
                    execution_id="pex_wire",
                    payload=Oneof(
                        "assistant_text", event_pb.AssistantText(block=0, text="wire ok")
                    ),
                )
            ]
        )

    async def cancel_execution(self, request, ctx):
        self.authorize(ctx)
        self.cancelled.append(request.session_id)
        return service_pb.CancelExecutionResponse()


@pytest.mark.parametrize(
    ("stage", "headers", "expected_lane"),
    [
        ("PROD", {}, ""),
        ("PROD_REGRESSION_TEST", {}, "prt"),
        ("PROD_REGRESSION_TEST", {"trace-context": '{"laneId":"dev_wire"}'}, "dev_wire"),
    ],
)
async def test_demo_api_over_real_agentk_sdk_connectrpc_transport(
    tmp_path, monkeypatch, stage, headers, expected_lane
):
    monkeypatch.chdir(tmp_path)
    monkeypatch.setenv("KWS_SERVICE_NAME", "agentk.demo.wire")
    monkeypatch.setenv("KWS_SERVICE_CREDENTIAL", "wire-credential")
    monkeypatch.setenv("KWS_SERVICE_STAGE", stage)
    monkeypatch.delenv("FALLBACK_TO_ENV_LANE_ID", raising=False)
    service = WireAgentKService()
    wire_app = AgentKServiceASGIApplication(service)
    transport = ASGITransport(wire_app, http_version=pyqwest.HTTPVersion.HTTP2)

    async with transport:
        sdk = create_client(
            "http://agentk-wire.test",
            "wire-secret",
            http_client=pyqwest.Client(transport),
        )
        demo = create_app(Settings("http://agentk-wire.test", "wire-secret"), sdk)
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=demo), base_url="http://demo.test", headers=headers
        ) as client:
            identity = await client.get("/api/connection")
            assert identity.json()["identity"]["displayName"] == "Wire Tester"

            catalog = await client.get("/api/capabilities")
            assert catalog.json()["capabilities"][0]["id"] == "agentk.wire_echo"

            started = await client.post(
                "/api/capabilities/agentk.wire_echo/runs", json={"args": {"text": "hello"}}
            )
            assert started.status_code == 202
            run_id = started.json()["run"]["executionId"]
            snapshot = await client.get(f"/api/runs/{run_id}")
            assert snapshot.json()["execution"]["pipeline"]["output"] == {"wire": "ok"}
            assert snapshot.json()["events"][0]["assistantText"]["text"] == "wire ok"

            agent_session = await client.post(
                "/api/agent-sessions", json={"agentId": "audio", "title": "Wire chat"}
            )
            session_id = agent_session.json()["session"]["sessionId"]
            message = await client.post(
                f"/api/agent-sessions/{session_id}/messages", json={"text": "hello"}
            )
            assert message.status_code == 202

            cancelled = await client.delete(f"/api/agent-sessions/{session_id}/execution")
            assert cancelled.status_code == 204

        await sdk.aclose()

    assert service.inputs == ["pipeline", "message"]
    assert service.cancelled == ["ses_wire"]
    assert service.identities
    assert set(service.lanes) == {expected_lane}
    assert {credential for _, credential in service.identities} == {"wire-credential"}
