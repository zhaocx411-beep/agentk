from __future__ import annotations

import importlib

from agentk.api.v1 import event_pb, service_pb
from agentk.api.v1.service_connect import AgentKServiceClient
from protobuf import Oneof


def test_all_generated_modules_import() -> None:
    modules = [
        "agentk.api.v1.event_pb",
        "agentk.api.v1.model_pb",
        "agentk.api.v1.profile_pb",
        "agentk.api.v1.service_pb",
        "agentk.api.v1.service_connect",
        "agentk.common.v1.common_pb",
        "agentk.invocation.v1.invocation_pb",
    ]

    for module in modules:
        assert importlib.import_module(module)


def test_generated_client_contains_complete_service_contract() -> None:
    methods = {
        "create_session",
        "list_sessions",
        "start_execution",
        "cancel_execution",
        "list_events",
        "watch_events",
        "list_pipelines",
        "list_executions",
        "get_execution",
        "list_capabilities",
        "who_am_i",
    }

    assert methods <= AgentKServiceClient.__dict__.keys()


def test_edition_2023_presence_oneof_and_binary_round_trip() -> None:
    original = service_pb.StartExecutionRequest(
        session_id="session-1",
        input=Oneof(
            "answers",
            service_pb.AgentAnswersExecutionInput(
                tool_call_id="call-1",
                answers=[
                    service_pb.AnswerInput(
                        question_id="q1",
                        selected=["cartoon"],
                    )
                ],
            ),
        ),
    )

    decoded = service_pb.StartExecutionRequest.from_binary(original.to_binary())

    assert decoded.session_id == "session-1"
    assert decoded.input is not None
    assert decoded.input.field == "answers"
    assert decoded.input.value.tool_call_id == "call-1"
    assert len(decoded.input.value.answers) == 1
    assert decoded.input.value.answers[0].question_id == "q1"
    assert decoded.input.value.answers[0].selected == ["cartoon"]


def test_list_pagination_binary_round_trip() -> None:
    original = service_pb.ListEventsRequest(
        session_id="session-1",
        page_size=20,
        page_token="event-20",
    )

    decoded = service_pb.ListEventsRequest.from_binary(original.to_binary())

    assert decoded.session_id == "session-1"
    assert decoded.page_size == 20
    assert decoded.page_token == "event-20"


def test_media_attachments_binary_round_trip() -> None:
    original = event_pb.UserInput(
        text="inspect",
        attachments=[
            event_pb.MediaAttachment(
                type="image",
                uri="artifact://cat.png",
                mime_type="image/png",
            ),
            event_pb.MediaAttachment(
                type="video",
                uri="https://cdn.example.test/clip.mp4",
                mime_type="video/mp4",
            ),
        ],
    )

    decoded = event_pb.UserInput.from_binary(original.to_binary())

    assert [attachment.type for attachment in decoded.attachments] == ["image", "video"]
    assert decoded.attachments[1].mime_type == "video/mp4"
    assert not hasattr(event_pb, "ImageAttachment")
