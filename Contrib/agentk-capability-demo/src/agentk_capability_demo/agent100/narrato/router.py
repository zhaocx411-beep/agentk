from __future__ import annotations

import asyncio
from dataclasses import replace
from typing import Annotated, Any, Literal

from fastapi import APIRouter, File, HTTPException, Request, UploadFile, status
from pydantic import BaseModel, ConfigDict, Field

from .agentk_core.catalog import MINIMAX_VOICE_OPTIONS, QWEN_VOICE_OPTIONS
from .contracts import (
    CREATE_INPUT_SCHEMA,
    GENERATE_INPUT_SCHEMA,
    RENDER_INPUT_SCHEMA,
    style_options,
)
from .history import NarratoExecutionHistory
from .media import MediaUploadService, UploadFailed, UploadRejected
from .service import AgentKConsoleService
from .settings import QWEN_TTS_INSTRUCT_MODEL, Settings

router = APIRouter()


class StartNarratoExecution(BaseModel):
    model_config = ConfigDict(populate_by_name=True, extra="forbid")

    pipeline: Literal["documentary_video", "documentary_script", "documentary_render"]
    args: dict[str, Any]
    session_id: str | None = Field(default=None, alias="sessionId")
    title: str = "Narrato Browser Test"


def configure_narrato(app: Any, root_settings: Any) -> None:
    loaded = Settings.load()
    shared_cdn_base = root_settings.blob_cdn_base
    if not shared_cdn_base and root_settings.blob_endpoint and root_settings.blob_bucket:
        shared_cdn_base = (
            f"{root_settings.blob_endpoint.rstrip('/')}/"
            f"{root_settings.blob_bucket.strip('/')}"
        )
    settings = replace(
        loaded,
        agentk_base_url=root_settings.agentk_address,
        agentk_api_key=root_settings.api_key,
        upload_backend="http",
        upload_bucket=root_settings.blob_bucket or "",
        bs3_endpoint=root_settings.blob_endpoint or "",
        cdn_base=shared_cdn_base or "",
    )
    app.state.narrato_settings = settings
    app.state.narrato_console = AgentKConsoleService(settings)
    app.state.narrato_history = NarratoExecutionHistory()
    app.state.narrato_upload = None
    app.state.narrato_media_error = ""
    try:
        settings.validate_media()
        app.state.narrato_upload = _media_service(settings)
    except Exception as exc:  # UI must remain available before media is configured.
        app.state.narrato_media_error = str(exc)


def _media_service(settings: Settings) -> MediaUploadService:
    from .media import BS3Backend, KessBlobStoreBackend

    backend = (
        KessBlobStoreBackend(
            database=settings.upload_database,
            bucket=settings.upload_bucket,
            cdn_base=settings.cdn_base,
        )
        if settings.upload_backend.strip().lower() == "blobstore"
        else BS3Backend(
            endpoint=settings.bs3_endpoint,
            bucket=settings.upload_bucket,
            cdn_base=settings.cdn_base,
        )
    )
    return MediaUploadService(backend, key_prefix=settings.media_key_prefix)


@router.get("/config")
async def config(request: Request) -> dict[str, Any]:
    settings: Settings = request.app.state.narrato_settings
    capability_error = ""
    try:
        capabilities = await request.app.state.narrato_console.get_capability_status()
    except Exception as exc:
        capabilities = []
        capability_error = f"{type(exc).__name__}: AgentK capability query failed"
    tts_catalog = _tts_catalog(capabilities, settings)
    return {
        "models": {
            "defaults": settings.model_defaults(),
            "options": {key: list(value) for key, value in settings.model_options().items()},
        },
        "capabilities": capabilities,
        "capabilityError": capability_error,
        "mediaError": request.app.state.narrato_media_error,
        "tts": tts_catalog,
        "voices": {
            "qwen": [dict(option) for option in QWEN_VOICE_OPTIONS],
            "minimax": [dict(option) for option in MINIMAX_VOICE_OPTIONS],
        },
        "styles": style_options(),
        "pipelines": [
            {"name": "documentary_video", "displayName": "一键成片", "inputSchema": CREATE_INPUT_SCHEMA},
            {"name": "documentary_script", "displayName": "生成脚本", "inputSchema": GENERATE_INPUT_SCHEMA},
            {"name": "documentary_render", "displayName": "脚本渲染", "inputSchema": RENDER_INPUT_SCHEMA},
        ],
    }


def _tts_catalog(capabilities: list[dict[str, Any]], settings: Settings) -> dict[str, Any]:
    rows = {str(item.get("id") or ""): item for item in capabilities}
    definitions = (
        ("qwen", "agentk.generate_speech_qwen", QWEN_VOICE_OPTIONS),
        ("minimax", "agentk.generate_speech_minimax", MINIMAX_VOICE_OPTIONS),
    )
    providers: dict[str, Any] = {}
    for provider, capability_id, voices in definitions:
        row = rows.get(capability_id)
        if not row or not row.get("published"):
            continue
        schema = row.get("inputSchema")
        properties = schema.get("properties", {}) if isinstance(schema, dict) else {}
        model_schema = properties.get("model", {}) if isinstance(properties, dict) else {}
        advertised = model_schema.get("enum") if isinstance(model_schema, dict) else None
        allowed_models = settings.tts_model_options()[provider]
        models = (
            [str(item) for item in advertised if str(item) in allowed_models]
            if isinstance(advertised, list) and advertised
            else list(allowed_models)
        )
        if not models:
            continue
        schema_default = (
            str(model_schema.get("default") or "").strip()
            if isinstance(model_schema, dict)
            else ""
        )
        configured_default = settings.tts_defaults()[provider]
        preferred_model = QWEN_TTS_INSTRUCT_MODEL if provider == "qwen" else ""
        default_model = (
            preferred_model
            if preferred_model in models
            else schema_default
            if schema_default in models
            else configured_default
            if configured_default in models
            else models[0]
        )
        providers[provider] = {
            "capabilityId": capability_id,
            "defaultModel": default_model,
            "models": models,
            "defaultVoiceId": "Cherry" if provider == "qwen" else voices[0]["id"],
            "voices": [dict(option) for option in voices],
        }
    default_provider = "qwen" if "qwen" in providers else next(iter(providers), "")
    return {"defaultProvider": default_provider, "providers": providers}


@router.post("/media", status_code=status.HTTP_201_CREATED)
async def upload_media(request: Request, file: Annotated[UploadFile, File()]) -> dict[str, object]:
    service = request.app.state.narrato_upload
    if service is None:
        raise HTTPException(status_code=503, detail=request.app.state.narrato_media_error)
    try:
        result = await service.upload(file, file.content_type or "", file.filename or "")
    except UploadRejected as exc:
        code = 413 if "exceeds" in str(exc) else 422
        raise HTTPException(status_code=code, detail=str(exc)) from exc
    except UploadFailed as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc
    finally:
        await file.close()
    return result.to_dict()


@router.post("/executions", status_code=status.HTTP_201_CREATED)
async def start_execution(body: StartNarratoExecution, request: Request) -> dict[str, Any]:
    try:
        started = await request.app.state.narrato_console.start_execution(
            body.pipeline, body.args, session_id=body.session_id, title=body.title
        )
        request.app.state.narrato_history.add(
            body.pipeline,
            body.title,
            started,
            body.model_dump(by_alias=True, exclude_none=True),
        )
        return started
    except ValueError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
    except Exception as exc:
        raise HTTPException(status_code=502, detail=f"AgentK request failed: {type(exc).__name__}") from exc


@router.get("/executions")
async def list_executions(request: Request, refresh: bool = False) -> dict[str, Any]:
    if refresh:
        running = [
            item
            for item in request.app.state.narrato_history.list()
            if str(item.get("status") or "running").lower()
            not in {"completed", "failed", "cancelled"}
        ]
        await asyncio.gather(
            *(_refresh_history_item(request, item) for item in running),
            return_exceptions=True,
        )
    items = request.app.state.narrato_history.list()
    return {"items": items, "count": len(items)}


async def _refresh_history_item(request: Request, item: dict[str, Any]) -> None:
    snapshot = await request.app.state.narrato_console.get_execution(
        item["sessionId"], item["executionId"]
    )
    request.app.state.narrato_history.update(
        item["sessionId"], item["executionId"], snapshot
    )


@router.get("/executions/{session_id}/{execution_id}")
async def get_execution(session_id: str, execution_id: str, request: Request) -> dict[str, Any]:
    try:
        snapshot = await request.app.state.narrato_console.get_execution(session_id, execution_id)
        record = request.app.state.narrato_history.update(session_id, execution_id, snapshot)
        return record
    except ValueError as exc:
        cached = request.app.state.narrato_history.get(session_id, execution_id)
        if cached is not None:
            return {**cached, "cached": True, "statusSyncError": str(exc)}
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except Exception as exc:
        cached = request.app.state.narrato_history.get(session_id, execution_id)
        if cached is not None:
            return {
                **cached,
                "cached": True,
                "statusSyncError": f"AgentK request failed: {type(exc).__name__}",
            }
        raise HTTPException(status_code=502, detail=f"AgentK request failed: {type(exc).__name__}") from exc


@router.post("/sessions/{session_id}/cancel")
async def cancel_execution(session_id: str, request: Request) -> dict[str, Any]:
    try:
        return await request.app.state.narrato_console.cancel_execution(session_id)
    except Exception as exc:
        raise HTTPException(status_code=502, detail=f"AgentK request failed: {type(exc).__name__}") from exc


@router.post("/executions/{session_id}/{execution_id}/cancel")
async def cancel_history_execution(
    session_id: str, execution_id: str, request: Request
) -> dict[str, Any]:
    history: NarratoExecutionHistory = request.app.state.narrato_history
    record = history.get(session_id, execution_id)
    if record is None:
        raise HTTPException(status_code=404, detail="Narrato execution record not found")
    if str(record.get("status") or "").lower() in {"completed", "failed", "cancelled"}:
        raise HTTPException(status_code=409, detail="Narrato execution is already terminal")
    try:
        result = await request.app.state.narrato_console.cancel_execution(session_id)
        history.mark_cancel_requested(session_id, execution_id)
        return {**result, "sessionId": session_id, "executionId": execution_id}
    except Exception as exc:
        raise HTTPException(status_code=502, detail=f"AgentK request failed: {type(exc).__name__}") from exc
