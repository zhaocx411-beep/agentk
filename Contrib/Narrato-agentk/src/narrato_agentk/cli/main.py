from __future__ import annotations

import argparse
import asyncio
import json
import sys
from pathlib import Path
from typing import Any

import httpx
from agentk.api.v1 import model_pb, profile_pb, service_pb
from protobuf import Oneof
from protobuf.wkt import struct_pb

from narrato_agentk.agentk_client import create_agentk_client
from narrato_agentk.settings import Settings
from narrato_agentk.workflows import (
    CREATE_CAPABILITY_ID,
    GENERATE_CAPABILITY_ID,
    RENDER_CAPABILITY_ID,
)

PIPELINE_PROFILES = (
    (CREATE_CAPABILITY_ID, "documentary_video", "纪录片解说一键成片"),
    (GENERATE_CAPABILITY_ID, "documentary_script", "生成纪录片解说脚本"),
    (RENDER_CAPABILITY_ID, "documentary_render", "按脚本渲染纪录片解说视频"),
)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="narrato-agentk")
    subcommands = parser.add_subparsers(dest="command", required=True)

    upload = subcommands.add_parser("upload", help="upload a source video")
    upload.add_argument("file", type=Path)

    session = subcommands.add_parser("session", help="manage AgentK sessions")
    session_commands = session.add_subparsers(dest="session_command", required=True)
    create = session_commands.add_parser("create")
    create.add_argument("--title", default="Narrato AgentK Documentary")

    run = subcommands.add_parser("run", help="run the end-to-end documentary pipeline")
    _add_source(run)
    _add_session(run)
    _add_generation(run)
    _add_render(run)
    run.add_argument("--output", type=Path)

    script = subcommands.add_parser("script", help="generate an editable script")
    script_commands = script.add_subparsers(dest="script_command", required=True)
    generate = script_commands.add_parser("generate")
    _add_source(generate)
    _add_session(generate)
    _add_generation(generate)
    generate.add_argument("--output", type=Path, required=True)

    render = subcommands.add_parser("render", help="render a previously generated script")
    render.add_argument("--script", type=Path, required=True)
    _add_session(render)
    _add_render(render)
    render.add_argument("--output", type=Path)

    watch = subcommands.add_parser("watch", help="watch an AgentK execution")
    watch.add_argument("--session-id", required=True)
    watch.add_argument("--execution-id", required=True)

    cancel = subcommands.add_parser("cancel", help="cancel the active execution in a session")
    cancel.add_argument("--session-id", required=True)

    download = subcommands.add_parser("download", help="download an artifact")
    download.add_argument("--uri", required=True)
    download.add_argument("--output", type=Path, required=True)
    return parser


def _add_source(parser: argparse.ArgumentParser) -> None:
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--video-uri")
    group.add_argument("--file", type=Path)


def _add_session(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--session-id", help="reuse an existing session; otherwise create one")


def _add_generation(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--theme", default="")
    parser.add_argument("--instructions", default="")


def _add_render(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--voice-id")
    parser.add_argument("--aspect", choices=("source", "16:9", "9:16", "1:1"), default="source")
    parser.add_argument("--narration-volume", type=float, default=1.0)
    parser.add_argument("--no-subtitles", action="store_true")


async def _upload(path: Path, settings: Settings) -> str:
    if not path.is_file():
        raise ValueError(f"video file does not exist: {path}")
    content_type = _video_content_type(path)
    headers = {"X-Narrato-Console-Token": settings.console_token} if settings.console_token else {}
    with path.open("rb") as handle:
        async with httpx.AsyncClient(timeout=None) as client:
            response = await client.post(
                f"{settings.media_api_url.rstrip('/')}/v1/media",
                files={"file": (path.name, handle, content_type)},
                headers=headers,
            )
            response.raise_for_status()
            payload = response.json()
    uri = str(payload.get("uri") or "")
    if not uri:
        raise RuntimeError("media API returned no URI")
    print(uri)
    return uri


def _video_content_type(path: Path) -> str:
    mapping = {
        ".mp4": "video/mp4",
        ".mov": "video/quicktime",
        ".mkv": "video/x-matroska",
        ".webm": "video/webm",
    }
    try:
        return mapping[path.suffix.lower()]
    except KeyError as error:
        raise ValueError(f"unsupported video extension: {path.suffix or '(missing)'}") from error


async def _create_session(client: Any, settings: Settings, title: str) -> str:
    response = await client.create_session(
        service_pb.CreateSessionRequest(
            title=title,
            profile=profile_pb.Profile(
                default_model=settings.text_model,
                pipelines=[
                    profile_pb.ProfilePipeline(
                        capability=capability,
                        name=name,
                        display_name=display,
                    )
                    for capability, name, display in PIPELINE_PROFILES
                ],
            ),
        )
    )
    if response.session is None or not response.session.id:
        raise RuntimeError("AgentK returned no session")
    print(response.session.id)
    return response.session.id


async def _start_pipeline(client: Any, session_id: str, pipeline: str, args: dict[str, Any]) -> str:
    response = await client.start_execution(
        service_pb.StartExecutionRequest(
            session_id=session_id,
            input=Oneof(
                "pipeline",
                service_pb.PipelineExecutionInput(
                    pipeline=pipeline,
                    args=struct_pb.Struct.from_python(args),
                ),
            ),
        )
    )
    if response.execution is None or not response.execution.id:
        raise RuntimeError("AgentK returned no execution")
    print(f"execution={response.execution.id}")
    return response.execution.id


async def _watch(client: Any, session_id: str, execution_id: str) -> dict[str, Any] | None:
    seen: set[str] = set()
    retries = 0
    while True:
        try:
            async for frame in client.watch_events(
                service_pb.WatchEventsRequest(
                    session_id=session_id,
                    execution_id=execution_id,
                )
            ):
                if frame.frame is None:
                    continue
                if frame.frame.field == "committed_event":
                    event = frame.frame.value
                    if event.event_id in seen:
                        continue
                    seen.add(event.event_id)
                    _print_event(event)
                elif frame.frame.field == "stream_end":
                    return await _execution_output(client, session_id, execution_id)
            return await _execution_output(client, session_id, execution_id)
        except asyncio.CancelledError:
            raise
        except Exception:
            retries += 1
            await _recover_events(client, session_id, execution_id, seen)
            fetched = await client.get_execution(
                service_pb.GetExecutionRequest(
                    session_id=session_id,
                    execution_id=execution_id,
                )
            )
            if (
                fetched.execution is None
                or fetched.execution.status != model_pb.ExecutionStatus.RUNNING
            ):
                return _output_from_execution(fetched.execution)
            if retries >= 3:
                raise
            await asyncio.sleep(retries)


async def _recover_events(
    client: Any,
    session_id: str,
    execution_id: str,
    seen: set[str],
) -> None:
    """Replay missed committed events in chronological order after a stream break."""
    token = ""
    recovered: list[Any] = []
    while True:
        response = await client.list_events(
            service_pb.ListEventsRequest(
                session_id=session_id,
                page_size=100,
                page_token=token,
            )
        )
        for event in response.events:
            if event.execution_id == execution_id and event.event_id not in seen:
                recovered.append(event)
        token = response.next_page_token
        if not token:
            break
    for event in reversed(recovered):
        if event.event_id in seen:
            continue
        seen.add(event.event_id)
        _print_event(event)


def _print_event(event: Any) -> None:
    if event.payload is None:
        return
    if event.payload.field == "invocation_progress":
        progress = event.payload.value
        data = progress.data.to_python() if progress.data is not None else {}
        suffix = json.dumps(data, ensure_ascii=False) if data else ""
        print(f"[{progress.step_id}] {progress.message} {suffix}".rstrip())
    elif event.payload.field == "execution_finished":
        finished = event.payload.value
        print(f"finished status={finished.status} error={finished.error or '-'}")


async def _execution_output(
    client: Any, session_id: str, execution_id: str
) -> dict[str, Any] | None:
    response = await client.get_execution(
        service_pb.GetExecutionRequest(session_id=session_id, execution_id=execution_id)
    )
    return _output_from_execution(response.execution)


def _output_from_execution(execution: Any) -> dict[str, Any] | None:
    if execution is None or execution.kind is None or execution.kind.field != "pipeline":
        return None
    output = execution.kind.value.output
    return output.to_python() if output is not None else None


async def _download(uri: str, output: Path) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    async with httpx.AsyncClient(timeout=None) as client, client.stream("GET", uri) as response:
        response.raise_for_status()
        with output.open("wb") as handle:
            async for chunk in response.aiter_bytes(1 << 20):
                handle.write(chunk)
    print(output)


async def _source_uri(args: argparse.Namespace, settings: Settings) -> str:
    return args.video_uri if args.video_uri else await _upload(args.file, settings)


async def _run(args: argparse.Namespace) -> None:
    settings = Settings.load()
    if args.command == "upload":
        await _upload(args.file, settings)
        return
    if args.command == "download":
        await _download(args.uri, args.output)
        return
    if not settings.agentk_api_key:
        raise ValueError("AGENTK_API_KEY is required")

    async with create_agentk_client(settings.agentk_base_url, settings.agentk_api_key) as client:
        if args.command == "session":
            await _create_session(client, settings, args.title)
            return
        if args.command == "cancel":
            await client.cancel_execution(
                service_pb.CancelExecutionRequest(session_id=args.session_id)
            )
            print("cancel requested")
            return
        if args.command == "watch":
            output = await _watch(client, args.session_id, args.execution_id)
            if output is not None:
                print(json.dumps(output, ensure_ascii=False, indent=2))
            return

        session_id = args.session_id or await _create_session(
            client, settings, "Narrato AgentK Documentary"
        )
        if args.command == "run":
            video_uri = await _source_uri(args, settings)
            voice_id = args.voice_id or settings.qwen_voice_id
            if not voice_id:
                raise ValueError("--voice-id or NARRATO_QWEN_VOICE_ID is required")
            pipeline = "documentary_video"
            payload = {
                "videoUri": video_uri,
                "theme": args.theme,
                "instructions": args.instructions,
                "voiceId": voice_id,
                "aspect": args.aspect,
                "narrationVolume": args.narration_volume,
                "subtitles": not args.no_subtitles,
            }
        elif args.command == "script":
            pipeline = "documentary_script"
            payload = {
                "videoUri": await _source_uri(args, settings),
                "theme": args.theme,
                "instructions": args.instructions,
            }
        elif args.command == "render":
            voice_id = args.voice_id or settings.qwen_voice_id
            if not voice_id:
                raise ValueError("--voice-id or NARRATO_QWEN_VOICE_ID is required")
            pipeline = "documentary_render"
            payload = {
                "script": json.loads(args.script.read_text(encoding="utf-8")),
                "voiceId": voice_id,
                "aspect": args.aspect,
                "narrationVolume": args.narration_volume,
                "subtitles": not args.no_subtitles,
            }
        else:
            raise AssertionError(f"unhandled command: {args.command}")

        execution_id = await _start_pipeline(client, session_id, pipeline, payload)
        output = await _watch(client, session_id, execution_id)
        if output is None:
            return
        if args.command == "script":
            args.output.write_text(
                json.dumps(output, ensure_ascii=False, indent=2), encoding="utf-8"
            )
            print(args.output)
        else:
            print(json.dumps(output, ensure_ascii=False, indent=2))
            if args.output:
                artifacts = output.get("artifacts") or []
                if not artifacts:
                    raise RuntimeError("pipeline returned no final artifact")
                await _download(artifacts[0]["uri"], args.output)


def main() -> None:
    try:
        asyncio.run(_run(_parser().parse_args()))
    except KeyboardInterrupt:
        print("stopped watching; the remote execution was not cancelled", file=sys.stderr)
    except Exception as error:
        print(f"error: {error}", file=sys.stderr)
        raise SystemExit(1) from error


if __name__ == "__main__":
    main()
