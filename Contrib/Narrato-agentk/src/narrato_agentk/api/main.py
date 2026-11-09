from __future__ import annotations

import hmac
from pathlib import Path
from typing import Annotated, Any, Literal

from fastapi import Depends, FastAPI, File, Header, HTTPException, Request, UploadFile, status
from fastapi.responses import FileResponse
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel, ConfigDict, Field

from narrato_agentk.agentk_core.catalog import QWEN_VOICE_OPTIONS
from narrato_agentk.contracts import (
    CREATE_INPUT_SCHEMA,
    GENERATE_INPUT_SCHEMA,
    RENDER_INPUT_SCHEMA,
)
from narrato_agentk.media import BS3Backend, MediaUploadService, UploadRejected
from narrato_agentk.settings import Settings
from narrato_agentk.ui import AgentKConsoleService, ConsoleService

_STATIC_DIR = Path(__file__).resolve().parent.parent / "ui" / "static"


class StartConsoleExecution(BaseModel):
    model_config = ConfigDict(populate_by_name=True, extra="forbid")

    pipeline: Literal["documentary_video", "documentary_script", "documentary_render"]
    args: dict[str, Any]
    session_id: str | None = Field(default=None, alias="sessionId")
    title: str = "Narrato Browser Test"


def create_app(
    settings: Settings | None = None,
    upload_service: MediaUploadService | None = None,
    console_service: ConsoleService | None = None,
) -> FastAPI:
    resolved = settings or Settings.load()
    resolved.validate_api()
    if upload_service is None:
        resolved.validate_media()
        upload_service = MediaUploadService(
            BS3Backend(
                endpoint=resolved.bs3_endpoint,
                bucket=resolved.upload_bucket,
                cdn_base=resolved.cdn_base,
            ),
            key_prefix=resolved.media_key_prefix,
        )
    if console_service is None:
        console_service = AgentKConsoleService(resolved)

    app = FastAPI(title="Narrato AgentK Media API", version="0.1.0")
    app.mount("/static", StaticFiles(directory=_STATIC_DIR), name="static")

    async def require_api_access(
        request: Request,
        console_token: Annotated[str | None, Header(alias="X-Narrato-Console-Token")] = None,
    ) -> None:
        if resolved.console_token:
            if console_token is None or not hmac.compare_digest(
                console_token,
                resolved.console_token,
            ):
                raise HTTPException(status_code=401, detail="invalid console token")
            return
        client_host = request.client.host if request.client is not None else ""
        if client_host not in {"127.0.0.1", "::1", "testclient"}:
            raise HTTPException(
                status_code=403,
                detail="remote API access requires NARRATO_CONSOLE_TOKEN",
            )

    @app.middleware("http")
    async def add_browser_security_headers(request: Request, call_next: Any) -> Any:
        response = await call_next(request)
        response.headers["Content-Security-Policy"] = (
            "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; "
            "object-src 'none'; script-src 'self'; style-src 'self'; "
            "img-src 'self' data:; connect-src 'self'; form-action 'self'"
        )
        response.headers["Permissions-Policy"] = "camera=(), microphone=(), geolocation=()"
        response.headers["Referrer-Policy"] = "no-referrer"
        response.headers["X-Content-Type-Options"] = "nosniff"
        response.headers["X-Frame-Options"] = "DENY"
        return response

    @app.get("/", include_in_schema=False)
    async def console_page() -> FileResponse:
        return FileResponse(_STATIC_DIR / "index.html")

    @app.get("/healthz")
    async def healthz() -> dict[str, str]:
        return {"status": "ok"}

    @app.post(
        "/v1/media",
        status_code=status.HTTP_201_CREATED,
        dependencies=[Depends(require_api_access)],
    )
    async def upload_media(file: Annotated[UploadFile, File()]) -> dict[str, object]:
        try:
            result = await upload_service.upload(
                file,
                file.content_type or "",
                file.filename or "",
            )
        except UploadRejected as error:
            detail = str(error)
            code = (
                status.HTTP_413_CONTENT_TOO_LARGE
                if "exceeds" in detail
                else status.HTTP_422_UNPROCESSABLE_CONTENT
            )
            raise HTTPException(status_code=code, detail=detail) from error
        finally:
            await file.close()
        return result.to_dict()

    @app.get("/v1/ui/config", dependencies=[Depends(require_api_access)])
    async def console_config() -> dict[str, Any]:
        return {
            "models": {
                "vision": resolved.vision_model,
                "text": resolved.text_model,
                "tts": resolved.tts_model,
                "selectionEnabled": False,
            },
            "voices": {
                "qwen": [dict(option) for option in QWEN_VOICE_OPTIONS],
            },
            "pipelines": [
                {
                    "name": "documentary_video",
                    "displayName": "一键成片",
                    "inputSchema": CREATE_INPUT_SCHEMA,
                },
                {
                    "name": "documentary_script",
                    "displayName": "生成脚本",
                    "inputSchema": GENERATE_INPUT_SCHEMA,
                },
                {
                    "name": "documentary_render",
                    "displayName": "脚本渲染",
                    "inputSchema": RENDER_INPUT_SCHEMA,
                },
            ],
        }

    @app.post(
        "/v1/ui/executions",
        status_code=status.HTTP_201_CREATED,
        dependencies=[Depends(require_api_access)],
    )
    async def start_console_execution(body: StartConsoleExecution) -> dict[str, Any]:
        try:
            return await console_service.start_execution(
                body.pipeline,
                body.args,
                session_id=body.session_id,
                title=body.title,
            )
        except ValueError as error:
            raise HTTPException(status_code=422, detail=str(error)) from error
        except Exception as error:
            raise HTTPException(
                status_code=502,
                detail=f"AgentK request failed: {type(error).__name__}",
            ) from error

    @app.get(
        "/v1/ui/executions/{session_id}/{execution_id}",
        dependencies=[Depends(require_api_access)],
    )
    async def get_console_execution(session_id: str, execution_id: str) -> dict[str, Any]:
        try:
            return await console_service.get_execution(session_id, execution_id)
        except ValueError as error:
            raise HTTPException(status_code=404, detail=str(error)) from error
        except Exception as error:
            raise HTTPException(
                status_code=502,
                detail=f"AgentK request failed: {type(error).__name__}",
            ) from error

    @app.post(
        "/v1/ui/sessions/{session_id}/cancel",
        dependencies=[Depends(require_api_access)],
    )
    async def cancel_console_execution(session_id: str) -> dict[str, Any]:
        try:
            return await console_service.cancel_execution(session_id)
        except Exception as error:
            raise HTTPException(
                status_code=502,
                detail=f"AgentK request failed: {type(error).__name__}",
            ) from error

    return app


def main() -> None:
    import uvicorn

    settings = Settings.load()
    settings.validate_api()
    uvicorn.run(create_app(settings), host=settings.api_host, port=8000)


if __name__ == "__main__":
    main()
