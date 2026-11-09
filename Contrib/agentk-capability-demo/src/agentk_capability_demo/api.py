from __future__ import annotations

import asyncio
import json
import mimetypes
import secrets
from collections.abc import AsyncIterator
from pathlib import Path
from typing import Any

from agentk.api.v1 import event_pb, service_pb
from agentk.api.v1.profile_pb import Profile, ProfilePipeline
from fastapi import APIRouter, File, HTTPException, Request, UploadFile
from fastapi.responses import StreamingResponse
from protobuf import Oneof
from protobuf.wkt import Struct
from pydantic import BaseModel, Field

from .agentk_client import friendly_error
from .catalog import profile_for, public_agents
from .serialization import message_dict, proto_from_dict
from .store import AgentSession, RunRecord
from .voice_options import capability_is_public, enrich_capability_schema

router = APIRouter(prefix="/api")

DEFAULT_UPLOAD_LIMIT = 20 * 1024 * 1024
VIDEO_UPLOAD_LIMIT = 100 * 1024 * 1024
VIDEO_EXTENSIONS = {".mp4", ".mov", ".webm", ".mpeg", ".mpg", ".mkv", ".avi"}
VIDEO_MIME_BY_EXTENSION = {
    ".mp4": "video/mp4",
    ".mov": "video/quicktime",
    ".webm": "video/webm",
    ".mpeg": "video/mpeg",
    ".mpg": "video/mpg",
    ".avi": "video/avi",
}
AUDIO_MIME_BY_EXTENSION = {
    ".wav": "audio/wav",
    ".mp3": "audio/mpeg",
    ".aif": "audio/aiff",
    ".aiff": "audio/aiff",
    ".aac": "audio/aac",
    ".ogg": "audio/ogg",
    ".oga": "audio/ogg",
    ".flac": "audio/flac",
    ".mpga": "audio/mpeg",
    ".m4a": "audio/m4a",
    ".l16": "audio/l16",
    ".opus": "audio/opus",
    ".alaw": "audio/alaw",
    ".mulaw": "audio/mulaw",
}
SUPPORTED_VIDEO_MIMES = set(VIDEO_MIME_BY_EXTENSION.values())
SUPPORTED_IMAGE_MIMES = {"image/jpeg", "image/png", "image/webp"}
SUPPORTED_AUDIO_MIMES = {
    "audio/wav",
    "audio/mp3",
    "audio/aiff",
    "audio/aac",
    "audio/ogg",
    "audio/flac",
    "audio/mpeg",
    "audio/m4a",
    "audio/l16",
    "audio/opus",
    "audio/alaw",
    "audio/mulaw",
    "audio/webm",
}
AUDIO_MIME_ALIASES = {
    "audio/x-wav": "audio/wav",
    "audio/wave": "audio/wav",
    "audio/vnd.wave": "audio/wav",
    "audio/x-flac": "audio/flac",
    "audio/x-aiff": "audio/aiff",
    "audio/x-m4a": "audio/m4a",
    "audio/mp4": "audio/m4a",
}
NATIVE_IMAGE_MODELS = {
    "qwen3.5-plus",
    "gpt-5.4",
    "claude-sonnet-4-6",
    "claude-haiku-4-5",
    "kimi-k2.6",
    "gemini-3.1-pro-preview",
    "gemini-3-flash-preview",
    "gemini-3.1-flash-lite",
}
NATIVE_VIDEO_MODELS = {
    "gemini-3.1-pro-preview",
    "gemini-3-flash-preview",
    "gemini-3.1-flash-lite",
}
NATIVE_AUDIO_MODELS = set(NATIVE_VIDEO_MODELS)
AGENT_STREAM_INTERVAL_SECONDS = 1.2


class CapabilityRunInput(BaseModel):
    args: dict[str, Any] = Field(default_factory=dict)


class SessionInput(BaseModel):
    agent_id: str = Field(alias="agentId")
    title: str = "Local agent test"
    model: str | None = None


class AttachmentInput(BaseModel):
    uri: str
    name: str = "upload"
    type: str = "file"
    mime_type: str = Field(default="", alias="mimeType")


class MessageInput(BaseModel):
    text: str = ""
    attachments: list[AttachmentInput] = Field(default_factory=list)


class AnswerItem(BaseModel):
    question_id: str = Field(alias="questionId")
    selected: list[str]


class AnswersInput(BaseModel):
    tool_call_id: str = Field(alias="toolCallId")
    answers: list[AnswerItem]


def _client(request: Request):
    return request.app.state.agentk


def _store(request: Request):
    return request.app.state.store


def _sdk_error(exc: Exception) -> HTTPException:
    return HTTPException(status_code=502, detail=friendly_error(exc))


def _upload_limit(filename: str, content_type: str) -> int:
    suffix = Path(filename).suffix.lower()
    guessed = mimetypes.guess_type(filename)[0] or ""
    if content_type.lower().startswith("video/") or guessed.startswith("video/"):
        return VIDEO_UPLOAD_LIMIT
    if suffix in VIDEO_EXTENSIONS:
        return VIDEO_UPLOAD_LIMIT
    return DEFAULT_UPLOAD_LIMIT


def _upload_media_type(filename: str, content_type: str) -> tuple[str, str]:
    suffix = Path(filename).suffix.lower()
    mime = content_type.lower().split(";", 1)[0].strip()
    if suffix in VIDEO_MIME_BY_EXTENSION and (
        not mime or mime == "application/octet-stream" or mime.startswith("video/")
    ):
        mime = VIDEO_MIME_BY_EXTENSION[suffix]
    if suffix in AUDIO_MIME_BY_EXTENSION and (
        not mime or mime == "application/octet-stream" or mime.startswith("audio/")
    ):
        mime = AUDIO_MIME_BY_EXTENSION[suffix]
    mime = _canonical_audio_mime(mime)
    if not mime:
        mime = mimetypes.guess_type(filename)[0] or "application/octet-stream"
    kind = mime.split("/", 1)[0]
    if kind == "video" and mime not in SUPPORTED_VIDEO_MIMES:
        kind = "document"
    elif kind not in {"image", "video", "audio"}:
        kind = "document" if mime != "text/plain" else "text"
    return mime, kind


def _canonical_audio_mime(value: str) -> str:
    mime = value.lower().split(";", 1)[0].strip()
    return AUDIO_MIME_ALIASES.get(mime, mime)


@router.get("/config")
async def config(request: Request) -> dict[str, object]:
    return request.app.state.settings.public()


@router.get("/connection")
async def connection(request: Request) -> dict[str, object]:
    try:
        result = await _client(request).who_am_i(service_pb.WhoAmIRequest())
        return {"ok": True, "identity": message_dict(result)}
    except Exception as exc:
        return {"ok": False, "error": friendly_error(exc)}


@router.get("/capabilities")
async def list_capabilities(request: Request) -> dict[str, object]:
    capabilities: list[dict[str, Any]] = []
    token = ""
    try:
        while True:
            response = await _client(request).list_capabilities(
                service_pb.ListCapabilitiesRequest(page_size=100, page_token=token)
            )
            for item in response.capabilities:
                capability = message_dict(item)
                if capability_is_public(capability):
                    capabilities.append(enrich_capability_schema(capability))
            token = response.next_page_token
            if not token:
                break
    except Exception as exc:
        raise _sdk_error(exc) from exc
    return {"capabilities": capabilities, "count": len(capabilities)}


@router.post("/capabilities/{capability_id}/runs", status_code=202)
async def run_capability(
    capability_id: str, payload: CapabilityRunInput, request: Request
) -> dict[str, object]:
    pipeline_name = "capability_test"
    profile = Profile(
        default_model="deepseek-v4-pro",
        system="This session runs one capability from AgentK Capability Demo.",
        pipelines=[
            ProfilePipeline(
                capability=capability_id,
                name=pipeline_name,
                display_name=f"Test {capability_id}",
            )
        ],
    )
    try:
        created = await _client(request).create_session(
            service_pb.CreateSessionRequest(title=f"Capability: {capability_id}", profile=profile)
        )
        if created.session is None:
            raise RuntimeError("AgentK did not return a session")
        started = await _client(request).start_execution(
            service_pb.StartExecutionRequest(
                session_id=created.session.id,
                input=Oneof(
                    "pipeline",
                    service_pb.PipelineExecutionInput(
                        pipeline=pipeline_name,
                        args=proto_from_dict(Struct, payload.args),
                    ),
                ),
            )
        )
        if started.execution is None:
            raise RuntimeError("AgentK did not return an execution")
    except Exception as exc:
        raise _sdk_error(exc) from exc
    record = RunRecord(
        session_id=created.session.id,
        execution_id=started.execution.id,
        kind="capability",
        label=capability_id,
    )
    async with _store(request).lock:
        _store(request).runs[record.execution_id] = record
    return {"run": _run_dict(record), "execution": message_dict(started.execution)}


@router.get("/runs/{execution_id}")
async def get_run(execution_id: str, request: Request) -> dict[str, object]:
    record = _store(request).runs.get(execution_id)
    if record is None:
        raise HTTPException(status_code=404, detail="run not found")
    try:
        execution = await _client(request).get_execution(
            service_pb.GetExecutionRequest(
                session_id=record.session_id, execution_id=record.execution_id
            )
        )
        events = await _all_events(request, record.session_id)
    except Exception as exc:
        raise _sdk_error(exc) from exc
    return {
        "run": _run_dict(record),
        "execution": message_dict(execution.execution),
        "events": events,
    }


@router.delete("/runs/{execution_id}", status_code=204)
async def cancel_run(execution_id: str, request: Request) -> None:
    record = _store(request).runs.get(execution_id)
    if record is None:
        raise HTTPException(status_code=404, detail="run not found")
    try:
        await _client(request).cancel_execution(
            service_pb.CancelExecutionRequest(session_id=record.session_id)
        )
    except Exception as exc:
        raise _sdk_error(exc) from exc


@router.get("/agents")
async def list_agents() -> dict[str, object]:
    agents = public_agents()
    return {"agents": agents, "count": len(agents)}


@router.post("/agent-sessions", status_code=201)
async def create_agent_session(payload: SessionInput, request: Request) -> dict[str, object]:
    try:
        profile_data = profile_for(payload.agent_id, payload.model)
    except KeyError as exc:
        raise HTTPException(status_code=404, detail="agent case not found") from exc
    try:
        profile = proto_from_dict(Profile, profile_data)
        response = await _client(request).create_session(
            service_pb.CreateSessionRequest(title=payload.title, profile=profile)
        )
        if response.session is None:
            raise RuntimeError("AgentK did not return a session")
    except Exception as exc:
        raise _sdk_error(exc) from exc
    item = AgentSession(
        session_id=response.session.id,
        agent_id=payload.agent_id,
        title=payload.title,
        model=profile.default_model,
    )
    async with _store(request).lock:
        _store(request).sessions[item.session_id] = item
    return {"session": _session_dict(item), "agentkSession": message_dict(response.session)}


@router.post("/agent-sessions/{session_id}/messages", status_code=202)
async def send_message(
    session_id: str, payload: MessageInput, request: Request
) -> dict[str, object]:
    item = _store(request).sessions.get(session_id)
    if item is None:
        raise HTTPException(status_code=404, detail="agent session not found")
    if not payload.text.strip() and not payload.attachments:
        raise HTTPException(status_code=422, detail="text or attachments is required")
    text = payload.text.strip()
    if payload.attachments:
        refs = "\n".join(
            f"- {value.name}（{value.type or value.mime_type or 'file'}）：{value.uri}"
            for value in payload.attachments
        )
        prompt = text or "请处理以下本地素材。"
        text = f"{prompt}\n\n用户上传的本地素材（可访问 URL）：\n{refs}"
    media_attachments: list[event_pb.MediaAttachment] = []
    video_attached = False
    for value in payload.attachments:
        attachment_type = value.type.lower().strip()
        mime_type = value.mime_type.lower().split(";", 1)[0].strip()
        is_video = attachment_type == "video" or mime_type.startswith("video/")
        is_image = attachment_type == "image" or mime_type.startswith("image/")
        is_audio = attachment_type == "audio" or mime_type.startswith("audio/")
        if is_video:
            if (
                item.model not in NATIVE_VIDEO_MODELS
                or video_attached
                or mime_type not in SUPPORTED_VIDEO_MIMES
            ):
                continue
            video_attached = True
            media_attachments.append(
                event_pb.MediaAttachment(type="video", uri=value.uri, mime_type=mime_type)
            )
        elif (
            is_image
            and item.model in NATIVE_IMAGE_MODELS
            and mime_type in SUPPORTED_IMAGE_MIMES
        ):
            media_attachments.append(
                event_pb.MediaAttachment(type="image", uri=value.uri, mime_type=mime_type)
            )
        elif is_audio and item.model in NATIVE_AUDIO_MODELS:
            mime_type = _canonical_audio_mime(mime_type)
            if mime_type in SUPPORTED_AUDIO_MIMES:
                media_attachments.append(
                    event_pb.MediaAttachment(type="audio", uri=value.uri, mime_type=mime_type)
                )
    try:
        response = await _client(request).start_execution(
            service_pb.StartExecutionRequest(
                session_id=session_id,
                input=Oneof(
                    "message",
                    service_pb.AgentMessageExecutionInput(
                        text=text,
                        attachments=media_attachments,
                    ),
                ),
            )
        )
        if response.execution is None:
            raise RuntimeError("AgentK did not return an execution")
    except Exception as exc:
        raise _sdk_error(exc) from exc
    item.execution_id = response.execution.id
    record = RunRecord(
        session_id=session_id,
        execution_id=item.execution_id,
        kind="agent",
        label=item.agent_id,
    )
    async with _store(request).lock:
        _store(request).runs[record.execution_id] = record
    return {"session": _session_dict(item), "execution": message_dict(response.execution)}


@router.post("/agent-sessions/{session_id}/answers", status_code=202)
async def answer_questions(
    session_id: str, payload: AnswersInput, request: Request
) -> dict[str, object]:
    item = _store(request).sessions.get(session_id)
    if item is None:
        raise HTTPException(status_code=404, detail="agent session not found")
    try:
        response = await _client(request).start_execution(
            service_pb.StartExecutionRequest(
                session_id=session_id,
                input=Oneof(
                    "answers",
                    service_pb.AgentAnswersExecutionInput(
                        tool_call_id=payload.tool_call_id,
                        answers=[
                            service_pb.AnswerInput(
                                question_id=answer.question_id, selected=answer.selected
                            )
                            for answer in payload.answers
                        ],
                    ),
                ),
            )
        )
        if response.execution is None:
            raise RuntimeError("AgentK did not return an execution")
    except Exception as exc:
        raise _sdk_error(exc) from exc
    item.execution_id = response.execution.id
    record = RunRecord(session_id, item.execution_id, "agent", item.agent_id)
    async with _store(request).lock:
        _store(request).runs[record.execution_id] = record
    return {"session": _session_dict(item), "execution": message_dict(response.execution)}


@router.get("/agent-sessions/{session_id}")
async def get_agent_session(session_id: str, request: Request) -> dict[str, object]:
    return await _agent_session_snapshot(session_id, request)


async def _agent_session_snapshot(session_id: str, request: Request) -> dict[str, object]:
    item = _store(request).sessions.get(session_id)
    if item is None:
        raise HTTPException(status_code=404, detail="agent session not found")
    try:
        events = await _all_events(request, session_id)
        execution: dict[str, Any] = {}
        if item.execution_id:
            response = await _client(request).get_execution(
                service_pb.GetExecutionRequest(
                    session_id=session_id, execution_id=item.execution_id
                )
            )
            execution = message_dict(response.execution)
    except Exception as exc:
        raise _sdk_error(exc) from exc
    return {"session": _session_dict(item), "execution": execution, "events": events}


def _terminal_execution(execution: dict[str, Any]) -> bool:
    status = str(execution.get("status", "")).upper()
    return any(value in status for value in ("COMPLETED", "FAILED", "CANCELLED"))


@router.get("/agent-sessions/{session_id}/events")
async def stream_agent_session(session_id: str, request: Request) -> StreamingResponse:
    if session_id not in _store(request).sessions:
        raise HTTPException(status_code=404, detail="agent session not found")

    async def snapshots() -> AsyncIterator[str]:
        last_payload = ""
        failures = 0
        idle_polls = 0
        while not await request.is_disconnected():
            try:
                snapshot = await _agent_session_snapshot(session_id, request)
                payload = json.dumps(
                    snapshot,
                    ensure_ascii=False,
                    separators=(",", ":"),
                    sort_keys=True,
                )
            except Exception as exc:
                failures += 1
                if failures >= 5:
                    error = json.dumps(
                        {"error": friendly_error(exc)}, ensure_ascii=False, separators=(",", ":")
                    )
                    yield f"event: agent-error\ndata: {error}\n\n"
                    return
            else:
                failures = 0
                if payload != last_payload:
                    yield f"retry: 1500\nevent: snapshot\ndata: {payload}\n\n"
                    last_payload = payload
                    idle_polls = 0
                else:
                    idle_polls += 1
                    if idle_polls >= 10:
                        yield ": keep-alive\n\n"
                        idle_polls = 0
                if _terminal_execution(snapshot["execution"]):
                    return
            await asyncio.sleep(AGENT_STREAM_INTERVAL_SECONDS)

    return StreamingResponse(
        snapshots(),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )


@router.delete("/agent-sessions/{session_id}/execution", status_code=204)
async def cancel_agent(session_id: str, request: Request) -> None:
    if session_id not in _store(request).sessions:
        raise HTTPException(status_code=404, detail="agent session not found")
    try:
        await _client(request).cancel_execution(
            service_pb.CancelExecutionRequest(session_id=session_id)
        )
    except Exception as exc:
        raise _sdk_error(exc) from exc


@router.post("/uploads", status_code=201)
async def upload(request: Request, file: UploadFile = File(...)) -> dict[str, object]:
    original_name = Path(file.filename or "upload").name
    mime, kind = _upload_media_type(original_name, file.content_type or "")
    limit = _upload_limit(original_name, mime)
    suffix = Path(original_name).suffix.lower()[:12]
    filename = f"{secrets.token_hex(16)}{suffix}"
    target = request.app.state.upload_dir / filename
    size = 0
    try:
        with target.open("wb") as output:
            while chunk := await file.read(1024 * 1024):
                size += len(chunk)
                if size > limit:
                    maximum = limit // (1024 * 1024)
                    raise HTTPException(
                        status_code=413,
                        detail=f"file must be between 1 byte and {maximum} MB",
                    )
                output.write(chunk)
        if size == 0:
            raise HTTPException(status_code=413, detail="file must not be empty")
    except Exception:
        target.unlink(missing_ok=True)
        raise
    base = request.app.state.settings.public_base_url
    local_url = str(request.base_url).rstrip("/") + "/uploads/" + filename
    managed_url = ""
    object_key = ""
    uploads = request.app.state.upload_storage
    if uploads is not None:
        try:
            managed_url, object_key = await uploads.put(target, original_name, size, mime)
        except Exception as exc:
            raise HTTPException(status_code=502, detail=str(exc)) from exc
    return {
        "name": original_name,
        "size": size,
        "type": kind,
        "mimeType": mime,
        "url": managed_url or (f"{base}/uploads/{filename}" if base else local_url),
        "previewUrl": local_url,
        "objectKey": object_key,
        "agentkReachable": bool(managed_url or base),
        "warning": "未配置 BlobStore 或 --public-base-url；远端 AgentK 可能无法读取本机 URL"
        if not managed_url and not base
        else "",
    }


async def _all_events(request: Request, session_id: str) -> list[dict[str, Any]]:
    result: list[dict[str, Any]] = []
    token = ""
    while True:
        response = await _client(request).list_events(
            service_pb.ListEventsRequest(session_id=session_id, page_size=100, page_token=token)
        )
        result.extend(message_dict(event) for event in response.events)
        token = response.next_page_token
        if not token:
            break
    result.reverse()
    return result


def _run_dict(value: RunRecord) -> dict[str, str]:
    return {
        "sessionId": value.session_id,
        "executionId": value.execution_id,
        "kind": value.kind,
        "label": value.label,
        "createdAt": value.created_at,
    }


def _session_dict(value: AgentSession) -> dict[str, str]:
    return {
        "sessionId": value.session_id,
        "agentId": value.agent_id,
        "title": value.title,
        "model": value.model,
        "executionId": value.execution_id,
        "createdAt": value.created_at,
    }
