from pathlib import Path
from types import SimpleNamespace

import pytest
from agentk.api.v1 import service_pb

from narrato_agentk.cli import main as cli
from narrato_agentk.cli.main import (
    PIPELINE_PROFILES,
    _create_session,
    _recover_events,
    _start_pipeline,
    _upload,
    _video_content_type,
)
from narrato_agentk.settings import Settings


@pytest.mark.parametrize(
    ("name", "expected"),
    [
        ("a.mp4", "video/mp4"),
        ("a.MOV", "video/quicktime"),
        ("a.mkv", "video/x-matroska"),
        ("a.webm", "video/webm"),
    ],
)
def test_video_content_type(name, expected):
    assert _video_content_type(Path(name)) == expected


def test_video_content_type_rejects_unknown_extension():
    with pytest.raises(ValueError, match="unsupported"):
        _video_content_type(Path("a.txt"))


async def test_recover_events_filters_execution_deduplicates_and_restores_order(monkeypatch):
    printed = []

    class Client:
        async def list_events(self, request):
            return SimpleNamespace(
                events=[
                    SimpleNamespace(event_id="3", execution_id="wanted"),
                    SimpleNamespace(event_id="other", execution_id="other"),
                    SimpleNamespace(event_id="2", execution_id="wanted"),
                    SimpleNamespace(event_id="1", execution_id="wanted"),
                ],
                next_page_token="",
            )

    monkeypatch.setattr(cli, "_print_event", lambda event: printed.append(event.event_id))
    seen = {"1"}
    await _recover_events(Client(), "session", "wanted", seen)

    assert printed == ["2", "3"]
    assert seen == {"1", "2", "3"}


async def test_create_session_sends_three_pipeline_profile(capsys):
    class Client:
        async def create_session(self, request):
            self.request = request
            return SimpleNamespace(session=SimpleNamespace(id="session-1"))

    client = Client()
    session_id = await _create_session(
        client,
        Settings(text_model="text-fixed"),
        "纪录片任务",
    )

    assert session_id == "session-1"
    assert client.request.title == "纪录片任务"
    assert client.request.profile.default_model == "text-fixed"
    assert [
        (pipeline.capability, pipeline.name, pipeline.display_name)
        for pipeline in client.request.profile.pipelines
    ] == list(PIPELINE_PROFILES)
    assert capsys.readouterr().out.strip() == "session-1"


async def test_start_pipeline_serializes_exact_pipeline_input(capsys):
    class Client:
        async def start_execution(self, request):
            self.request = request
            return SimpleNamespace(execution=SimpleNamespace(id="execution-1"))

    client = Client()
    execution_id = await _start_pipeline(
        client,
        "session-1",
        "documentary_video",
        {"videoUri": "https://cdn.test/a.mp4", "voiceId": "Cherry"},
    )

    assert execution_id == "execution-1"
    assert client.request.session_id == "session-1"
    assert client.request.input.field == "pipeline"
    assert client.request.input.value.pipeline == "documentary_video"
    assert client.request.input.value.args.to_python() == {
        "videoUri": "https://cdn.test/a.mp4",
        "voiceId": "Cherry",
    }
    assert capsys.readouterr().out.strip() == "execution=execution-1"


@pytest.mark.parametrize(
    ("response", "message"),
    [
        (SimpleNamespace(session=None), "no session"),
        (SimpleNamespace(session=SimpleNamespace(id="")), "no session"),
    ],
)
async def test_create_session_rejects_missing_output(response, message):
    class Client:
        async def create_session(self, request):
            return response

    with pytest.raises(RuntimeError, match=message):
        await _create_session(Client(), Settings(), "title")


async def test_recover_events_follows_pagination(monkeypatch):
    requests: list[service_pb.ListEventsRequest] = []

    class Client:
        async def list_events(self, request):
            requests.append(request)
            if not request.page_token:
                return SimpleNamespace(
                    events=[SimpleNamespace(event_id="2", execution_id="wanted")],
                    next_page_token="next",
                )
            return SimpleNamespace(
                events=[SimpleNamespace(event_id="1", execution_id="wanted")],
                next_page_token="",
            )

    printed = []
    monkeypatch.setattr(cli, "_print_event", lambda event: printed.append(event.event_id))
    await _recover_events(Client(), "session-1", "wanted", set())

    assert [request.page_token for request in requests] == ["", "next"]
    assert printed == ["1", "2"]


async def test_upload_forwards_configured_api_access_token(tmp_path, monkeypatch, capsys):
    video = tmp_path / "input.mp4"
    video.write_bytes(b"abc")
    captured = {}

    class Response:
        def raise_for_status(self):
            return None

        def json(self):
            return {"uri": "https://cdn.test/input.mp4"}

    class Client:
        async def __aenter__(self):
            return self

        async def __aexit__(self, *_):
            return None

        async def post(self, url, **kwargs):
            captured.update(url=url, **kwargs)
            return Response()

    monkeypatch.setattr(cli.httpx, "AsyncClient", lambda **kwargs: Client())

    uri = await _upload(
        video,
        Settings(
            media_api_url="https://media.test",
            console_token="upload-secret",
        ),
    )

    assert uri == "https://cdn.test/input.mp4"
    assert captured["headers"] == {"X-Narrato-Console-Token": "upload-secret"}
    assert capsys.readouterr().out.strip() == uri
