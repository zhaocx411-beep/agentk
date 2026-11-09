import asyncio
import os
from pathlib import Path
from time import monotonic
from typing import Any

from agentk.api.v1 import model_pb, profile_pb, service_pb
from agentk.client import AsyncClient, Options
from agentk.client.grpc import create_client
from mcp.server import MCPServer
from mcp.server.apps import Apps, ResourceCsp
from mcp.server.mcpserver import Context
from mcp.server.transport_security import TransportSecuritySettings
from protobuf import Oneof
from protobuf.wkt import struct_pb
from starlette.responses import PlainTextResponse

PROFILE_PIPELINE_NAME = "storyboard_film_example"
PROFILE_FILE = Path(__file__).resolve().parent / "profile.json"
APP_RESOURCE_URI = "ui://storyboard-film/player.html"
APP_FILE = Path(__file__).resolve().parent / "ui/storyboard-film.html"
POLL_INTERVAL_SECONDS = 5
PIPELINE_TIMEOUT_SECONDS = 30 * 60

apps = Apps()


def make_agentk_client() -> AsyncClient:
    return create_client(Options())


def load_profile() -> profile_pb.Profile:
    try:
        return profile_pb.Profile.from_json(PROFILE_FILE.read_text(encoding="utf-8"))
    except (OSError, ValueError) as error:
        raise RuntimeError(f"cannot load profile {PROFILE_FILE}: {error}") from error


@apps.tool(resource_uri=APP_RESOURCE_URI)
async def generate_storyboard_film(
    topic: str,
    ctx: Context,
    shots: int = 1,
) -> dict[str, Any]:
    """Generate a storyboard film and return its video artifact when complete."""
    topic = topic.strip()
    if not topic:
        raise ValueError("topic is required")
    if isinstance(shots, bool) or not isinstance(shots, int) or not 1 <= shots <= 8:
        raise ValueError("shots must be an integer between 1 and 8")

    profile = load_profile()
    args = {
        "topic": topic,
        "shots": shots,
        "chatModel": os.getenv("STORYBOARD_CHAT_MODEL", "deepseek-v4-pro"),
        "imageModel": os.getenv("STORYBOARD_IMAGE_MODEL", "z-image-turbo"),
        "videoModel": os.getenv("STORYBOARD_VIDEO_MODEL", "kling-3.0"),
    }

    async with make_agentk_client() as client:
        progress = 1.0
        await ctx.report_progress(progress, message="Creating AgentK session")
        created = await client.create_session(
            service_pb.CreateSessionRequest(
                title=f"Storyboard Film: {topic}",
                profile=profile,
            )
        )
        if created.session is None or not created.session.id.startswith("ses_"):
            raise RuntimeError("agent-k response is missing a valid ses_ id")
        session_id = created.session.id

        progress += 1
        await ctx.report_progress(progress, message="Starting storyboard pipeline")
        started = await client.start_execution(
            service_pb.StartExecutionRequest(
                session_id=session_id,
                input=Oneof(
                    "pipeline",
                    service_pb.PipelineExecutionInput(
                        pipeline=PROFILE_PIPELINE_NAME,
                        args=struct_pb.Struct.from_python(args),
                    ),
                ),
            )
        )
        if started.execution is None or not started.execution.id.startswith("exe_"):
            raise RuntimeError("agent-k response is missing a valid exe_ id")
        execution_id = started.execution.id

        started_at = monotonic()
        while True:
            fetched = await client.get_execution(
                service_pb.GetExecutionRequest(
                    session_id=session_id,
                    execution_id=execution_id,
                )
            )
            execution = fetched.execution
            if execution is None:
                raise RuntimeError("agent-k response is missing execution")
            status = execution.status

            if status == model_pb.ExecutionStatus.COMPLETED:
                if execution.kind is None or execution.kind.field != "pipeline":
                    raise RuntimeError(
                        f"execution {execution_id} is not a pipeline"
                    )
                pipeline = execution.kind.value
                output = (
                    pipeline.output.to_python()
                    if pipeline.output is not None
                    else None
                )
                if not isinstance(output, dict):
                    raise RuntimeError(
                        f"completed pipeline {execution_id} has no output object"
                    )
                artifacts = output.get("artifacts")
                clip_count = output.get("clipCount")
                if (
                    not isinstance(artifacts, list)
                    or len(artifacts) != 1
                    or not isinstance(artifacts[0], dict)
                ):
                    raise RuntimeError(
                        f"completed pipeline {execution_id} has invalid artifacts"
                    )
                artifact = artifacts[0]
                artifact_type = artifact.get("type")
                artifact_uri = artifact.get("uri")
                if artifact_type != "video":
                    raise RuntimeError(
                        f"completed pipeline {execution_id} has invalid artifact type"
                    )
                if not isinstance(artifact_uri, str) or not artifact_uri:
                    raise RuntimeError(
                        f"completed pipeline {execution_id} has no artifact URI"
                    )
                normalized_artifact: dict[str, Any] = {
                    "type": artifact_type,
                    "uri": artifact_uri,
                }
                for field in ("description", "model"):
                    value = artifact.get(field)
                    if value is not None:
                        if not isinstance(value, str):
                            raise RuntimeError(
                                f"completed pipeline {execution_id} has invalid "
                                f"artifact {field}"
                            )
                        normalized_artifact[field] = value
                params = artifact.get("params")
                if params is not None:
                    if not isinstance(params, dict):
                        raise RuntimeError(
                            f"completed pipeline {execution_id} has invalid "
                            "artifact params"
                        )
                    normalized_artifact["params"] = params
                if (
                    isinstance(clip_count, bool)
                    or not isinstance(clip_count, (int, float))
                    or not float(clip_count).is_integer()
                    or clip_count < 1
                ):
                    raise RuntimeError(
                        f"completed pipeline {execution_id} has invalid clipCount"
                    )
                clip_count = int(clip_count)

                await ctx.report_progress(progress + 1, message="Film ready")
                return {
                    "sessionId": session_id,
                    "executionId": execution_id,
                    "status": "completed",
                    "artifacts": [normalized_artifact],
                    "clipCount": clip_count,
                }

            if status in (
                model_pb.ExecutionStatus.FAILED,
                model_pb.ExecutionStatus.CANCELLED,
            ):
                error = execution.error
                if status == model_pb.ExecutionStatus.CANCELLED and not error:
                    error = "pipeline cancelled"
                raise RuntimeError(
                    f"pipeline failed (session={session_id}, "
                    f"execution={execution_id}): "
                    f"{error if isinstance(error, str) and error else 'unknown error'}"
                )
            if status != model_pb.ExecutionStatus.RUNNING:
                raise RuntimeError(
                    f"pipeline {execution_id} returned unknown status {status!r}"
                )

            elapsed = monotonic() - started_at
            if elapsed >= PIPELINE_TIMEOUT_SECONDS:
                raise TimeoutError(
                    f"pipeline still running after {elapsed:.0f}s "
                    f"(session={session_id}, execution={execution_id})"
                )

            progress += POLL_INTERVAL_SECONDS
            await ctx.report_progress(
                progress,
                message=f"Generating film; waited {elapsed:.0f}s",
            )
            await asyncio.sleep(POLL_INTERVAL_SECONDS)


apps.add_html_resource(
    APP_RESOURCE_URI,
    APP_FILE.read_text(encoding="utf-8"),
    title="Storyboard Film Player",
    description="Displays the generated storyboard film.",
    csp=ResourceCsp(
        resource_domains=["https://bs3-hb1.staging.kuaishou.com"],
    ),
    prefers_border=True,
)

mcp = MCPServer(
    "storyboard-film",
    instructions=(
        "Generate a storyboard film from a topic. "
        "Calls may take several minutes; show progress to the user."
    ),
    extensions=[apps],
)


@mcp.custom_route("/healthz", methods=["GET"])
async def healthz(_request: Any) -> PlainTextResponse:
    return PlainTextResponse("ok")


if __name__ == "__main__":
    host = os.getenv("MCP_HOST", "127.0.0.1")
    port = int(os.getenv("MCP_PORT", "8000"))
    allowed_host = os.getenv("MCP_ALLOWED_HOST", "localhost")
    security = TransportSecuritySettings(
        allowed_hosts=[
            "127.0.0.1",
            "127.0.0.1:*",
            "localhost",
            "localhost:*",
            allowed_host,
            f"{allowed_host}:*",
        ],
    )
    mcp.run(
        transport="streamable-http",
        host=host,
        port=port,
        transport_security=security,
    )
