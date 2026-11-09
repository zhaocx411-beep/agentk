"""studio 的质检执行路径：经 AgentK 调用 video_judge capability。

**为什么不再在 studio 进程里跑。** 原先 studio 直接调 ``analyze_case``，模型请求由
进程内的 HTTP 客户端发出。那条路和 worker 那条是两套独立实现：同一份判据，一边经
Temporal 编排、调用进历史、可取消、崩溃能续跑，另一边什么都没有。两套实现只要有一处
漂移，同一条视频在 studio 和在 capability 上就会给出不同结论，而且没有任何地方会报错。
现在 studio 只是 capability 的一个调用方，判据在全公司只有一份。

代价是 studio 不再能跑那些没进对外契约的调试开关（``use_vlm``、``allow_fallback``）。
它们本来就只对本地调试有意义，不该出现在别人也要调的能力签名里。

**素材怎么过去。** worker 与调用方不共享文件系统，所以本地上传的视频与参考素材要先
进对象存储换成 URL。``BlobStore`` 按内容寻址，同一条视频重复质检不会重复上传。

**进度怎么回来。** worker 把阶段事件压成 ``report_progress(step_id, message, data)``，
这里再还原成 studio 前端认识的那个事件形状。还原是有损的那部分（比如 ``type``）
按约定补回，前端因此一行都不用改。
"""

from __future__ import annotations

import asyncio
import contextlib
import json
import os
import re
import urllib.request
from collections.abc import Callable
from pathlib import Path
from typing import Any

from aigc_eval.config import load_agentk_config
from aigc_eval.judge.cases import CaseFiles, load_prompt, reference_kind, source_video_of
from aigc_eval.paths import REPO_ROOT
from aigc_eval.providers.blob_store import shared_blob_store

PIPELINE_NAME = "video_judge"
# 与 worker/profile.json 同一份：studio 与联调脚本用同一个会话画像，
# 免得两边对模型与工具声明的理解不一致。
PROFILE_PATH = REPO_ROOT / "worker" / "profile.json"
# 一次质检是分钟级的，且模型阶段本身还带重试。
DEFAULT_TIMEOUT = 2400.0
REPORT_FETCH_TIMEOUT = 120.0


class CapabilityError(RuntimeError):
    """调用 capability 未能拿到结论。"""


# ---------------------------------------------------------------------------
# 入参：本地素材 → 对象存储 URL
# ---------------------------------------------------------------------------


def upload_inputs(case_files: CaseFiles) -> dict[str, Any]:
    """把 case 的本地素材传上去，拼成 ``VideoJudgeInput`` 的素材字段。

    源视频要单独走 ``sourceVideoUrl``：给了它 worker 就按 V2V 判定，混在参考素材里
    传过去会被当成普通参考视频，V2V 的三个原生子判据便拿不到基线。
    """
    store = shared_blob_store()
    source_video = source_video_of(case_files)

    images: list[str] = []
    audios: list[str] = []
    for reference in case_files.reference_assets or []:
        path = Path(str(reference.get("path") or ""))
        if not path.is_file():
            continue
        if source_video is not None and path == Path(source_video):
            continue
        kind = reference_kind(reference)
        if kind == "image":
            images.append(store.put_file(path))
        elif kind == "audio":
            audios.append(store.put_file(path))

    payload: dict[str, Any] = {
        "videoUrl": store.put_file(Path(case_files.video_path)),
        "referenceImageUrls": images,
        "referenceAudioUrls": audios,
    }
    if source_video is not None:
        payload["sourceVideoUrl"] = store.put_file(Path(source_video))
    return payload


def build_args(
    case_files: CaseFiles,
    *,
    provider: str,
    mode: str | None,
    refresh: bool,
) -> dict[str, Any]:
    prompt = load_prompt(case_files)
    payload = upload_inputs(case_files)
    payload["prompt"] = prompt["zh"] or prompt["en"]
    payload["provider"] = provider
    if mode:
        payload["mode"] = mode
    if refresh:
        payload["refresh"] = True
    return payload


# ---------------------------------------------------------------------------
# 进度：report_progress → studio 阶段事件
# ---------------------------------------------------------------------------

# workflow 每次发起模型调用都会补一条 ``<阶段>.model.<第几次>`` 的进度。那是编排内部
# 的事，不是一个阶段：它只有 running、永远不会有对应的 done，而前端按 id 建行、按
# status 改状态，收下它们的结果是界面上多出几行永久停在「运行中」的假阶段。
#
# 也不能把它并进父阶段那一行——step_id 里的名字是 workflow 的阶段键（scan、
# evidence），跟界面上的阶段 id（unified_scan、grounding）本来就对不上。
#
# 丢掉它们之后，界面回到与进程内执行时一致的样子：阶段行在等模型期间显示「运行中」。
_MODEL_ATTEMPT_RE = re.compile(r"\.model\.\d+$")


def event_from_progress(step_id: str, message: str, data: Any) -> dict[str, Any] | None:
    """还原 worker 压扁前的阶段事件。

    ``stage_activities._stage_progress`` 把 ``{id, status}`` 拼进了 ``step_id``、
    其余字段塞进 ``data``。这里按同一约定拆回来。status 以 ``data`` 里的为准而不是从
    step_id 尾巴上猜——阶段 id 本身可能含点号，靠切分会把 id 截断。
    """
    if _MODEL_ATTEMPT_RE.search(step_id or ""):
        return None
    detail = data if isinstance(data, dict) else {}
    status = str(detail.get("status") or "").strip()
    step = str(step_id or "").strip()
    if not step:
        return None
    stage_id = step[: -(len(status) + 1)] if status and step.endswith(f".{status}") else step

    event: dict[str, Any] = {
        "type": "stage",
        "id": stage_id,
        "title": message or stage_id,
        "status": status or "running",
        "kind": str(detail.get("kind") or "tool"),
    }
    for key in ("input", "output", "detail"):
        if detail.get(key):
            event[key] = detail[key]
    return event


# ---------------------------------------------------------------------------
# 结果：取回完整报告并改成 studio 自己的素材地址
# ---------------------------------------------------------------------------


def fetch_report(url: str) -> dict[str, Any]:
    with urllib.request.urlopen(url, timeout=REPORT_FETCH_TIMEOUT) as response:
        return json.loads(response.read().decode("utf-8"))


def localize(report: dict[str, Any], case_files: CaseFiles, output: dict[str, Any]) -> dict[str, Any]:
    """把报告里的素材地址换成 studio 自己能提供的。

    报告是 worker 生成的，``media`` 里的地址形如 ``./api/upload_media?slug=<worker 的
    slug>``，指向 worker 的 runtime 目录。slug 由内容派生，两边**通常**一致，但参考
    素材的标注只有 studio 有，归一化结果因此可能不同——一旦不同，页面上的视频就是死链。
    与其赌它相等，不如按 studio 自己的 slug 重写。

    HTML 报告同理：本地那份在 worker 上，studio 只能给对象存储上的那份。
    """
    media = report.get("media")
    if isinstance(media, dict):
        worker_slug = str(((report.get("case") or {}).get("slug")) or "")
        if not worker_slug:
            worker_slug = _slug_from_urls(media)
        if worker_slug and worker_slug != case_files.slug:
            report["media"] = json.loads(
                json.dumps(media, ensure_ascii=False).replace(worker_slug, case_files.slug)
            )

    if output.get("reportUrl"):
        report["report_view_url"] = output["reportUrl"]
    return report


def _slug_from_urls(media: dict[str, Any]) -> str:
    """从 ``./api/upload_media?slug=...`` 里反解 worker 的 slug。"""
    import re
    import urllib.parse

    match = re.search(r"[?&]slug=([^&\"]+)", json.dumps(media, ensure_ascii=False))
    return urllib.parse.unquote(match.group(1)) if match else ""


# ---------------------------------------------------------------------------
# 调用
# ---------------------------------------------------------------------------


def warm_up_transport() -> None:
    """在主线程里先把 gRPC 通道模块导进来，必须在起 HTTP 服务之前调用一次。

    KESS 依赖的 ``infra.util`` 在**模块导入时**就构造 ``_SignalHandler`` 单例，而它
    的构造函数会 ``signal.signal(SIGTERM/SIGINT, ...)``。Python 只允许主线程装信号，
    别的线程调用直接 ``ValueError: signal only works in main thread``。

    ``_make_client`` 里那句 ``from agentk.client.grpc import ...`` 是延迟导入，而每次
    质检都跑在 ``_job_start`` 开的后台线程里——于是生产上第一次质检必然在子线程触发
    这次导入，整条链路还没发出任何请求就崩了。本地不复现：HTTP 通道根本不碰 KESS。

    只做导入，不建客户端：注册信号的是导入这个动作本身，模块缓存过后子线程里再导入
    就是空操作。导入失败不抛：真连不上 KESS 该由质检本身报错，不该让服务起不来。

    必须直接导 ``kess``，光导 ``agentk.client.grpc`` 不够——后者对 kess 是**函数级**
    导入，藏在 ``agentk/client/grpc/_kess.py`` 的 ``new_controller()`` 体内，只有
    真去建客户端时才执行。本文件第一版就栽在这里：预热跑了，子线程照样崩。
    """
    if load_agentk_config().transport != "grpc":
        return
    try:
        # 导子模块会连父包一起导，两句延迟导入（kess 与 kess.division）都覆盖到。
        import kess.division  # noqa: F401
    except Exception as error:  # noqa: BLE001 - 预热失败不该挡住服务启动
        print(f"[aigc-eval] KESS 预热失败，质检时可能报信号错误：{error}")


def _make_client():
    """按环境选传输通道，规则与质检调用侧共用 ``config.default_agentk_transport``。

    这里曾经写死 HTTP，理由是"studio 作为调用方没有理由依赖 KESS daemon"。那个理由
    在生产上不成立：``agentk-api.internal`` 那道 HTTP 门不对我们开放，写死 HTTP 的
    结果是 studio 在 ``start_execution`` 上拿 ``unauthorized``，而 worker 侧一切正常
    ——一半功能可用、一半报没授权，且报错与"服务名没登记"完全一样。

    两条 transport 的接口一致（都返回 ``AsyncClient``），泳道头由共用的 ``_core``
    注入，所以换通道不影响泳道路由。
    """
    from agentk.client import Options

    config = load_agentk_config()
    if config.transport == "grpc":
        # endpoint 留空才会触发 KESS 发现；给了地址就退回直连覆盖，等于没换。
        from agentk.client.grpc import create_client

        return create_client(Options())

    from agentk.client.http import create_client

    return create_client(Options(endpoint=config.base_url))


async def _execute(
    args: dict[str, Any],
    on_event: Callable[[dict[str, Any]], None],
    timeout: float,
) -> dict[str, Any]:
    from agentk import request_lane
    from agentk.api.v1 import model_pb, profile_pb, service_pb
    from protobuf import Oneof
    from protobuf.wkt import struct_pb

    # 联调时 worker 以泳道身份注册，不带 lane 的请求会走基线队列落不到它上面，
    # 表现是执行一直排队、没有任何进度。
    lane = os.environ.get("KWS_LANE_ID", "").strip()

    with request_lane.use(lane) if lane else contextlib.nullcontext():
        async with _make_client() as client:
            created = await client.create_session(
                service_pb.CreateSessionRequest(
                    title="aigc-eval studio",
                    profile=profile_pb.Profile.from_json(
                        PROFILE_PATH.read_text(encoding="utf-8")
                    ),
                )
            )
            session_id = created.session.id
            if not session_id:
                raise CapabilityError("CreateSession 未返回 session id")

            started = await client.start_execution(
                service_pb.StartExecutionRequest(
                    session_id=session_id,
                    input=Oneof(
                        "pipeline",
                        service_pb.PipelineExecutionInput(
                            pipeline=PIPELINE_NAME,
                            args=struct_pb.Struct.from_python(args),
                        ),
                    ),
                )
            )
            execution_id = started.execution.id
            if not execution_id:
                raise CapabilityError("StartExecution 未返回 execution id")

            finished = await asyncio.wait_for(
                _watch(client, session_id, execution_id, on_event), timeout=timeout
            )

    if finished is None:
        raise CapabilityError("事件流已结束但没有收到终态事件")
    if finished.status != model_pb.ExecutionStatus.COMPLETED:
        raise CapabilityError(f"执行未成功：{finished.error or finished.status}")

    result = finished.result
    if result is None or result.field != "pipeline":
        raise CapabilityError(f"终态结果不是 pipeline 输出：{result}")
    output = result.value.output
    return output.to_python() if output is not None else {}


async def _watch(
    client: Any,
    session_id: str,
    execution_id: str,
    on_event: Callable[[dict[str, Any]], None],
) -> Any:
    """订阅事件流直到终态。

    用 ``watch_events`` 而不是轮询执行快照：阶段进度是瞬时事件，轮询只能看到"当前
    阶段"，两次轮询之间跑完的阶段会永久丢失——而 studio 的界面画的正是逐阶段进度。
    """
    from agentk.api.v1 import service_pb

    stream = client.watch_events(
        service_pb.WatchEventsRequest(session_id=session_id, execution_id=execution_id)
    )
    async for response in stream:
        frame = response.frame
        if frame is None or frame.field != "committed_event":
            if frame is not None and frame.field == "stream_end":
                return None
            continue
        payload = frame.value.payload
        if payload is None:
            continue
        if payload.field == "invocation_progress":
            progress = payload.value
            data = progress.data.to_python() if progress.data is not None else None
            event = event_from_progress(progress.step_id, progress.message, data)
            if event is not None:
                on_event(event)
        elif payload.field == "execution_finished":
            return payload.value
    return None


def run_capability_job(
    *,
    case_files: CaseFiles,
    provider: str,
    mode: str | None = None,
    refresh: bool = False,
    on_event: Callable[[dict[str, Any]], None] | None = None,
    timeout: float = DEFAULT_TIMEOUT,
) -> dict[str, Any]:
    """跑完一次质检并返回完整报告（形状与旧的 ``analyze_case`` 一致）。

    同步接口：studio 的作业本来就在自己的线程里跑，调用方不必知道底下是 asyncio。
    """
    emit = on_event or (lambda _event: None)
    args = build_args(case_files, provider=provider, mode=mode, refresh=refresh)
    output = asyncio.run(_execute(args, emit, timeout))

    report_json_url = output.get("reportJsonUrl")
    if not report_json_url:
        # 结论本身还在 output 里，但 studio 的整个界面（证据回放、子判据、审计轨迹）
        # 都建立在完整报告上，拿不到它就没什么可渲染的，宁可明确失败。
        raise CapabilityError(
            f"capability 未返回 reportJsonUrl：{output.get('reportWarning') or '无详情'}"
        )
    return localize(fetch_report(str(report_json_url)), case_files, output)
