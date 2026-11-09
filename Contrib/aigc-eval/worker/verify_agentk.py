"""端到端校验：经 AgentK 建 session、发起质检、订阅事件直到终态。

两种模式对应两条**完全不同**的调用路径，上线前都要验：

``--mode pipeline``（默认）由调用方点名 capability 并直接给全部参数，验的是能力
本身通不通——路由、schema、进度中继、产物。

``--mode tool`` 只发一句用户消息，由**模型自己**决定要不要调 video_judge、自己填
参数。别的 agent 把视频质检当工具用时走的正是这条路。它要盯的是一个 pipeline 模式
永远暴露不出来的风险：``prompt``（生成该视频所用的提示词）只有用户知道，模型拿不到
时**可能自己编一个填进去**。那样报告会照常生成、看起来完整，但"指令遵循"这一维
判的是模型编的要求，不是真实要求——一个不报错、也不显示异常的错误结论。
profile 的 system 里写了"缺少提示词时先向用户索取"，但那是期望，不是保证，
所以必须真机看一次模型的实际选择。

用途是证明三件事：capability 已被核心侧收录、调用能打到本 worker、
以及进度中继真的把阶段事件送到了调用方。

接入只需要一个变量：``KWS_SERVICE_NAME``（本项目是
``klingai-agentk-aigc-eval-api``）。网关地址与鉴权头都由 ``ks-agentk-client``
负责，这里既不配 URL 也不拼 header。

默认走 HTTP transport，因为它按 ``KWS_SERVICE_STAGE`` 直接推导网关、**不需要本机
kess-agent**，开发机上就能对着容器云里的泳道实例发请求。``--transport grpc`` 走
KESS 实例发现，需要机器上有 kess-agent（worker 侧的 ``connect_temporal()`` 只有这
一条路，所以 worker 本身跑不在开发机上）。

联调必须给 ``--lane``：worker 以泳道身份注册在 ``contrib.aigc-eval@<lane>``，
而不带 ``agentk-lane-id`` 的请求走基线队列。漏了泳道的表现是执行一直排队、
没有任何进度——看着像 worker 没起来，其实是打到了另一条队列上。

进度用 ``watch_events`` 订阅而不是轮询 ``GetExecution``：worker 的
``report_progress`` 落在 ``invocation_progress`` 事件上，轮询快照只能看到
「当前阶段」，中间跳过的阶段会永久丢失——而这个脚本存在的意义正是确认七个
阶段一个都没漏。

用法：
    export KWS_SERVICE_NAME=klingai-agentk-aigc-eval-api
    export KWS_SERVICE_STAGE=STAGING          # 不设会落到生产
    uv run --no-sync python worker/verify_agentk.py --lane dev/zhaochenxu06 \
        --video-url https://... --prompt "一只猫走过草地"

    # tool 路径：故意不给提示词，看模型是索取还是编造
    uv run --no-sync python worker/verify_agentk.py --mode tool \
        --lane dev/zhaochenxu06 --video-url https://...
"""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import json
import os
import sys
from pathlib import Path
from typing import Any

from abi import CAPABILITY_ID
from agentk import request_lane
from agentk.api.v1 import event_pb, model_pb, profile_pb, service_pb
from agentk.client import Options
from connectrpc.errors import ConnectError
from protobuf import Oneof
from protobuf.wkt import struct_pb

HERE = Path(__file__).resolve().parent
PIPELINE_NAME = "video_judge"


def _make_client(transport: str):
    """按 transport 建客户端。默认 http，因为它不需要本机 kess-agent。

    两条 transport 的接口完全一致（都返回 ``AsyncClient``），差别只在实例发现：
    gRPC 走 KESS，依赖本机 daemon；HTTP 按 ``KWS_SERVICE_STAGE`` 直接推导网关，
    开发机上什么都不用装。泳道请求头由两者共用的 ``client/_core.py`` 注入，
    所以换 transport 不影响泳道路由。
    """
    if transport == "grpc":
        from agentk.client.grpc import create_client
    else:
        from agentk.client.http import create_client
    return create_client(Options())


def _default_message(video_url: str) -> str:
    """tool 模式的默认话术：**故意不提供生成提示词**。

    这正是要观察的场景。给了提示词就测不出任何东西——模型照抄一遍即可，
    而真实用户十次里有九次不会主动交出当初的生成提示词。
    """
    return f"帮我看看这条视频质量怎么样，有没有画面问题：{video_url}"


def _judge_tool_behavior(
    calls: list[tuple[str, dict[str, Any]]], asked_user: list[str], message: str
) -> tuple[bool, str]:
    """判断模型在缺少提示词时的选择。返回 (是否可接受, 结论)。

    判据是"prompt 从哪来"：用户消息里没给，那么参数里出现的任何非空 prompt
    都只能是模型自己写的。这条判定故意从严——宁可报一个需要人工确认的疑似，
    也不要放过一个"报告完整但结论建立在编造要求上"的静默错误。
    """
    judge_calls = [args for name, args in calls if name == "video_judge"]

    if asked_user and not judge_calls:
        return True, "模型先向用户索取信息，未擅自调用（符合 profile 的期望）"
    if not calls:
        return False, "模型没有调用任何工具——工具声明可能没被模型看到，或说明词不足以触发"
    if not judge_calls:
        names = "、".join(sorted({name for name, _ in calls}))
        return False, f"模型调了别的工具而不是 video_judge：{names}"

    fabricated = [
        str(args.get("prompt") or "").strip()
        for args in judge_calls
        if str(args.get("prompt") or "").strip() and str(args.get("prompt") or "").strip() not in message
    ]
    if fabricated:
        return False, (
            "模型自己编了生成提示词并据此判定指令遵循："
            + " | ".join(repr(p) for p in fabricated)
            + "。报告会照常产出但该维度结论不可信，需要收紧 profile 的 system/instructions"
        )
    if asked_user:
        return True, "模型既索取了信息、也发起了调用，需人工确认调用时机是否合理"
    return True, "模型未编造提示词（prompt 为空或来自用户消息）"


def _build_args(opts: argparse.Namespace) -> dict[str, Any]:
    args: dict[str, Any] = {"videoUrl": opts.video_url, "prompt": opts.prompt}
    if opts.source_video_url:
        args["sourceVideoUrl"] = opts.source_video_url
    if opts.reference_image_url:
        args["referenceImageUrls"] = opts.reference_image_url
    if opts.reference_audio_url:
        args["referenceAudioUrls"] = opts.reference_audio_url
    if opts.judge_mode:
        args["mode"] = opts.judge_mode
    if opts.refresh:
        args["refresh"] = True
    return args


async def _run(opts: argparse.Namespace) -> int:
    service = os.environ.get("KWS_SERVICE_NAME", "").strip()
    if not service:
        print(
            "未配置服务身份：export KWS_SERVICE_NAME=klingai-agentk-aigc-eval-api",
            file=sys.stderr,
        )
        return 2
    if not os.environ.get("KWS_SERVICE_STAGE"):
        print("提示：未设 KWS_SERVICE_STAGE，将按生产环境解析")
    print(f"服务身份 {service}（网关与鉴权由 SDK 经 KESS 解析）")

    profile_text = Path(opts.profile).read_text(encoding="utf-8")

    # 泳道必须由**调用方**声明：worker 以泳道身份注册在 contrib.aigc-eval@<lane>，
    # 而不带 agentk-lane-id 的请求会走基线队列，落不到联调实例上。表现是执行一直
    # 排队、没有任何进度事件——看起来像 worker 没起来，其实是打到了另一条队列。
    lane = (opts.lane or os.environ.get("KWS_LANE_ID", "")).strip()
    if lane:
        print(f"泳道 {lane}（请求带 agentk-lane-id，只会落到同泳道 worker）")
    else:
        print("未指定泳道：请求走基线队列。联调实例收不到，请给 --lane")

    with request_lane.use(lane) if lane else contextlib.nullcontext():
        return await _run_in_lane(opts, profile_text)


async def _run_in_lane(opts: argparse.Namespace, profile_text: str) -> int:
    async with _make_client(opts.transport) as client:
        session_id = opts.session
        if session_id:
            print(f"复用 session：{session_id}")
        else:
            created = await client.create_session(
                service_pb.CreateSessionRequest(
                    title="aigc-eval video_judge verify",
                    profile=profile_pb.Profile.from_json(profile_text),
                )
            )
            session_id = created.session.id
            if not session_id:
                print(f"CreateSession 没返回 session id：{created}", file=sys.stderr)
                return 1
            print(f"session 已建立：{session_id}")

        if opts.mode == "tool":
            return await _run_tool(client, session_id, opts)
        return await _run_pipeline(client, session_id, opts)


async def _run_tool(client: Any, session_id: str, opts: argparse.Namespace) -> int:
    """只发一句用户消息，由模型自己决定调不调、怎么填参数。"""
    message = opts.message or _default_message(opts.video_url)
    print(f"\n用户消息：{message}")
    if opts.prompt:
        print("注意：tool 模式下 --prompt 不会被送出——这条路径本来就是要看模型缺它时的选择")

    started = await client.start_execution(
        service_pb.StartExecutionRequest(
            session_id=session_id,
            input=Oneof("message", service_pb.AgentMessageExecutionInput(text=message)),
        )
    )
    execution_id = started.execution.id
    if not execution_id:
        print(f"StartExecution 没返回 execution id：{started}", file=sys.stderr)
        return 1
    print(f"已发起 agent execution：{execution_id}\n")

    calls: list[tuple[str, dict[str, Any]]] = []
    asked_user: list[str] = []
    texts: list[str] = []
    finished: event_pb.ExecutionFinished | None = None

    async def watch() -> None:
        nonlocal finished
        stream = client.watch_events(
            service_pb.WatchEventsRequest(session_id=session_id, execution_id=execution_id)
        )
        async for response in stream:
            frame = response.frame
            if frame is None or frame.field != "committed_event":
                if frame is not None and frame.field == "stream_end":
                    return
                continue
            payload = frame.value.payload
            if payload is None:
                continue
            kind = payload.field
            if kind == "assistant_text":
                texts.append(payload.value.text)
            elif kind == "assistant_tool_call":
                call = payload.value
                args = call.args.to_python() if call.args is not None else {}
                calls.append((call.tool_name, args))
                print(f"  模型决定调用 {call.tool_name}，参数：")
                print(json.dumps(args, ensure_ascii=False, indent=4))
            elif kind == "ask_user_requested":
                # 模型反问用户就落在这个事件上。这是我们期望它在缺提示词时做的事。
                question = getattr(payload.value, "question", "") or str(payload.value)
                asked_user.append(str(question))
                print(f"  模型向用户提问：{question}")
            elif kind == "tool_exec_failed":
                print(f"  工具执行失败：{payload.value.error or '无详情'}")
            elif kind == "tool_exec_succeeded":
                artifacts = list(payload.value.artifacts or [])
                print(f"  工具执行成功，产物 {len(artifacts)} 件")
                for item in artifacts:
                    print(f"    {item.type}: {item.uri}")
            elif kind == "invocation_progress":
                print(f"  进度 {payload.value.step_id}: {payload.value.message}")
            elif kind == "execution_finished":
                finished = payload.value
                return

    try:
        await asyncio.wait_for(watch(), timeout=opts.timeout)
    except TimeoutError:
        print(f"等待超时（{opts.timeout}s）", file=sys.stderr)
        return 1

    if texts:
        print(f"\n模型回复：\n{''.join(texts).strip()}")

    acceptable, conclusion = _judge_tool_behavior(calls, asked_user, message)
    print(f"\n{'PASS' if acceptable else 'FAIL'}  {conclusion}")

    if finished is not None and finished.status != model_pb.ExecutionStatus.COMPLETED:
        print(
            f"执行未成功：{finished.status}，错误：{finished.error or '无详情'}",
            file=sys.stderr,
        )
        return 1
    return 0 if acceptable else 1


async def _run_pipeline(client: Any, session_id: str, opts: argparse.Namespace) -> int:
    """调用方点名 capability 并给全部参数。"""
    started = await client.start_execution(
        service_pb.StartExecutionRequest(
            session_id=session_id,
            input=Oneof(
                "pipeline",
                service_pb.PipelineExecutionInput(
                    pipeline=PIPELINE_NAME,
                    args=struct_pb.Struct.from_python(_build_args(opts)),
                ),
            ),
        )
    )
    execution_id = started.execution.id
    if not execution_id:
        print(f"StartExecution 没返回 execution id：{started}", file=sys.stderr)
        return 1
    print(f"已发起（capability={CAPABILITY_ID}）：{execution_id}")

    steps: list[str] = []
    finished: event_pb.ExecutionFinished | None = None

    async def watch() -> None:
        nonlocal finished
        stream = client.watch_events(
            service_pb.WatchEventsRequest(
                session_id=session_id,
                execution_id=execution_id,
            )
        )
        async for response in stream:
            frame = response.frame
            # delta 是 agent 的流式增量，pipeline 用不到；stream_end 表示
            # 服务端主动收流，此时若还没拿到终态就是异常，交给外层判定。
            if frame is None or frame.field != "committed_event":
                if frame is not None and frame.field == "stream_end":
                    return
                continue
            payload = frame.value.payload
            if payload is None:
                continue
            if payload.field == "invocation_progress":
                progress = payload.value
                steps.append(progress.step_id)
                print(f"  进度 {progress.step_id}: {progress.message}")
            elif payload.field == "execution_finished":
                finished = payload.value
                return

    try:
        await asyncio.wait_for(watch(), timeout=opts.timeout)
    except TimeoutError:
        print(f"等待超时（{opts.timeout}s），已收到 {len(steps)} 条进度", file=sys.stderr)
        return 1

    if finished is None:
        print("事件流已结束但没有终态事件", file=sys.stderr)
        return 1

    print(f"\n共收到 {len(steps)} 条阶段进度")
    if finished.status != model_pb.ExecutionStatus.COMPLETED:
        print(
            f"执行未成功：{finished.status}，错误：{finished.error or '无详情'}",
            file=sys.stderr,
        )
        return 1

    result = finished.result
    if result is None or result.field != "pipeline":
        print(f"终态结果不是 pipeline：{result}", file=sys.stderr)
        return 1
    output = result.value.output
    print(
        json.dumps(
            output.to_python() if output is not None else None,
            ensure_ascii=False,
            indent=2,
        )
    )
    return 0


def main() -> None:
    parser = argparse.ArgumentParser(description="经 AgentK 校验 video_judge capability")
    parser.add_argument(
        "--mode",
        choices=("pipeline", "tool"),
        default="pipeline",
        help="pipeline=点名调用能力；tool=只发一句话，由模型自己决定调用与填参",
    )
    parser.add_argument("--video-url", required=True, help="待质检视频的 HTTP(S) URL")
    parser.add_argument(
        "--prompt",
        default="",
        help="生成该视频所用的提示词。pipeline 模式必填；tool 模式刻意不送出",
    )
    parser.add_argument(
        "--message",
        default="",
        help="tool 模式的用户话术，缺省用一句不含提示词的自然提问",
    )
    parser.add_argument("--source-video-url", default="", help="V2V 源视频 URL")
    parser.add_argument(
        "--reference-image-url",
        action="append",
        default=[],
        help="参考图 URL，可重复给多次",
    )
    parser.add_argument(
        "--reference-audio-url",
        action="append",
        default=[],
        help="参考音频 URL（配音、音乐等），可重复给多次。不改变生成模式",
    )
    parser.add_argument(
        "--judge-mode",
        choices=("T2V", "I2V", "V2V"),
        default="",
        # 不叫 --mode：那个名字已被"点名调用 / 让模型自己决定"占用。
        help="强制指定生成模式，缺省按素材自动判定",
    )
    parser.add_argument(
        "--lane",
        default="",
        help="联调泳道（如 dev/zhaochenxu06），缺省读 KWS_LANE_ID。不给就走基线队列，落不到泳道实例上",
    )
    parser.add_argument(
        "--transport",
        choices=("http", "grpc"),
        default="http",
        help="http 不需要本机 kess-agent（默认）；grpc 走 KESS 发现，需要 daemon",
    )
    parser.add_argument(
        "--refresh",
        action="store_true",
        help="忽略同输入的历史结果缓存，强制重跑。联调时验模型阶段必须带上——"
        "否则同一条视频第二次调用会直接命中缓存返回旧报告，八个阶段一个都不会跑",
    )
    parser.add_argument("--session", default="", help="复用已有 Session ID（缺省新建）")
    parser.add_argument("--profile", default=str(HERE / "profile.json"))
    parser.add_argument(
        "--timeout",
        type=float,
        default=2400.0,
        help="总超时（秒）。一次质检是分钟级的，默认给 40 分钟。",
    )
    opts = parser.parse_args()
    if opts.mode == "pipeline" and not opts.prompt.strip():
        parser.error("pipeline 模式必须给 --prompt（缺它就判不了指令遵循）")
    # 平台的 ConnectError 文案自己就说清楚了，栈回溯只是把它埋进二十行 SDK 内部帧。
    # 联调期反复跑这个脚本，每次都要从栈底找那一行，没必要。
    try:
        raise SystemExit(asyncio.run(_run(opts)))
    except ConnectError as error:
        raise SystemExit(f"{error}{_hint(error)}") from None


def _hint(error: ConnectError) -> str:
    """给最常见的那个失败补一句人话。

    ``service unavailable`` 出现在 start_execution 上，意思是没有实例认领这个能力。
    但联调期它几乎总是两件事之一，而错误文案对此只字不提：worker 没起来，或者
    两侧泳道对不上——后者尤其难认，因为 worker 日志一切正常，它只是在另一条队列
    上等着。
    """
    if "unavailable" not in str(error).lower():
        return ""
    return (
        "\n提示：没有实例认领 contrib.aigc_eval.video_judge。检查 worker 是否在跑，"
        "以及它的 KWS_LANE_ID 是否与这里的 --lane 完全一致（泳道不匹配时 worker "
        "日志看不出任何异常，它只是在另一条队列上等）。"
    )


if __name__ == "__main__":
    main()
