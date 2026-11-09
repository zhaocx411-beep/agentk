"""质检 Activity：取素材、跑 analyze_case、上传报告、把结论投影成对外契约。

所有阻塞动作都在这里，workflow 只负责编排与转发进度。

进度中继的形状值得说明一下。``analyze_case`` 是同步函数，跑在 ``asyncio.to_thread``
的工作线程里，它的 ``on_event`` 回调也在那个线程被调用；而 signal 是协程。两者之间
用 ``loop.call_soon_threadsafe`` 过桥，再由 ``_pump`` 协程成批 signal 回 workflow。
成批而非逐条是因为每次 ``report_progress`` 在 workflow 侧都是一趟完整的 activity
往返，逐条转发会把一次质检的历史撑得很长。
"""

from __future__ import annotations

import asyncio
import json
import logging
from pathlib import Path
from typing import Any

from abi import (
    INSPECT_ACTIVITY,
    PROGRESS_SIGNAL,
    JudgeArtifact,
    VideoJudgeInput,
    VideoJudgeOutput,
)
from media_intake import MediaIntakeError, resolve_media
from temporalio import activity
from temporalio.exceptions import ApplicationError

logger = logging.getLogger(__name__)

# 进度批次的合流窗口。质检阶段是分钟级的，1 秒的延迟在观感上无差别，
# 但足以把「同一瞬间连着抛出的几条事件」并成一次 signal。
_PUMP_INTERVAL_SECONDS = 1.0


def _stage_progress(event: dict[str, Any]) -> dict[str, Any] | None:
    """把一条 on_event 阶段事件转成 report_progress 的入参。

    ``step_id`` 必须带上 status。AgentK 用 step_id 做幂等，而同一阶段会先抛
    ``running`` 再抛 ``done``、两者共用一个 id，不加后缀会被当成同一步的重试而互相覆盖，
    调用方就只能看到其中一个。
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


class VideoJudgeActivities:
    """持有 Temporal client，因为 Activity 需要主动 signal 回自己的 workflow。

    SDK 没有在 activity 上下文里提供 client，所以由 ``worker.py`` 把连接好的 client
    注入进来，而不是在 activity 内部另开一条连接。
    """

    def __init__(self, client: Any) -> None:
        self._client = client

    @activity.defn(name=INSPECT_ACTIVITY)
    async def inspect(self, args: VideoJudgeInput) -> VideoJudgeOutput:
        loop = asyncio.get_running_loop()
        info = activity.info()
        handle = self._client.get_workflow_handle(
            info.workflow_id,
            run_id=info.workflow_run_id,
        )
        queue: asyncio.Queue[dict[str, Any]] = asyncio.Queue()

        def on_event(event: dict[str, Any]) -> None:
            """在 analyze_case 的工作线程里被调用，只做入队。

            进度失败绝不能影响质检本身——这与 analyze_case 内部 emit 的处理一致。
            """
            payload = _stage_progress(event)
            if payload is None:
                return
            try:
                loop.call_soon_threadsafe(queue.put_nowait, payload)
            except RuntimeError:  # 事件循环已关闭，说明 activity 正在收尾
                pass

        async def flush() -> None:
            batch: list[dict[str, Any]] = []
            while not queue.empty():
                batch.append(queue.get_nowait())
            if not batch:
                return
            try:
                await handle.signal(PROGRESS_SIGNAL, batch)
            except Exception:  # noqa: BLE001 - 进度是旁路，丢了也要让质检跑完
                logger.warning("进度回传失败，已跳过 %d 条", len(batch), exc_info=True)

        async def pump() -> None:
            while True:
                await asyncio.sleep(_PUMP_INTERVAL_SECONDS)
                # 心跳与进度同频：质检单阶段可能几分钟不产出事件，
                # 没有心跳会被 Temporal 判成失联。
                activity.heartbeat()
                await flush()

        work_dir = Path(activity.info().workflow_run_id or "run")
        pump_task = asyncio.create_task(pump())
        try:
            report = await asyncio.to_thread(_run_inspection, args, on_event, work_dir)
        except (ApplicationError, asyncio.CancelledError):
            # 已经分过类的、以及取消，原样抛。
            raise
        except Exception as exc:
            # 应用层异常一律标成不可重试。workflow 侧允许一次重试，那次重试是留给
            # 「进程/容器丢失」的（心跳超时后 Temporal 重新派发，检查点让它跳过已
            # 完成的模型调用）。而代码 bug、契约校验失败、素材解不开这类确定性错误
            # 重跑一次只会再失败一次，白等一轮还让调用方晚拿到原因。
            raise ApplicationError(
                str(exc), type=type(exc).__name__, non_retryable=True
            ) from exc
        finally:
            pump_task.cancel()
            await asyncio.gather(pump_task, return_exceptions=True)
            # 收尾再冲一次，否则最后一批事件（含「生成报告」完成）会丢在队列里。
            await flush()

        return project_output(report, args)


def _run_inspection(
    args: VideoJudgeInput,
    on_event: Any,
    work_dir: Path,
) -> dict[str, Any]:
    """同步跑完整条质检链。在工作线程里执行，不碰事件循环。"""
    # 延迟导入：这些模块会拉起 ffmpeg 探测与配置加载，放在模块顶层会拖慢
    # worker 启动，也让 workflow 侧的沙箱导入变重。
    from aigc_eval.dataset.upload_case import build_upload_case_files
    from aigc_eval.judge.pipeline import analyze_case
    from aigc_eval.paths import RUNTIME_ROOT

    staging = RUNTIME_ROOT / "worker_intake" / work_dir.name
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
    except MediaIntakeError as exc:
        # 入参问题重试不会变好，标成不可重试让调用方立刻拿到原因。
        raise ApplicationError(str(exc), type="InvalidArgument", non_retryable=True) from exc

    case_files = build_upload_case_files(
        prompt=args.prompt,
        video_path=video_path,
        references=references,
    )
    return analyze_case(
        case_files.case_id,
        case_files=case_files,
        use_vlm=True,
        provider=args.provider,
        on_event=on_event,
        source="upload",
        refresh=args.refresh,
    )


def _upload_report(report: dict[str, Any]) -> tuple[str | None, str | None]:
    """把 HTML 报告推到对象存储，返回 (链接, 失败原因)。

    报告只是附带产物：上传失败时结论照样返回，只在 ``reportWarning`` 里说明，
    不能因为存储抖动把一次已经花掉模型钱的质检整体判失败。
    """
    from aigc_eval.providers.blob_store import BlobUnavailable, shared_blob_store

    local = report.get("report_html_path")
    if not local or not Path(str(local)).exists():
        return None, str(report.get("report_error") or "报告未生成")
    try:
        store = shared_blob_store()
        return store.put_file(Path(str(local))), None
    except (BlobUnavailable, OSError) as exc:
        return None, f"报告上传失败：{exc}"


# 报告里指向本地帧文件的键。``path`` 来自采样与 zoom 帧，``image_path`` 来自证据帧。
_FRAME_KEYS = ("path", "image_path")
# 只对 worker 本机有意义的内部产物路径，导出时抹掉而不是上传。
_INTERNAL_PATH_KEYS = ("saved_path", "trace_saved_path", "report_html_path")


def _portable_report(report: dict[str, Any]) -> dict[str, Any]:
    """把报告里的本地帧路径换成对象存储 URL，得到一份可离机渲染的副本。

    不做这一步，导出的 JSON 只对与 worker 共享文件系统的调用方有用——证据帧全是
    ``/.../runtime/work/...`` 这样的绝对路径。帧本身在模型调用时多半已经传过，
    ``BlobStore`` 按内容寻址去重，所以这里基本不产生额外上传。

    单张帧上传失败不影响整体：该帧退化成没有图，其余照常导出。
    """
    from aigc_eval.providers.blob_store import BlobUnavailable, shared_blob_store

    store = shared_blob_store()
    cache: dict[str, str | None] = {}

    def url_for(local: str) -> str | None:
        if local not in cache:
            try:
                cache[local] = store.put_file(Path(local))
            except (BlobUnavailable, OSError):
                cache[local] = None
        return cache[local]

    def convert(node: Any) -> Any:
        if isinstance(node, dict):
            out: dict[str, Any] = {}
            for key, value in node.items():
                if key in _INTERNAL_PATH_KEYS:
                    continue
                if (
                    key in _FRAME_KEYS
                    and isinstance(value, str)
                    and value.startswith("/")
                    and Path(value).is_file()
                ):
                    out[key] = url_for(value)
                else:
                    out[key] = convert(value)
            return out
        if isinstance(node, list):
            return [convert(item) for item in node]
        return node

    return convert(report)


def _upload_report_json(report: dict[str, Any]) -> tuple[str | None, str | None]:
    """导出完整报告 JSON，返回 (链接, 失败原因)。

    对外契约 ``VideoJudgeOutput`` 只是结论投影，渲染详版报告（证据帧、子判据、
    审计轨迹）需要原始报告，所以额外给一份 JSON。同 HTML 一样属于附带产物，
    失败不影响结论返回。
    """
    from aigc_eval.providers.blob_store import BlobUnavailable, shared_blob_store

    try:
        payload = json.dumps(_portable_report(report), ensure_ascii=False)
        store = shared_blob_store()
        return store.put_bytes(payload.encode(), suffix=".json"), None
    except (BlobUnavailable, OSError, TypeError, ValueError) as exc:
        return None, f"报告 JSON 上传失败：{exc}"


def project_output(report: dict[str, Any], args: VideoJudgeInput) -> VideoJudgeOutput:
    """把 native8 报告投影成对外契约。

    公开（无下划线）是因为拆成多阶段后，报告 activity 在 ``stage_activities`` 里，
    投影必须与单 activity 路径共用同一份实现——两份投影一旦漂移，同一份报告在两条
    路径下会给出不同的对外结论。

    维度结论一律经 ``summarize_native8``，不自己解读 tier——直接读 tier 会把
    「子判据全未核实」的维度当成通过。
    """
    from aigc_eval.judge.native8 import summarize_native8
    from aigc_eval.report.html import NATIVE_REQUIREMENT_TEXT

    summary = summarize_native8(report)
    unverified_ids = set(summary.get("unverified_dimension_ids") or [])

    dimensions = []
    for dimension in report.get("rubric") or []:
        unresolved = list(dimension.get("unresolved_subcriteria") or [])
        applicable = dimension.get("applicable", True)
        dimensions.append(
            {
                "id": dimension.get("dimension_id") or "",
                "name": dimension.get("dimension_name") or "",
                "applicable": bool(applicable),
                "tier": dimension.get("tier"),
                "score": dimension.get("score"),
                "decisionStatus": dimension.get("decision_status")
                or ("review_required" if dimension.get("dimension_id") in unverified_ids else "resolved"),
                "unresolvedSubcriteria": unresolved,
            }
        )

    # 问题**自身不带**维度、标题和秒数：native8 把归属放在 rubric 侧，由子判据的
    # issue_ids 指向问题，时间只存在于逐条证据里。所以这里先按 issue_id 反查归属，
    # 再从证据帧取时间区间。照「问题里有 dimension_id / title / start_seconds」去读
    # 会得到一整列 null，而且因为字段缺失不报错，静默返回空结论。
    owners: dict[str, tuple[dict[str, Any], dict[str, Any]]] = {}
    for dimension in report.get("rubric") or []:
        for sub in dimension.get("subcriteria") or []:
            if not isinstance(sub, dict):
                continue
            for issue_id in sub.get("issue_ids") or []:
                owners.setdefault(str(issue_id), (dimension, sub))

    issues = []
    for issue in report.get("issues") or []:
        issue_id = str(issue.get("issue_id") or "")
        dimension, sub = owners.get(issue_id, ({}, {}))
        basis = sub.get("decision_basis") or {}
        times = [
            float(frame["time"])
            for frame in issue.get("evidence_frames") or []
            if isinstance(frame, dict) and isinstance(frame.get("time"), (int, float))
        ]
        key = str(sub.get("key") or "")
        issues.append(
            {
                "id": issue_id,
                "dimensionId": dimension.get("dimension_id") or "",
                "subcriterionId": key or None,
                # 子判据名（如「同镜头时序一致性」）是这条问题唯一的人类可读标签。
                "title": sub.get("name") or key or issue_id,
                "startSeconds": min(times) if times else None,
                "endSeconds": max(times) if times else None,
                "requirement": basis.get("expected") or NATIVE_REQUIREMENT_TEXT.get(key),
                "observed": basis.get("actual"),
                "explanation": issue.get("decision_summary") or basis.get("why_this_dimension"),
            }
        )

    report_url, warning = _upload_report(report)
    report_json_url, json_warning = _upload_report_json(report)
    warning = "；".join(w for w in (warning, json_warning) if w) or None
    mode_label = (report.get("case") or {}).get("mode") or "T2V"
    artifacts = []
    if report_url:
        artifacts.append(
            JudgeArtifact(
                type="html",
                uri=report_url,
                description=f"{mode_label} 视频质检报告",
            )
        )
    if report_json_url:
        artifacts.append(
            JudgeArtifact(
                type="json",
                uri=report_json_url,
                description=f"{mode_label} 视频质检报告（完整数据）",
            )
        )
    return VideoJudgeOutput(
        analysisStatus=str(summary.get("analysis_status") or "unknown"),
        mode=str((report.get("case") or {}).get("mode") or "T2V"),
        dimensions=dimensions,
        issues=issues,
        violatedSubcriteria=int(summary.get("violated_subcriteria") or 0),
        unresolvedSubcriteria=int(summary.get("unresolved_subcriteria") or 0),
        applicableDimensions=int(summary.get("applicable_dimensions") or 0),
        resolvedDimensions=int(summary.get("resolved_dimensions") or 0),
        unverifiedDimensions=int(summary.get("unverified_dimensions") or 0),
        unverifiedDimensionIds=sorted(unverified_ids),
        reviewRequiredCount=int(summary.get("review_required_count") or 0),
        reportUrl=report_url,
        reportJsonUrl=report_json_url,
        reportWarning=warning,
        artifacts=artifacts,
        cachedResult=bool(report.get("from_cache")),
    )
