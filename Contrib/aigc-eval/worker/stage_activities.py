"""按阶段拆开的 Activity：八个阶段各一个，状态经对象存储传递。

为什么拆：Temporal 只能在 **activity 边界**上续跑。整条质检塞在一个 activity 里时，
崩在第 90 分钟就得从第 0 分钟重来——模型级检查点能省下模型钱，但抽帧、标框、报告
组装要全部重做，而且重来的窗口里再崩一次就又回到起点。拆开之后，已完成阶段的结果
留在 workflow 历史里，重启后从下一个阶段接着跑。

三条贯穿全文的约定：

* **状态**（``worker/state.py``）：每个 activity 读上一阶段的键、写自己的键，从不覆盖
  别人的。所以重试是幂等的：重跑读到的输入与第一次完全一致。
* **产物**（``aigc_eval.judge.artifact_store``）：帧图/证据图经对象存储传递，阶段函数
  内部自己在开工前还原。activity 之间因此不需要共享文件系统。
* **进度**：每个 activity 都把阶段事件 signal 回 workflow，由 workflow 转成
  ``report_progress``。调用方看到的进度流与单 activity 时代一致。

失败分类沿用原来的口径：应用层异常一律标成 non_retryable，只有"进程/容器丢失"才让
Temporal 重派。区别是现在重派只影响**当前阶段**，前面的阶段不会重跑。
"""

from __future__ import annotations

import asyncio
import logging
from collections.abc import Callable
from pathlib import Path
from typing import Any, Self

from abi import (
    STAGE_ACTIVITIES,
    ModelStep,
    ModelStepInput,
    StageInput,
    StageResult,
    VideoJudgeInput,
    VideoJudgeOutput,
)
from state import (
    UsageCarrier,
    deserialize_case_files,
    deserialize_media_bundle,
    merge_checkpoint_stats,
    serialize_case_files,
    serialize_media_bundle,
    shared_state_store,
)
from temporalio import activity
from temporalio.exceptions import ApplicationError

logger = logging.getLogger(__name__)

_PUMP_INTERVAL_SECONDS = 1.0

# 阶段之间的读写顺序。写在一处，activity 与 workflow 都从这里取，
# 免得"读哪个键"在两边各写一遍而漂移。
STAGE_ORDER = (
    "prepare",
    "compile",
    "observe",
    "scan",
    "recheck",
    "aggregate",
    "evidence",
    "report",
)


class _ProgressRelay:
    """把阶段事件成批 signal 回 workflow，并顺带心跳。

    阶段函数是同步的，跑在 ``asyncio.to_thread`` 的工作线程里，它的回调也在那个线程被
    调用；signal 是协程。两者之间用 ``loop.call_soon_threadsafe`` 过桥。

    成批而非逐条：每次 ``report_progress`` 在 workflow 侧都是一趟完整的 activity 往返，
    逐条转发会把历史撑得很长。
    """

    def __init__(self, client: Any, signal_name: str):
        self._client = client
        self._signal = signal_name
        self._queue: asyncio.Queue[dict[str, Any]] = asyncio.Queue()
        self._loop = asyncio.get_running_loop()
        info = activity.info()
        self._handle = client.get_workflow_handle(
            info.workflow_id, run_id=info.workflow_run_id
        )
        self._task: asyncio.Task[None] | None = None

    def on_event(self, event: dict[str, Any]) -> None:
        payload = _stage_progress(event)
        if payload is None:
            return
        try:
            self._loop.call_soon_threadsafe(self._queue.put_nowait, payload)
        except RuntimeError:  # 事件循环已关闭，说明 activity 正在收尾
            pass

    async def _flush(self) -> None:
        batch: list[dict[str, Any]] = []
        while not self._queue.empty():
            batch.append(self._queue.get_nowait())
        if not batch:
            return
        try:
            await self._handle.signal(self._signal, batch)
        except Exception:  # noqa: BLE001 - 进度是旁路，丢了也要让质检跑完
            logger.warning("进度回传失败，已跳过 %d 条", len(batch), exc_info=True)

    async def _pump(self) -> None:
        while True:
            await asyncio.sleep(_PUMP_INTERVAL_SECONDS)
            # 心跳与进度同频：单个阶段可能几分钟不产出事件，
            # 没有心跳会被 Temporal 判成失联。
            activity.heartbeat()
            await self._flush()

    async def __aenter__(self) -> Self:
        self._task = asyncio.create_task(self._pump())
        return self

    async def __aexit__(self, *exc: object) -> None:
        if self._task is not None:
            self._task.cancel()
            await asyncio.gather(self._task, return_exceptions=True)
        # 收尾再冲一次，否则该阶段最后一批事件会丢在队列里。
        await self._flush()


def _stage_progress(event: dict[str, Any]) -> dict[str, Any] | None:
    """把一条阶段事件转成 report_progress 的入参。

    ``step_id`` 必须带上 status。AgentK 用 step_id 做幂等，而同一阶段会先抛 ``running``
    再抛 ``done``、两者共用一个 id，不加后缀会被当成同一步的重试而互相覆盖。
    """
    if event.get("type") != "stage":
        return None
    step = str(event.get("id") or "").strip()
    status = str(event.get("status") or "").strip()
    if not step or not status:
        return None
    detail = {
        key: event.get(key)
        for key in ("kind", "status", "input", "output", "detail")
        if event.get(key)
    }
    return {
        "stepId": f"{step}.{status}",
        "message": str(event.get("title") or step),
        "data": detail or None,
    }


def _as_application_error(exc: BaseException) -> ApplicationError:
    """应用层异常一律标成不可重试。

    workflow 侧留的那次重试是给"进程/容器丢失"的（心跳超时后 Temporal 重派）。代码
    bug、契约校验失败、素材解不开这类确定性错误重跑只会再失败一次，白等一轮，还让
    调用方晚拿到原因。
    """
    return ApplicationError(str(exc), type=type(exc).__name__, non_retryable=True)


class StageActivities:
    """八个阶段 activity。

    持有 Temporal client 是因为 activity 要主动 signal 回自己的 workflow；SDK 没有在
    activity 上下文里提供 client，所以由 ``worker.py`` 注入连接好的那一个。
    """

    def __init__(self, client: Any, progress_signal: str) -> None:
        self._client = client
        self._signal = progress_signal
        self._state = shared_state_store()

    # -- 通用骨架 ---------------------------------------------------------

    async def _run_stage(
        self,
        stage: str,
        run_key: str,
        from_stage: str | None,
        work: Callable[[dict[str, Any], Callable[..., None]], dict[str, Any]],
    ) -> StageResult:
        """所有阶段共用的外壳：读状态 → 跑阶段 → 写状态 → 回摘要。

        阶段正文在工作线程里跑（阻塞的 ffmpeg 在里面），主协程只做进度与心跳，
        这样长阶段不会因为"没心跳"被判失联。
        """
        state = self._state.get(run_key, from_stage) if from_stage else {}
        async with _ProgressRelay(self._client, self._signal) as relay:
            try:
                state = await asyncio.to_thread(work, state, relay.on_event)
            except (ApplicationError, asyncio.CancelledError):
                raise
            except Exception as exc:
                raise _as_application_error(exc) from exc
        self._state.put(run_key, stage, state)
        return StageResult(runKey=run_key, stage=stage, summary=_summarize(stage, state))

    async def _run_model_stage(
        self,
        stage: str,
        args: ModelStepInput,
        work: Callable[..., dict[str, Any]],
    ) -> ModelStep:
        """含模型调用的阶段：可重入。

        与 ``_run_stage`` 的唯一区别是多接一条出口——阶段正文跑到需要模型调用时会抛
        ``NeedsModelCall``，这里把请求交回 workflow，**不写状态**。等 workflow 调完带着
        响应再进来一次，阶段正文从头重放，走到同一个点时响应已经在手里，于是继续往下。

        不写状态是关键：中间几轮的 usage / trace / 产物累积随 client 实例一起丢弃，
        只有跑到阶段末尾的那一遍才落库。所以最终记下来的恰好是完整的一份，不重不漏。
        """
        from aigc_eval.providers.model_call import NeedsModelCall

        state = self._state.get(args.runKey, args.fromStage)
        async with _ProgressRelay(self._client, self._signal) as relay:
            try:
                state = await asyncio.to_thread(
                    work, state, relay.on_event, args.responses
                )
            except NeedsModelCall as pending:
                return ModelStep(request=pending.request.model_dump(mode="json"))
            except (ApplicationError, asyncio.CancelledError):
                raise
            except Exception as exc:
                raise _as_application_error(exc) from exc
        self._state.put(args.runKey, stage, state)
        return ModelStep(
            result=StageResult(
                runKey=args.runKey, stage=stage, summary=_summarize(stage, state)
            )
        )

    # -- 1 / 1b / 2：取素材、文件检查、V2V 测量、抽帧 ----------------------

    @activity.defn(name=STAGE_ACTIVITIES["prepare"])
    async def prepare(self, args: VideoJudgeInput) -> StageResult:
        info = activity.info()
        run_key = info.workflow_run_id or info.workflow_id

        def work(_: dict[str, Any], on_event: Callable[..., None]) -> dict[str, Any]:
            return _do_prepare(args, on_event, run_key)

        return await self._run_stage("prepare", run_key, None, work)

    # -- 3：拆解提示词 -----------------------------------------------------

    @activity.defn(name=STAGE_ACTIVITIES["compile"])
    async def compile_prompt(self, args: ModelStepInput) -> ModelStep:
        return await self._run_model_stage("compile", args, _do_compile)

    # -- 4：看视频、记录事实 -----------------------------------------------

    @activity.defn(name=STAGE_ACTIVITIES["observe"])
    async def observe(self, args: ModelStepInput) -> ModelStep:
        return await self._run_model_stage("observe", args, _do_observe)

    # -- 4b：原生八维主判 --------------------------------------------------

    @activity.defn(name=STAGE_ACTIVITIES["scan"])
    async def scan(self, args: ModelStepInput) -> ModelStep:
        return await self._run_model_stage("scan", args, _do_scan)

    # -- 4c：疑点复核 + 音频复听 -------------------------------------------

    @activity.defn(name=STAGE_ACTIVITIES["recheck"])
    async def recheck(self, args: ModelStepInput) -> ModelStep:
        return await self._run_model_stage("recheck", args, _do_recheck)

    # -- 5：事实聚合（确定性） ---------------------------------------------

    @activity.defn(name=STAGE_ACTIVITIES["aggregate"])
    async def aggregate(self, args: StageInput) -> StageResult:
        return await self._run_stage(
            "aggregate", args.runKey, args.fromStage, _do_aggregate
        )

    # -- 6：问题定位与证据卡 -----------------------------------------------

    @activity.defn(name=STAGE_ACTIVITIES["evidence"])
    async def evidence(self, args: ModelStepInput) -> ModelStep:
        return await self._run_model_stage("evidence", args, _do_evidence)

    # -- 7：报告组装并投影成对外契约 ---------------------------------------

    @activity.defn(name=STAGE_ACTIVITIES["report"])
    async def report(self, args: StageInput) -> VideoJudgeOutput:
        state = self._state.get(args.runKey, args.fromStage)
        async with _ProgressRelay(self._client, self._signal) as relay:
            try:
                state = await asyncio.to_thread(_do_report, state, relay.on_event)
            except (ApplicationError, asyncio.CancelledError):
                raise
            except Exception as exc:
                raise _as_application_error(exc) from exc
        self._state.put(args.runKey, "report", _without_report(state))
        from activities import project_output

        return project_output(state["report"], VideoJudgeInput(**state["args"]))


def _summarize(stage: str, state: dict[str, Any]) -> dict[str, Any]:
    """进 workflow 历史的小摘要。只放定位问题用得上的计数。"""
    if stage == "prepare":
        return {
            "cached": bool(state.get("cachedReport")),
            "mode": state.get("resolvedMode"),
            "frames": len((state.get("mediaBundle") or {}).get("frames") or []),
        }
    if stage == "compile":
        return {
            "constraints": len((state.get("promptGraph") or {}).get("constraints") or [])
        }
    if stage == "observe":
        return {
            "observations": len(
                (state.get("observationsBundle") or {}).get("observations") or []
            ),
            "nativeVideo": bool(state.get("native")),
        }
    if stage == "scan":
        return {"issues": len((state.get("uni") or {}).get("issues") or [])}
    if stage == "recheck":
        return {"candidates": len(state.get("recheckCandidates") or [])}
    if stage == "aggregate":
        return {"rubricViolations": state.get("rubricViolations")}
    if stage == "evidence":
        return {"evidenceCards": len(state.get("evidenceCards") or [])}
    return {}


def _without_report(state: dict[str, Any]) -> dict[str, Any]:
    """写回状态时去掉整份报告。

    报告已经落了 HTML 与结果缓存，再往状态里塞一份只是把同样的几百 KB 多存一遍。
    """
    return {key: value for key, value in state.items() if key != "report"}


# ===========================================================================
# 阶段正文。全部是同步函数，跑在工作线程里，不碰事件循环。
#
# 每个都遵循同一形状：从状态里取出自己要的部分 → 调 pipeline 的阶段函数 → 把结果
# 写回状态并返回。pipeline 侧的阶段函数一行不改，所以本地单机跑法与 worker 跑法走的
# 是同一段判定逻辑。
# ===========================================================================


def _artifact_store() -> Any:
    from aigc_eval.judge.artifact_store import shared_artifact_store

    return shared_artifact_store()


def _session(state: dict[str, Any]) -> Any:
    from aigc_eval.trace_store import TraceSession

    return TraceSession.from_state(state["trace"])


def _relay_client(
    state: dict[str, Any],
    session: Any,
    on_event: Any,
    responses: dict[str, list[dict[str, Any]]],
) -> Any:
    """建一个把模型调用交给 workflow 的 client。

    这是 worker 路径与 studio 路径唯一的分岔点。分岔只有这一处，判定逻辑因此完全
    不知道自己跑在哪条路上——``_stage_observe`` 之类的阶段函数一行没改。
    """
    from aigc_eval.judge.pipeline import build_relay_client

    return build_relay_client(
        provider=str(state["args"]["provider"]),
        session=session,
        responses=responses,
        on_event=on_event,
        refresh=bool(state["args"].get("refresh")),
    )


def _close_model_stage(
    state: dict[str, Any], session: Any, client: Any
) -> dict[str, Any]:
    """模型阶段收尾：把 trace、用量、检查点统计汇进状态。

    用量必须累计而不是覆盖：成本要的是整次质检的合计值，只留最后一段等于把账少算。
    """
    state["trace"] = session.to_state()
    state["usage"] = list(state.get("usage") or []) + list(getattr(client, "usage", []))
    stats = getattr(client, "checkpoint_stats", None)
    if callable(stats):
        state["checkpoint"] = merge_checkpoint_stats(state.get("checkpoint"), stats())
    return state


def _do_prepare(
    args: VideoJudgeInput, on_event: Any, run_key: str
) -> dict[str, Any]:
    from media_intake import MediaIntakeError, resolve_media

    from aigc_eval.dataset.upload_case import build_upload_case_files
    from aigc_eval.judge.cases import detect_mode, load_prompt
    from aigc_eval.judge.pipeline import (
        _stage_prepare,
        load_result_cache,
        make_emit,
        result_cache_path,
    )
    from aigc_eval.paths import RUNTIME_ROOT
    from aigc_eval.trace_store import TraceSession

    emit = make_emit(on_event)
    staging = RUNTIME_ROOT / "worker_intake" / run_key
    try:
        video_path = resolve_media(
            args.videoUrl, dest_dir=staging, name="generated", kind="video"
        )
        references: list[dict[str, Any]] = []
        if args.sourceVideoUrl:
            source_path = resolve_media(
                args.sourceVideoUrl, dest_dir=staging, name="source", kind="video"
            )
            references.append(
                {
                    "path": str(source_path),
                    "kind": "video",
                    "expected_role": "V2V 源视频（改动前基线）",
                }
            )
        for index, url in enumerate(args.referenceImageUrls, start=1):
            image_path = resolve_media(
                url, dest_dir=staging, name=f"ref_{index:02d}", kind="image"
            )
            references.append({"path": str(image_path), "kind": "image"})
        for index, url in enumerate(args.referenceAudioUrls, start=1):
            audio_path = resolve_media(
                url, dest_dir=staging, name=f"ref_audio_{index:02d}", kind="audio"
            )
            references.append({"path": str(audio_path), "kind": "audio"})
    except MediaIntakeError as exc:
        raise ApplicationError(
            str(exc), type="InvalidArgument", non_retryable=True
        ) from exc

    case_files = build_upload_case_files(
        prompt=args.prompt, video_path=video_path, references=references
    )
    prompt = load_prompt(case_files)
    prompt_text = prompt["zh"] or prompt["en"]
    resolved_mode = detect_mode(case_files, args.mode)
    cache_path = result_cache_path(
        case_files.slug,
        source="upload",
        use_vlm=True,
        provider=args.provider,
        mode=args.mode,
    )

    state: dict[str, Any] = {
        "args": args.model_dump(),
        "caseFiles": serialize_case_files(case_files),
        "prompt": prompt,
        "promptText": prompt_text,
        "resolvedMode": resolved_mode,
        "source": "upload",
        "sourceLabel": "上传视频",
        "cachePath": str(cache_path),
        "engine": args.provider,
        "vlmError": None,
        "usage": [],
        "checkpoint": None,
        "artifacts": {},
    }

    # 整份报告的缓存命中就没必要往下跑八个阶段了：直接把它带到报告阶段投影成契约。
    # 与单 activity 时代的语义一致（``cachedResult=true``、不产生模型开销）。
    cached = load_result_cache(cache_path, refresh=bool(args.refresh), emit=emit)
    if cached is not None:
        state["cachedReport"] = cached
        return state

    session = TraceSession(
        case_id=case_files.case_id,
        mode=resolved_mode,
        engine=args.provider,
        model=None,
    )
    prepared = _stage_prepare(
        emit=emit,
        session=session,
        case_files=case_files,
        video_path=case_files.video_path,
        source="upload",
        source_label="上传视频",
        resolved_mode=resolved_mode,
        store=_artifact_store(),
    )
    state.update(
        {
            "trace": session.to_state(),
            "metadata": prepared["metadata"],
            "integrity": prepared["integrity"],
            "sourceVideoPath": (
                str(prepared["source_video_path"])
                if prepared["source_video_path"]
                else None
            ),
            "v2vAlign": prepared["v2v_align"],
            "mediaBundle": serialize_media_bundle(prepared["media_bundle"]),
            "motionSummary": prepared["motion_summary"],
            "artifacts": prepared["artifacts"],
        }
    )
    return state


def _do_compile(
    state: dict[str, Any],
    on_event: Any,
    responses: dict[str, list[dict[str, Any]]],
) -> dict[str, Any]:
    from aigc_eval.judge.pipeline import _stage_compile, make_emit

    session = _session(state)
    client = _relay_client(state, session, on_event, responses)
    compiled = _stage_compile(
        emit=make_emit(on_event),
        client=client,
        case_files=deserialize_case_files(state["caseFiles"]),
        prompt_text=state["promptText"],
        resolved_mode=state["resolvedMode"],
        store=_artifact_store(),
        artifacts=state["artifacts"],
    )
    state["promptGraph"] = compiled["prompt_graph"]
    state["hasRef"] = compiled["has_ref"]
    return _close_model_stage(state, session, client)


def _do_observe(
    state: dict[str, Any],
    on_event: Any,
    responses: dict[str, list[dict[str, Any]]],
) -> dict[str, Any]:
    from aigc_eval.judge.pipeline import _stage_observe, make_emit

    session = _session(state)
    client = _relay_client(state, session, on_event, responses)
    case_files = deserialize_case_files(state["caseFiles"])
    observed = _stage_observe(
        emit=make_emit(on_event),
        client=client,
        case_files=case_files,
        video_path=case_files.video_path,
        source_video_path=(
            Path(state["sourceVideoPath"]) if state.get("sourceVideoPath") else None
        ),
        prompt_text=state["promptText"],
        resolved_mode=state["resolvedMode"],
        prompt_graph=state["promptGraph"],
        metadata=state["metadata"],
        integrity=state["integrity"],
        media_bundle=deserialize_media_bundle(state["mediaBundle"]),
        motion_summary=state["motionSummary"],
        v2v_align=state.get("v2vAlign"),
        has_ref=bool(state.get("hasRef")),
        store=_artifact_store(),
        artifacts=state["artifacts"],
    )
    state["observationsBundle"] = observed["observations_bundle"]
    state["native"] = observed["native"]
    state["focus"] = observed["focus"]
    return _close_model_stage(state, session, client)


def _do_scan(
    state: dict[str, Any],
    on_event: Any,
    responses: dict[str, list[dict[str, Any]]],
) -> dict[str, Any]:
    from aigc_eval.judge.pipeline import _stage_scan, make_emit

    session = _session(state)
    client = _relay_client(state, session, on_event, responses)
    case_files = deserialize_case_files(state["caseFiles"])
    observations_bundle = state["observationsBundle"]
    scanned = _stage_scan(
        emit=make_emit(on_event),
        session=session,
        client=client,
        provider=str(state["args"]["provider"]),
        case_files=case_files,
        source=state["source"],
        video_path=case_files.video_path,
        source_video_path=(
            Path(state["sourceVideoPath"]) if state.get("sourceVideoPath") else None
        ),
        prompt=state["prompt"],
        prompt_text=state["promptText"],
        prompt_graph=state["promptGraph"],
        resolved_mode=state["resolvedMode"],
        metadata=state["metadata"],
        media_bundle=deserialize_media_bundle(state["mediaBundle"]),
        v2v_align=state.get("v2vAlign"),
        observations_bundle=observations_bundle,
        focus=state.get("focus") or [],
        native=bool(state.get("native")),
        store=_artifact_store(),
        artifacts=state["artifacts"],
    )
    uni = scanned["uni"]
    observations_bundle["native8_scan"] = uni
    observations_bundle["_zoom_frames"] = uni.get("_frames") or []
    state["uni"] = uni
    state["observationsBundle"] = observations_bundle
    state["artifacts"].update(scanned["artifacts"])
    return _close_model_stage(state, session, client)


def _do_recheck(
    state: dict[str, Any],
    on_event: Any,
    responses: dict[str, list[dict[str, Any]]],
) -> dict[str, Any]:
    from copy import deepcopy

    from aigc_eval.judge.pipeline import _stage_recheck, make_emit

    session = _session(state)
    client = _relay_client(state, session, on_event, responses)
    case_files = deserialize_case_files(state["caseFiles"])
    candidates = _stage_recheck(
        emit=make_emit(on_event),
        client=client,
        video_path=case_files.video_path,
        prompt_text=state["promptText"],
        prompt_graph=state["promptGraph"],
        metadata=state["metadata"],
        observations_bundle=state["observationsBundle"],
        v2v_align=state.get("v2vAlign"),
        uni=state["uni"],
        focus=state.get("focus") or [],
        store=_artifact_store(),
        artifacts=state["artifacts"],
    )
    state["recheckCandidates"] = candidates
    # 4b 是唯一事实底稿；4c 只在候选 issue 上增量取证。
    state["verdictBundle"] = deepcopy(state["uni"])
    return _close_model_stage(state, session, client)


def _do_aggregate(state: dict[str, Any], on_event: Any) -> dict[str, Any]:
    from aigc_eval.judge.pipeline import _stage_aggregate

    aggregated = _stage_aggregate(
        case_files=deserialize_case_files(state["caseFiles"]),
        resolved_mode=state["resolvedMode"],
        metadata=state["metadata"],
        prompt_graph=state["promptGraph"],
        observations_bundle=state["observationsBundle"],
        verdict_bundle=state["verdictBundle"],
        recheck_candidates=state.get("recheckCandidates") or [],
        v2v_align=state.get("v2vAlign"),
        use_vlm=True,
    )
    state["observations"] = aggregated["observations"]
    state["nativeDraft"] = aggregated["native_draft"]
    state["reviewFunnel"] = aggregated["review_funnel"]
    state["rubricViolations"] = aggregated["rubric_violations"]
    return state


def _do_evidence(
    state: dict[str, Any],
    on_event: Any,
    responses: dict[str, list[dict[str, Any]]],
) -> dict[str, Any]:
    from aigc_eval.judge.pipeline import _stage_evidence, make_emit

    session = _session(state)
    client = _relay_client(state, session, on_event, responses)
    case_files = deserialize_case_files(state["caseFiles"])
    evidence = _stage_evidence(
        emit=make_emit(on_event),
        session=session,
        client=client,
        case_files=case_files,
        source=state["source"],
        video_path=case_files.video_path,
        prompt=state["prompt"],
        native_draft=state["nativeDraft"],
        observations_bundle=state["observationsBundle"],
        use_vlm=True,
        store=_artifact_store(),
        artifacts=state["artifacts"],
    )
    state["groundingBundle"] = evidence["grounding_bundle"]
    state["evidenceCards"] = evidence["evidence_cards"]
    state["artifacts"].update(evidence["artifacts"])
    return _close_model_stage(state, session, client)


def _do_report(state: dict[str, Any], on_event: Any) -> dict[str, Any]:
    from aigc_eval.judge.pipeline import _stage_report, make_emit

    if state.get("cachedReport"):
        # 缓存命中：前面的阶段一个都没跑，这里直接把历史报告端出去。
        state["report"] = state["cachedReport"]
        return state

    session = _session(state)
    case_files = deserialize_case_files(state["caseFiles"])
    state["report"] = _stage_report(
        emit=make_emit(on_event),
        session=session,
        client=UsageCarrier(state.get("usage") or [], state.get("checkpoint")),
        case_files=case_files,
        source=state["source"],
        source_label=state["sourceLabel"],
        resolved_mode=state["resolvedMode"],
        prompt=state["prompt"],
        prompt_graph=state["promptGraph"],
        metadata=state["metadata"],
        integrity=state["integrity"],
        media_bundle=deserialize_media_bundle(state["mediaBundle"]),
        motion_summary=state["motionSummary"],
        source_video_path=(
            Path(state["sourceVideoPath"]) if state.get("sourceVideoPath") else None
        ),
        v2v_align=state.get("v2vAlign"),
        observations=state["observations"],
        observations_bundle=state["observationsBundle"],
        native_draft=state["nativeDraft"],
        review_funnel=state.get("reviewFunnel") or {},
        rubric_violations=int(state.get("rubricViolations") or 0),
        evidence_cards=state.get("evidenceCards") or [],
        grounding_bundle=state.get("groundingBundle") or {},
        engine=str(state.get("engine") or "agentk"),
        vlm_error=state.get("vlmError"),
        cache_path=Path(state["cachePath"]),
        store=_artifact_store(),
        artifacts=state["artifacts"],
    )
    state["trace"] = session.to_state()
    return state
