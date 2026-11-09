from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from fastapi.testclient import TestClient

from narrato_agentk.api.main import create_app
from narrato_agentk.settings import Settings


@dataclass
class UploadResult:
    def to_dict(self) -> dict[str, object]:
        return {
            "uri": "https://cdn.test/source.mp4",
            "sha256": "abc",
            "sizeBytes": 3,
            "contentType": "video/mp4",
        }


class UploadService:
    async def upload(self, reader: Any, content_type: str, filename: str) -> UploadResult:
        await reader.read()
        return UploadResult()


class ConsoleService:
    def __init__(self) -> None:
        self.started: tuple[str, dict[str, Any], str | None, str] | None = None
        self.cancelled: str | None = None

    async def start_execution(
        self,
        pipeline: str,
        args: dict[str, Any],
        *,
        session_id: str | None,
        title: str,
    ) -> dict[str, Any]:
        self.started = (pipeline, args, session_id, title)
        return {
            "sessionId": session_id or "session-new",
            "executionId": "execution-1",
            "status": "running",
        }

    async def get_execution(self, session_id: str, execution_id: str) -> dict[str, Any]:
        return {
            "sessionId": session_id,
            "executionId": execution_id,
            "status": "completed",
            "error": "",
            "events": [
                {
                    "id": "event-1",
                    "type": "progress",
                    "step": "complete",
                    "message": "完成",
                    "data": {"percent": 100},
                }
            ],
            "output": {
                "version": "1",
                "sourceVideoUri": "https://cdn.test/source.mp4",
                "sourceDurationSeconds": 10,
                "theme": "",
                "segments": [],
            },
        }

    async def cancel_execution(self, session_id: str) -> dict[str, Any]:
        self.cancelled = session_id
        return {"sessionId": session_id, "cancelRequested": True}


def _client() -> tuple[TestClient, ConsoleService]:
    console = ConsoleService()
    app = create_app(
        Settings(
            vision_model="vision-fixed",
            text_model="text-fixed",
            tts_model="tts-fixed",
        ),
        UploadService(),
        console,
    )
    return TestClient(app), console


def test_console_page_and_static_assets_are_served():
    client, _ = _client()

    page = client.get("/")
    assert page.status_code == 200
    assert "Narrato Control Room" in page.text
    assert "documentary_video" in page.text
    assert '<select id="voice-id"' in page.text
    assert '<input id="voice-id"' not in page.text
    assert '/static/app.js?v=20260818-voices' in page.text
    assert client.get("/static/app.css").status_code == 200
    script = client.get("/static/app.js")
    assert script.status_code == 200
    assert 'url.startsWith("/v1/")' in script.text
    assert "function safeHttpUrl" in script.text
    assert 'url.protocol === "http:"' in script.text
    assert "const artifactUri = safeHttpUrl(video?.uri)" in script.text


def test_console_config_exposes_current_contracts_without_credentials():
    client, _ = _client()

    response = client.get("/v1/ui/config")

    assert response.status_code == 200
    assert response.json()["models"] == {
        "vision": "vision-fixed",
        "text": "text-fixed",
        "tts": "tts-fixed",
        "selectionEnabled": False,
    }
    assert response.json()["voices"] == {
        "qwen": [
            {"id": "Cherry", "description": "芊晓：阳光亲切女声"},
            {"id": "Serena", "description": "佩霞：温婉女声"},
            {"id": "Ethan", "description": "晨煦：阳光稳重男声"},
            {"id": "Christie", "description": "千雪：二次元萌系女声"},
            {"id": "Rono", "description": "元兔：俏皮活泼女声"},
            {"id": "Vivian", "description": "十三：甜美女声"},
            {"id": "Ryan", "description": "沐白：清爽大气男声"},
            {"id": "Rana", "description": "兰月：温柔女声"},
            {"id": "Kai", "description": "凯：未来感男声"},
            {"id": "Nafish", "description": "不败枭：低沉男声"},
            {"id": "Bella", "description": "碧宝：俏皮稳重女声"},
            {"id": "Jennifer", "description": "珍妮弗：知性磁性女声"},
        ]
    }
    assert [item["name"] for item in response.json()["pipelines"]] == [
        "documentary_video",
        "documentary_script",
        "documentary_render",
    ]
    assert "agentkApiKey" not in response.text


def test_console_starts_pipeline_with_exact_browser_input():
    client, console = _client()
    args = {
        "videoUri": "https://cdn.test/source.mp4",
        "theme": "自然",
        "instructions": "克制",
        "voiceId": "Cherry",
        "aspect": "source",
        "narrationVolume": 1,
        "subtitles": True,
    }

    response = client.post(
        "/v1/ui/executions",
        json={
            "pipeline": "documentary_video",
            "args": args,
            "sessionId": "session-existing",
            "title": "浏览器测试",
        },
    )

    assert response.status_code == 201
    assert response.json() == {
        "sessionId": "session-existing",
        "executionId": "execution-1",
        "status": "running",
    }
    assert console.started == (
        "documentary_video",
        args,
        "session-existing",
        "浏览器测试",
    )


def test_console_rejects_unknown_pipeline_before_calling_agentk():
    client, console = _client()

    response = client.post(
        "/v1/ui/executions",
        json={"pipeline": "arbitrary_tool", "args": {}},
    )

    assert response.status_code == 422
    assert console.started is None


def test_console_polls_output_and_cancels_by_session():
    client, console = _client()

    fetched = client.get("/v1/ui/executions/session-1/execution-1")
    cancelled = client.post("/v1/ui/sessions/session-1/cancel")

    assert fetched.status_code == 200
    assert fetched.json()["status"] == "completed"
    assert fetched.json()["events"][0]["data"]["percent"] == 100
    assert cancelled.json() == {"sessionId": "session-1", "cancelRequested": True}
    assert console.cancelled == "session-1"


def test_console_token_protects_agentk_proxy_endpoints():
    app = create_app(
        Settings(console_token="browser-secret"),
        UploadService(),
        ConsoleService(),
    )
    client = TestClient(app)

    assert client.get("/v1/ui/config").status_code == 401
    response = client.get(
        "/v1/ui/config",
        headers={"X-Narrato-Console-Token": "browser-secret"},
    )
    assert response.status_code == 200
    assert "browser-secret" not in response.text
