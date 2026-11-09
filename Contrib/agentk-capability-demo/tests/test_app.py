import json
from pathlib import Path

import httpx
import pytest
from agentk.api.v1 import model_pb, service_pb

from agentk_capability_demo.agent100.narrato.router import _tts_catalog
from agentk_capability_demo.agent100.narrato.settings import Settings as NarratoSettings
from agentk_capability_demo.api import (
    DEFAULT_UPLOAD_LIMIT,
    VIDEO_UPLOAD_LIMIT,
    _upload_limit,
    _upload_media_type,
)
from agentk_capability_demo.app import create_app
from agentk_capability_demo.config import Settings

from .fakes import FakeAgentK


class FakeUploadStorage:
    def __init__(self) -> None:
        self.calls = []

    async def put(self, path, name, size, mime):
        self.calls.append((path.read_bytes(), name, size, mime))
        key = "agentk-capability-demo/reference.png"
        return f"https://blob.test/bucket/{key}", key


@pytest.fixture
def app(tmp_path: Path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    return create_app(Settings("http://agentk.test", "secret"), FakeAgentK())


@pytest.fixture
async def client(app):
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://demo.test") as value:
        yield value


async def test_page_health_and_catalog(client):
    assert (await client.get("/healthz")).json() == {"status": "ok"}
    page = await client.get("/")
    assert page.status_code == 200
    assert "CAPABILITY LAB" in page.text
    assert "AGENT 100" in page.text
    assert "Narrato Control Room" in page.text
    assert page.headers["cache-control"] == "no-store"
    assert "/assets/form-defaults.js" in page.text
    assert "/assets/run-state.js" in page.text
    assert "/assets/result-media.js" in page.text
    assert "/assets/request-ledger.js" in page.text
    form_defaults = await client.get("/assets/form-defaults.js")
    assert form_defaults.status_code == 200
    assert form_defaults.headers["cache-control"] == "no-store"
    assert (await client.get("/assets/run-state.js")).status_code == 200
    assert (await client.get("/assets/result-media.js")).status_code == 200
    assert (await client.get("/assets/request-ledger.js")).status_code == 200
    narrato_script = await client.get("/assets/narrato.js")
    assert narrato_script.status_code == 200
    assert "narratoUploadButton" not in page.text
    assert "narratoUploadButton" not in narrato_script.text
    assert 'id="narratoCancel"' not in page.text
    assert "narrato-history-stop" in narrato_script.text
    assert "停止请求已接受 · 等待 AgentK 返回终态" in narrato_script.text
    assert 'upload(file);' in narrato_script.text
    assert "正在上传" in narrato_script.text
    assert "Narrato 状态刷新失败" not in narrato_script.text
    assert "状态连接暂时中断，正在自动重试" in narrato_script.text
    assert "agentk.detect_shots" not in narrato_script.text
    assert "const POLL_INTERVAL_MS = 10000" in narrato_script.text
    assert "const HISTORY_POLL_INTERVAL_MS = 10000" in narrato_script.text
    assert "已经在后台执行，到历史生成记录里去查看" in narrato_script.text
    assert "支持多个任务后台并发" in page.text
    assert 'id="narratoInputVideo"' in page.text
    assert 'id="narratoInputParams"' in page.text
    assert 'aria-controls="narratoInputSummary"' in page.text
    assert 'aria-expanded="false"' in page.text
    assert 'rel="noopener noreferrer"' in page.text
    assert "selectTaskInput" in narrato_script.text
    assert "findInputVideoUri" in narrato_script.text
    assert "withObservedRequestTimes" in narrato_script.text
    assert "request-timing" in narrato_script.text
    assert "request-state" in narrato_script.text
    run_source = narrato_script.text.split("async function run()", 1)[1].split(
        "function renderEvents", 1
    )[0]
    assert "await openHistory" not in run_source
    assert "void poll();" in narrato_script.text
    assert run_source.index("setSubmitting(false);") < run_source.index("openHistory(")
    connection = await client.get("/api/connection")
    assert connection.json()["ok"] is True
    assert connection.headers["cache-control"] == "no-store"
    agents = (await client.get("/api/agents")).json()
    assert agents["count"] == 10
    agent100 = (await client.get("/api/agent100")).json()
    assert agent100 == {
        "agents": [
            {
                "id": "narrato",
                "name": "Narrato",
                "icon": "N/A",
                "accent": "#ff6b48",
                "description": "纪录片解说脚本、配音与成片 Pipeline 测试台。",
            }
        ],
        "count": 1,
    }


async def test_narrato_config_and_execution_routes(client, app):
    fail_poll = False

    class FakeNarratoConsole:
        async def get_capability_status(self):
            return [{"id": "contrib.narrato.create_documentary_video", "published": True}]

        async def start_execution(self, pipeline, args, *, session_id, title):
            assert pipeline == "documentary_script"
            assert args == {"videoUri": "https://cdn.test/input.mp4"}
            assert session_id is None
            assert title == "Browser test"
            return {"sessionId": "ses_n", "executionId": "pex_n", "status": "running"}

        async def get_execution(self, session_id, execution_id):
            assert (session_id, execution_id) == ("ses_n", "pex_n")
            if fail_poll:
                raise ConnectionError("temporary")
            return {
                "status": "completed",
                "events": [
                    {
                        "id": "request-1",
                        "type": "progress",
                        "step": "request",
                        "data": {
                            "kind": "request",
                            "requestKind": "activity",
                            "name": "agentk.capability.probe_video",
                            "request": {"videoUrl": "https://cdn.test/input.mp4"},
                        },
                    }
                ],
                "output": {"version": "2"},
            }

        async def cancel_execution(self, session_id):
            return {"sessionId": session_id, "cancelRequested": True}

    app.state.narrato_console = FakeNarratoConsole()
    config = (await client.get("/api/agent100/narrato/config")).json()
    assert [item["name"] for item in config["pipelines"]] == [
        "documentary_video",
        "documentary_script",
        "documentary_render",
    ]
    assert config["capabilities"][0]["published"] is True
    assert config["mediaError"]

    started = await client.post(
        "/api/agent100/narrato/executions",
        json={
            "pipeline": "documentary_script",
            "args": {"videoUri": "https://cdn.test/input.mp4"},
            "title": "Browser test",
        },
    )
    assert started.status_code == 201
    assert started.json()["executionId"] == "pex_n"
    history = (await client.get("/api/agent100/narrato/executions")).json()
    assert history["count"] == 1
    assert history["items"][0] == {
        "sessionId": "ses_n",
        "executionId": "pex_n",
        "pipeline": "documentary_script",
        "title": "Browser test",
        "status": "running",
        "cancelRequested": False,
        "error": "",
        "hasOutput": False,
        "createdAt": history["items"][0]["createdAt"],
        "updatedAt": history["items"][0]["updatedAt"],
    }
    task_cancelled = await client.post(
        "/api/agent100/narrato/executions/ses_n/pex_n/cancel"
    )
    assert task_cancelled.json() == {
        "sessionId": "ses_n",
        "executionId": "pex_n",
        "cancelRequested": True,
    }
    cancelling_history = (await client.get("/api/agent100/narrato/executions")).json()
    assert cancelling_history["items"][0]["cancelRequested"] is True
    background_refresh = (
        await client.get("/api/agent100/narrato/executions?refresh=true")
    ).json()
    assert background_refresh["items"][0]["status"] == "completed"
    assert background_refresh["items"][0]["hasOutput"] is True
    assert background_refresh["items"][0]["cancelRequested"] is False
    terminal_cancel = await client.post(
        "/api/agent100/narrato/executions/ses_n/pex_n/cancel"
    )
    assert terminal_cancel.status_code == 409
    polled = await client.get("/api/agent100/narrato/executions/ses_n/pex_n")
    assert polled.json()["output"] == {"version": "2"}
    assert polled.json()["request"] == {
        "pipeline": "documentary_script",
        "args": {"videoUri": "https://cdn.test/input.mp4"},
        "title": "Browser test",
    }
    assert polled.json()["events"][0]["observedAt"]
    refreshed_history = (await client.get("/api/agent100/narrato/executions")).json()
    assert refreshed_history["items"][0]["status"] == "completed"
    assert refreshed_history["items"][0]["hasOutput"] is True
    fail_poll = True
    cached = await client.get("/api/agent100/narrato/executions/ses_n/pex_n")
    assert cached.status_code == 200
    assert cached.json()["cached"] is True
    assert cached.json()["output"] == {"version": "2"}
    cancelled = await client.post("/api/agent100/narrato/sessions/ses_n/cancel")
    assert cancelled.json()["cancelRequested"] is True
    unavailable_upload = await client.post(
        "/api/agent100/narrato/media",
        files={"file": ("input.mp4", b"abc", "video/mp4")},
    )
    assert unavailable_upload.status_code == 503


def test_narrato_reuses_root_blob_configuration(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    settings = Settings(
        "http://agentk.test",
        "secret",
        blob_endpoint="https://blob.test",
        blob_bucket="shared-bucket",
    )

    app = create_app(settings, FakeAgentK(), FakeUploadStorage())

    assert app.state.narrato_settings.upload_bucket == "shared-bucket"
    assert app.state.narrato_settings.bs3_endpoint == "https://blob.test"
    assert app.state.narrato_settings.cdn_base == "https://blob.test/shared-bucket"
    assert app.state.narrato_media_error == ""


def test_narrato_uses_shared_blob_environment_only(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    monkeypatch.setenv("AGENTK_DEMO_BLOB_ENDPOINT", "https://blob.test")
    monkeypatch.setenv("AGENTK_DEMO_BLOB_BUCKET", "shared-bucket")

    app = create_app(client=FakeAgentK(), upload_storage=FakeUploadStorage())

    assert app.state.narrato_settings.upload_bucket == "shared-bucket"
    assert app.state.narrato_settings.bs3_endpoint == "https://blob.test"
    assert app.state.narrato_settings.cdn_base == "https://blob.test/shared-bucket"
    assert app.state.narrato_media_error == ""


def test_narrato_ignores_legacy_specific_media_configuration(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    monkeypatch.setenv("NARRATO_UPLOAD_BUCKET", "narrato-bucket")
    monkeypatch.setenv("NARRATO_BS3_ENDPOINT", "https://narrato-blob.test")
    monkeypatch.setenv("NARRATO_CDN_BASE", "https://narrato-cdn.test")
    settings = Settings(
        "http://agentk.test",
        "secret",
        blob_endpoint="https://blob.test",
        blob_bucket="shared-bucket",
        blob_cdn_base="https://cdn.test",
    )

    app = create_app(settings, FakeAgentK(), FakeUploadStorage())

    assert app.state.narrato_settings.upload_bucket == "shared-bucket"
    assert app.state.narrato_settings.bs3_endpoint == "https://blob.test"
    assert app.state.narrato_settings.cdn_base == "https://cdn.test"


async def test_capability_discovery_run_status_and_cancel(client, app):
    catalog = (await client.get("/api/capabilities")).json()
    assert catalog["count"] == 1
    assert app.state.agentk.capability_pages == 2

    response = await client.post(
        "/api/capabilities/agentk.echo/runs", json={"args": {"text": "hello"}}
    )
    assert response.status_code == 202
    run = response.json()["run"]
    assert run["sessionId"] == "ses_1"
    status = await client.get(f"/api/runs/{run['executionId']}")
    assert status.json()["execution"]["pipeline"]["output"] == {"ok": True}
    assert (await client.delete(f"/api/runs/{run['executionId']}")).status_code == 204
    assert app.state.agentk.cancelled == ["ses_1"]


async def test_keffects_catalog_and_extended_arguments_are_forwarded(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    capability_ids = (
        "agentk.generate_text",
        "agentk.generate_video",
        "agentk.create_voice_embedding",
        "agentk.create_kling_subject",
        "agentk.identify_faces",
        "agentk.lip_sync_video",
        "agentk.kling_omni_video",
    )

    class KeffectsCatalogFake(FakeAgentK):
        async def list_capabilities(self, _request):
            capabilities = [
                model_pb.Capability.from_json(
                    json.dumps(
                        {
                            "id": capability_id,
                            "source": "CAPABILITY_SOURCE_CORE",
                            "description": f"Test {capability_id}",
                            "inputSchema": {"type": "object"},
                            "outputSchema": {"type": "object"},
                        }
                    )
                )
                for capability_id in capability_ids
            ]
            return service_pb.ListCapabilitiesResponse(capabilities=capabilities)

    app = create_app(Settings("http://agentk.test", "secret"), KeffectsCatalogFake())
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://demo.test") as local:
        catalog = (await local.get("/api/capabilities")).json()
        assert [item["id"] for item in catalog["capabilities"]] == list(capability_ids)

        cases = {
            "agentk.generate_text": {
                "model": "deepseek-v4-pro",
                "prompt": "返回结构化测试结果",
                "outputSchema": {
                    "type": "object",
                    "required": ["ok"],
                    "properties": {"ok": {"type": "boolean"}},
                },
            },
            "agentk.generate_video": {
                "model": "kling-3.0",
                "firstFrame": "https://assets.test/first.png",
                "lastFrame": "https://assets.test/last.png",
                "prompt": "主体从首帧自然运动到尾帧",
                "resolution": "1080p",
                "duration": "5",
                "audio": "off",
            },
            "agentk.create_voice_embedding": {"audio": "https://assets.test/voice.wav"},
            "agentk.create_kling_subject": {
                "name": "测试主体",
                "images": ["https://assets.test/subject.png"],
                "voiceEmbeddings": ["https://assets.test/voice.embedding"],
            },
            "agentk.identify_faces": {"video": "https://assets.test/input.mp4"},
            "agentk.lip_sync_video": {
                "video": "https://assets.test/input.mp4",
                "audio": "https://assets.test/speech.wav",
                "faceIndexKey": "face-index-key",
                "faceId": "face-id",
                "soundInsertTime": 0,
                "soundStartTime": 0,
                "soundEndTime": 3000,
                "soundVolume": 1.2,
                "originalAudioVolume": 0.5,
            },
            "agentk.kling_omni_video": {
                "model": "kling-3.0-omni",
                "prompt": "让已创建主体自然地向镜头挥手",
                "subjectLists": ["subject-list-from-create-kling-subject"],
                "duration": "5",
                "resolution": "720p",
                "audio": "off",
                "multiShot": False,
            },
        }
        for capability_id, args in cases.items():
            response = await local.post(
                f"/api/capabilities/{capability_id}/runs", json={"args": args}
            )
            assert response.status_code == 202
            started = app.state.agentk.started[-1]
            assert json.loads(started.to_json())["pipeline"]["args"] == args


async def test_capability_catalog_hides_cosyvoice_and_enriches_public_tts_options(
    tmp_path, monkeypatch
):
    monkeypatch.chdir(tmp_path)

    class VoiceCatalogFake(FakeAgentK):
        async def list_capabilities(self, _request):
            capabilities = []
            for capability_id in (
                "agentk.generate_speech_qwen",
                "agentk.generate_speech_minimax",
                "agentk.generate_speech_cosyvoice",
                "agentk.generate_brand_ad",
            ):
                capabilities.append(
                    model_pb.Capability.from_json(
                        json.dumps(
                            {
                                "id": capability_id,
                                "source": "CAPABILITY_SOURCE_CORE",
                                "inputSchema": {
                                    "type": "object",
                                    "required": ["voiceId"],
                                    "properties": {"voiceId": {"type": "string"}},
                                },
                                "outputSchema": {"type": "object"},
                            }
                        )
                    )
                )
            capabilities.append(
                model_pb.Capability.from_json(
                    json.dumps(
                        {
                            "id": "contrib.example.capability",
                            "source": "CAPABILITY_SOURCE_CONTRIB",
                            "inputSchema": {"type": "object"},
                            "outputSchema": {"type": "object"},
                        }
                    )
                )
            )
            return service_pb.ListCapabilitiesResponse(capabilities=capabilities)

    app = create_app(Settings("http://agentk.test", "secret"), VoiceCatalogFake())
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://demo.test") as local:
        payload = (await local.get("/api/capabilities")).json()

    assert payload["count"] == 2
    by_id = {item["id"]: item for item in payload["capabilities"]}
    assert "agentk.generate_speech_cosyvoice" not in by_id
    assert "agentk.generate_brand_ad" not in by_id
    assert "contrib.example.capability" not in by_id
    assert by_id["agentk.generate_speech_qwen"]["inputSchema"]["properties"]["voiceId"][
        "default"
    ] == "Cherry"
    assert by_id["agentk.generate_speech_minimax"]["inputSchema"]["properties"]["voiceId"][
        "x-options"
    ][0]["value"] == "moss_audio_ce44fc67-7ce3-11f0-8de5-96e35d26fb85"


def test_narrato_qwen_catalog_prefers_instruct_model_only_when_advertised():
    settings = NarratoSettings()

    with_instruct = _tts_catalog(
        [
            {
                "id": "agentk.generate_speech_qwen",
                "published": True,
                "inputSchema": {
                    "properties": {
                        "model": {
                            "enum": [
                                "qwen3-tts-flash",
                                "qwen3-tts-instruct-flash",
                                "qwen3-tts-unknown",
                            ],
                            "default": "qwen3-tts-flash",
                        }
                    }
                },
            }
        ],
        settings,
    )
    assert with_instruct["providers"]["qwen"]["defaultModel"] == (
        "qwen3-tts-instruct-flash"
    )
    assert with_instruct["providers"]["qwen"]["models"] == [
        "qwen3-tts-flash",
        "qwen3-tts-instruct-flash",
    ]

    flash_only = _tts_catalog(
        [
            {
                "id": "agentk.generate_speech_qwen",
                "published": True,
                "inputSchema": {
                    "properties": {"model": {"enum": ["qwen3-tts-flash"]}}
                },
            }
        ],
        settings,
    )
    assert flash_only["providers"]["qwen"]["defaultModel"] == "qwen3-tts-flash"


async def test_agent_message_answer_and_upload(client, app):
    created = await client.post(
        "/api/agent-sessions",
        json={"agentId": "audio", "title": "Audio test", "model": "deepseek-v4-pro"},
    )
    assert created.status_code == 201
    assert app.state.agentk.created[0].profile.default_model == "deepseek-v4-pro"
    session_id = created.json()["session"]["sessionId"]
    sent = await client.post(
        f"/api/agent-sessions/{session_id}/messages",
        json={
            "text": "transcribe",
            "attachments": [
                {
                    "uri": "https://assets.test/sample.png",
                    "name": "sample.png",
                    "type": "image",
                    "mimeType": "image/png",
                },
                {
                    "uri": "https://assets.test/sample.mp4",
                    "name": "sample.mp4",
                    "type": "video",
                    "mimeType": "video/mp4",
                },
            ],
        },
    )
    assert sent.status_code == 202
    delivered = app.state.agentk.started[0].input.value.text
    assert "transcribe" in delivered
    assert "sample.png（image）：https://assets.test/sample.png" in delivered
    delivered_media = app.state.agentk.started[0].input.value.attachments
    assert len(delivered_media) == 0
    stream = await client.get(f"/api/agent-sessions/{session_id}/events")
    assert stream.headers["content-type"].startswith("text/event-stream")
    assert "event: snapshot" in stream.text
    payload = json.loads(next(line[6:] for line in stream.text.splitlines() if line.startswith("data: ")))
    assert payload["execution"]["status"] == "EXECUTION_STATUS_COMPLETED"
    answered = await client.post(
        f"/api/agent-sessions/{session_id}/answers",
        json={"toolCallId": "call_1", "answers": [{"questionId": "q1", "selected": ["A"]}]},
    )
    assert answered.status_code == 202
    upload = await client.post("/api/uploads", files={"file": ("note.txt", b"hello", "text/plain")})
    assert upload.status_code == 201
    assert upload.json()["agentkReachable"] is False
    assert upload.json()["type"] == "text"
    image = await client.post(
        "/api/uploads", files={"file": ("reference.png", b"\x89PNG\r\n\x1a\n", "image/png")}
    )
    assert image.status_code == 201
    assert image.json()["type"] == "image"
    assert image.json()["mimeType"] == "image/png"
    audio = await client.post(
        "/api/uploads", files={"file": ("tone.wav", b"RIFFaudio", "application/octet-stream")}
    )
    assert audio.status_code == 201
    assert audio.json()["type"] == "audio"
    assert audio.json()["mimeType"] == "audio/wav"
    assert len(app.state.agentk.started) == 2


async def test_gemini_message_attaches_image_and_only_one_video(client, app):
    created = await client.post(
        "/api/agent-sessions",
        json={
            "agentId": "gemini-creative",
            "model": "gemini-3-flash-preview",
        },
    )
    assert created.status_code == 201
    assert app.state.agentk.created[0].profile.default_model == "gemini-3-flash-preview"
    session_id = created.json()["session"]["sessionId"]
    sent = await client.post(
        f"/api/agent-sessions/{session_id}/messages",
        json={
            "text": "compare",
            "attachments": [
                {
                    "uri": "https://assets.test/frame.png",
                    "name": "frame.png",
                    "type": "image",
                    "mimeType": "image/png",
                },
                {
                    "uri": "https://assets.test/vector.svg",
                    "name": "vector.svg",
                    "type": "image",
                    "mimeType": "image/svg+xml",
                },
                {
                    "uri": "https://assets.test/first.mp4",
                    "name": "first.mp4",
                    "type": "video",
                    "mimeType": "video/mp4",
                },
                {
                    "uri": "https://assets.test/second.mp4",
                    "name": "second.mp4",
                    "type": "video",
                    "mimeType": "video/mp4",
                },
                {
                    "uri": "https://assets.test/speech.wav",
                    "name": "speech.wav",
                    "type": "audio",
                    "mimeType": "audio/x-wav",
                },
            ],
        },
    )
    assert sent.status_code == 202
    delivered = app.state.agentk.started[0].input.value
    assert "second.mp4" in delivered.text
    assert "vector.svg" in delivered.text
    assert [(item.type, item.uri) for item in delivered.attachments] == [
        ("image", "https://assets.test/frame.png"),
        ("video", "https://assets.test/first.mp4"),
        ("audio", "https://assets.test/speech.wav"),
    ]
    assert delivered.attachments[2].mime_type == "audio/wav"


async def test_agent_event_stream_retries_transient_status_errors(tmp_path, monkeypatch):
    class FlakyAgentK(FakeAgentK):
        status_calls = 0

        async def get_execution(self, request):
            self.status_calls += 1
            if self.status_calls < 3:
                raise RuntimeError("temporary status failure")
            return await super().get_execution(request)

    monkeypatch.chdir(tmp_path)
    monkeypatch.setattr("agentk_capability_demo.api.AGENT_STREAM_INTERVAL_SECONDS", 0)
    agentk = FlakyAgentK()
    app = create_app(Settings("http://agentk.test", "secret"), agentk)
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://demo.test") as client:
        created = await client.post("/api/agent-sessions", json={"agentId": "audio"})
        session_id = created.json()["session"]["sessionId"]
        await client.post(f"/api/agent-sessions/{session_id}/messages", json={"text": "hello"})
        stream = await client.get(f"/api/agent-sessions/{session_id}/events")

    assert agentk.status_calls == 3
    assert "event: agent-error" not in stream.text
    assert "event: snapshot" in stream.text


def test_video_upload_limit_is_100_mb_while_other_files_stay_at_20_mb():
    assert _upload_limit("clip.mp4", "video/mp4") == VIDEO_UPLOAD_LIMIT
    assert _upload_limit("clip.mov", "application/octet-stream") == VIDEO_UPLOAD_LIMIT
    assert _upload_limit("poster.png", "image/png") == DEFAULT_UPLOAD_LIMIT
    assert VIDEO_UPLOAD_LIMIT == 100 * 1024 * 1024
    assert DEFAULT_UPLOAD_LIMIT == 20 * 1024 * 1024


def test_upload_media_type_normalizes_supported_video_extensions():
    assert _upload_media_type("clip.mov", "application/octet-stream") == (
        "video/quicktime",
        "video",
    )
    assert _upload_media_type("clip.avi", "video/x-msvideo") == ("video/avi", "video")
    assert _upload_media_type("clip.mkv", "video/x-matroska") == (
        "video/x-matroska",
        "document",
    )


async def test_upload_returns_agentk_managed_blob_url(tmp_path: Path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    uploads = FakeUploadStorage()
    app = create_app(Settings("http://agentk.test", "secret"), FakeAgentK(), uploads)
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://demo.test") as client:
        response = await client.post(
            "/api/uploads",
            files={"file": ("reference.png", b"\x89PNG\r\n\x1a\n", "image/png")},
        )
    assert response.status_code == 201
    result = response.json()
    assert result["url"].startswith("https://blob.test/bucket/agentk-capability-demo/")
    assert result["objectKey"].startswith("agentk-capability-demo/")
    assert result["previewUrl"].startswith("http://demo.test/uploads/")
    assert result["agentkReachable"] is True
    assert result["warning"] == ""
    assert uploads.calls[0][1:] == ("reference.png", 8, "image/png")
