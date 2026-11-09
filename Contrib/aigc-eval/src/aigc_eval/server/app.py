"""Zero-dependency HTTP server for 多模态视频质检.

只有一个功能：上传（或从离线数据集挑）一条生成视频 + 参考素材，跑一次质检，
产出可审计的报告。对外只暴露这条链路需要的接口 —— 上传、轮询、取素材、取报告。

Run with: python3 -m aigc_eval
Then visit: http://127.0.0.1:9900
"""

from __future__ import annotations

import json
import mimetypes
import os
import re
import sys
import threading
import traceback
import uuid
from http import HTTPStatus
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

from aigc_eval import paths
from aigc_eval.history import (
    delete_record as history_delete_record,
    history_case_sources,
    list_history,
    load_record as history_load_record,
    set_title as history_set_title,
)
from aigc_eval.providers.blob_store import load_blob_config
from aigc_eval.providers.capabilities import audio_support
from aigc_eval.config import (
    DEFAULT_PROVIDER,
    load_agentk_config,
    normalize_provider,
)
from aigc_eval.dataset.cache import (
    dataset_asset_path,
    dataset_case_detail,
    dataset_media_path,
    dataset_thumb_path,
    list_dataset_cases,
)
from aigc_eval.judge.pipeline import (
    RUNTIME_ROOT,
    media_path,
)
from aigc_eval.server.capability_job import run_capability_job, warm_up_transport
from aigc_eval.trace_store import TRACE_ROOT
from aigc_eval.dataset.upload_case import (
    UPLOAD_ROOT,
    build_upload_case_files,
    cleanup_upload,
    finalize_staging,
    kind_for_suffix,
    normalize_prompt,
    parse_multipart,
    upload_media_path,
)


APP_ROOT = paths.PACKAGE_ROOT
STATIC_ROOT = paths.STATIC_ROOT
REPORT_ROOT = paths.REPORT_ROOT

# 唯一的页面。文件名保持 studio.html，这样已经发出去的 /studio.html 链接不断。
INDEX_PAGE = "studio.html"

# 数据集参考素材的扫描上限：`dataset_ref_<i>` 是稀疏的（前端可能跳号），
# 只能按固定区间扫而不是数到第一个空为止。
_MAX_DATASET_REFS = 64

# ---------------------------------------------------------------------------
# Background analysis jobs (polling model — robust to dropped connections).
# Each job runs analyze_case in a worker thread; the browser polls /api/job
# for newly appended steps and the final report.
# ---------------------------------------------------------------------------

JOBS: dict[str, dict[str, object]] = {}
JOBS_LOCK = threading.Lock()
JOBS_MAX = 64


def _failed_stage_events(
    events: list[dict[str, object]], error: Exception
) -> list[dict[str, object]]:
    """把仍停在 running 的阶段补成 error 事件。

    管线里只有部分阶段自己 catch 后 emit 了失败；直接向上抛的那些（比如 observer）
    最后一条事件仍是 `running`，于是界面上那一行永远显示「运行中」——一个已经结束的
    失败被画成还在跑。这里在作业终态时统一补一条，前端不必知道哪些阶段自己报了错。
    """
    running: dict[str, dict[str, object]] = {}
    for event in events:
        if not isinstance(event, dict) or event.get("type") != "stage":
            continue
        key = str(event.get("id") or event.get("title") or "")
        if not key:
            continue
        if str(event.get("status") or "") == "running":
            running[key] = event
        else:
            running.pop(key, None)
    detail = str(error)
    return [
        {
            "type": "stage",
            "id": key,
            "kind": str(event.get("kind") or "tool"),
            "title": str(event.get("title") or key),
            "status": "error",
            "output_text": detail[:400],
        }
        for key, event in running.items()
    ]


def _job_start(params: dict[str, object]) -> str:
    job_id = uuid.uuid4().hex[:12]
    with JOBS_LOCK:
        JOBS[job_id] = {"status": "running", "events": [], "report": None, "error": None}
        while len(JOBS) > JOBS_MAX:
            JOBS.pop(next(iter(JOBS)), None)

    def on_event(event: dict[str, object]) -> None:
        with JOBS_LOCK:
            job = JOBS.get(job_id)
            if job is not None:
                job["events"].append(event)  # type: ignore[union-attr]

    def worker() -> None:
        try:
            report = run_capability_job(on_event=on_event, **params)  # type: ignore[arg-type]
            with JOBS_LOCK:
                job = JOBS.get(job_id)
                if job is not None:
                    job["report"] = report
                    job["status"] = "done"
        except Exception as error:  # noqa: BLE001 - surface to the poller
            # 页面上只看得到一句 str(error)。不打堆栈的话，像"signal only works in
            # main thread"这种没有业务语义的报错完全无从定位。
            traceback.print_exc()
            with JOBS_LOCK:
                job = JOBS.get(job_id)
                if job is not None:
                    job["events"].extend(_failed_stage_events(job["events"], error))  # type: ignore[union-attr,arg-type]
                    job["error"] = str(error)
                    job["status"] = "error"

    threading.Thread(target=worker, daemon=True).start()
    return job_id


def _job_snapshot(job_id: str, cursor: int) -> dict[str, object]:
    with JOBS_LOCK:
        job = JOBS.get(job_id)
        if job is None:
            raise FileNotFoundError("任务不存在或已过期")
        events = job["events"]  # type: ignore[index]
        new_events = list(events[cursor:])  # type: ignore[index]
        status = job["status"]
        payload: dict[str, object] = {
            "status": status,
            "events": new_events,
            "next_cursor": len(events),  # type: ignore[arg-type]
        }
        if status == "done":
            payload["report"] = job["report"]
        elif status == "error":
            payload["error"] = job["error"]
        return payload


HOST = "0.0.0.0"
PORT = int(os.environ.get("VIDEO_JUDGE_PORT", "9900"))


class VideoJudgeHandler(SimpleHTTPRequestHandler):
    server_version = "VideoJudgeDemo/0.2"
    # HTTP/1.0 每个响应后即关连接；经反向代理从外网上传几十 MB 时，连接会在
    # 请求体读完前被判定断开，浏览器拿到代理的纯文本错误页而非 JSON。
    protocol_version = "HTTP/1.1"

    def log_message(self, format: str, *args: object) -> None:
        sys.stdout.write("[video-judge] " + format % args + "\n")

    def _send_json(
        self,
        payload: object,
        status: HTTPStatus = HTTPStatus.OK,
        *,
        compact: bool = False,
    ) -> None:
        """``compact`` 去掉分隔符里的空格，输出 ``{"a":1}`` 而不是 ``{"a": 1}``。

        只有 ``/healthz`` 需要它：容器云的 readiness 探针拿返回体与配置里的期望值做
        **字符串精确比对**，`json.dumps` 默认在冒号后留一个空格，肉眼看不出差别但比不过。
        平台侧那份期望值是按 FastAPI 的输出（紧凑）写的，兄弟项目因此天然对得上。
        """
        # Body may be UTF-8; HTTP status/reason must stay ASCII (latin-1).
        separators = (",", ":") if compact else None
        content = json.dumps(
            payload, ensure_ascii=False, separators=separators
        ).encode("utf-8")
        self.send_response(int(status))
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(content)))
        self.end_headers()
        self.wfile.write(content)

    def _send_static_asset(self, path: Path, content_type: str | None = None) -> None:
        """发送内容不可变的离线素材：支持 HTTP Range + 长缓存，且流式读盘。

        `_send_file` 会 `read_bytes()` 整个文件再发（一条 107MB 视频就占 107MB 内存），
        且带 `no-store` —— 浏览器每次展开都要重下。离线数据集的文件是只读且内容固定的，
        所以这里改成：
          · ETag/immutable 长缓存 —— 同一张图第二次展开直接走 304，不再过网；
          · Range 206 —— `<video>` 只取它需要的片段，而不是把整片推给浏览器
            （不支持 Range 时浏览器无法拖进度条，且会整片下载，这是卡顿主因之一）。
        """
        if not path.exists() or not path.is_file():
            self._send_json({"error": f"文件不存在：{path.name}"}, HTTPStatus.NOT_FOUND)
            return
        stat = path.stat()
        total = stat.st_size
        etag = f'"{int(stat.st_mtime)}-{total}"'
        mime = content_type or mimetypes.guess_type(path.name)[0] or "application/octet-stream"

        # 304 只能回给**不带 Range** 的请求。浏览器 seek 视频时会同时发
        # Range 与 If-None-Match，若在这里就返回空的 304，播放器拿不到任何
        # 字节 —— 表现为进度条能拖、点问题帧也能跳，但画面始终不更新。
        # 按 RFC 9110：带 If-Range 之外的条件头 + Range 时，仍应返回 206。
        if self.headers.get("If-None-Match") == etag and not self.headers.get("Range"):
            self.send_response(HTTPStatus.NOT_MODIFIED)
            self.send_header("ETag", etag)
            self.send_header("Cache-Control", "public, max-age=31536000, immutable")
            self.end_headers()
            return

        start, end = 0, total - 1
        partial = False
        range_header = self.headers.get("Range") or ""
        match = re.match(r"bytes=(\d*)-(\d*)$", range_header.strip())
        if match and total:
            raw_start, raw_end = match.group(1), match.group(2)
            if raw_start:
                start = int(raw_start)
                if raw_end:
                    end = min(int(raw_end), total - 1)
            elif raw_end:
                # `bytes=-500` 表示最后 500 字节
                start = max(0, total - int(raw_end))
            if start >= total:
                self.send_response(HTTPStatus.REQUESTED_RANGE_NOT_SATISFIABLE)
                self.send_header("Content-Range", f"bytes */{total}")
                self.end_headers()
                return
            # 只要客户端**显式带了 Range**，就必须回 206 —— 哪怕它要的正好是整个
            # 文件（`bytes=0-`，这正是 <video> 的第一个请求）。以前这里按
            # "区间是否小于全长" 判断，于是 `bytes=0-` 回了 200；Chrome 把
            # "带 Range 却收到 200" 理解为服务端不支持 Range，直接把 seekable
            # 置为 [0,0]，此后任何 currentTime 赋值都被打回 0 ——
            # 表现就是进度条拖不动、点问题帧画面不变。
            partial = True

        length = end - start + 1
        self.send_response(HTTPStatus.PARTIAL_CONTENT if partial else HTTPStatus.OK)
        self.send_header("Content-Type", mime)
        self.send_header("Content-Length", str(length))
        self.send_header("Accept-Ranges", "bytes")
        self.send_header("ETag", etag)
        self.send_header("Cache-Control", "public, max-age=31536000, immutable")
        if partial:
            self.send_header("Content-Range", f"bytes {start}-{end}/{total}")
        self.end_headers()
        if self.command == "HEAD":
            return
        with path.open("rb") as handle:
            handle.seek(start)
            remaining = length
            while remaining > 0:
                chunk = handle.read(min(1 << 20, remaining))
                if not chunk:
                    break
                try:
                    self.wfile.write(chunk)
                except (BrokenPipeError, ConnectionResetError):
                    # 浏览器 seek 视频时会主动掐断上一个请求，属正常行为。
                    return
                remaining -= len(chunk)

    def _send_file(self, path: Path, content_type: str | None = None) -> None:
        if not path.exists() or not path.is_file():
            self._send_json({"error": f"文件不存在：{path.name}"}, HTTPStatus.NOT_FOUND)
            return
        data = path.read_bytes()
        self.send_response(HTTPStatus.OK)
        self.send_header(
            "Content-Type",
            content_type or mimetypes.guess_type(path.name)[0] or "application/octet-stream",
        )
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(data)

    def _read_json_body(self) -> dict[str, object]:
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else b"{}"
        payload = json.loads(body.decode("utf-8") or "{}")
        if not isinstance(payload, dict):
            raise ValueError("请求体必须是 JSON 对象")
        return payload

    def _upload_analyze(self) -> None:
        """multipart 上传 → 合成 CaseFiles → 后台起 job，返回 job_id。

        字段约定：`prompt`(text)、`video`(file, 待质检视频)、`ref`(file, 可多个参考素材)；
        第 i 个 `ref` 的标签由文本字段 `label_<i>`（从 0 起）给出。标签按序号而非文件名
        对应，因为同名文件完全可能出现两次，用文件名做 key 会互相覆盖。

        离线数据集复用：素材已在服务器磁盘上，浏览器只回传文件名而非字节 ——
        `dataset_case_id` + `dataset_video`(文件名) + `dataset_ref_<i>`(文件名/标签)。
        与上传文件可混用（比如用数据集的参考图配一条新上传的视频）。

        历史记录重跑：`history_slug` 指向 `runtime/uploads/<slug>/manifest.json`，
        素材同样原地引用。改了 prompt 就是改了内容寻址的输入，会算出新 slug、
        落成一条新记录，原记录不受影响。
        """
        staging = UPLOAD_ROOT / f"staging_{uuid.uuid4().hex[:12]}"
        try:
            fields, files = parse_multipart(
                self.rfile,
                self.headers.get("Content-Type") or "",
                int(self.headers.get("Content-Length") or 0),
                staging,
            )
            # 浏览器按 HTML 规范用 CRLF 传 <textarea>，磁盘上的 prompt.txt 是 LF；
            # 不归一化则同一条多行 prompt 会算出两个 slug，缓存永远打不中。
            # prompt 非必填：没有指令时退化成「无指令质检」，只跑不依赖指令的
            # 维度（画质 / 动态 / 音画），指令遵循维度由判定链路自行跳过。
            # 空串照样参与 slug 哈希，所以「有 prompt」和「无 prompt」是两条缓存。
            prompt = normalize_prompt(fields.get("prompt") or "").strip()

            dataset_case_id = (fields.get("dataset_case_id") or "").strip()
            history_slug = (fields.get("history_slug") or "").strip()

            # 历史重跑：视频与参考素材从 manifest 原地取，浏览器只回传 slug。
            # 与离线数据集同构 —— `history_ref_<i>` 给 manifest 里的原标签用于选条，
            # `history_label_<i>` 给界面上当前的标签（删掉一条会重新编号，
            # 两者可能不同；标签参与内容寻址，必须用界面上那个）。
            history_video: Path | None = None
            history_refs: list[dict[str, object]] = []
            if history_slug:
                history_video, available = history_case_sources(history_slug)
                picked: list[dict[str, object]] = []
                selected_any = False
                for index in range(_MAX_DATASET_REFS):
                    want = (fields.get(f"history_ref_{index}") or "").strip()
                    if not want:
                        continue
                    selected_any = True
                    match = next((r for r in available if str(r.get("label")) == want), None)
                    if match is None:
                        raise ValueError(f"历史参考素材不存在：{want}")
                    label = (fields.get(f"history_label_{index}") or "").strip()
                    picked.append({**match, "label": label or match.get("label")})
                # 没带任何选择字段时按整套取，保持接口对脚本调用友好。
                history_refs = picked if selected_any else available

            video_item = next((f for f in files if f["field"] == "video"), None)
            if video_item is not None:
                video_source = Path(video_item["path"])
            elif history_video is not None:
                video_source = history_video
            elif dataset_case_id and (fields.get("dataset_video") or "").strip():
                video_source = dataset_asset_path(
                    dataset_case_id, (fields.get("dataset_video") or "").strip()
                )
            else:
                raise ValueError("缺少待质检视频")

            references: list[dict[str, object]] = list(history_refs)
            for index, item in enumerate(f for f in files if f["field"] != "video"):
                label = (fields.get(f"label_{index}") or "").strip()
                references.append({
                    "path": item["path"],
                    "label": label or None,
                    "kind": kind_for_suffix(Path(item["filename"]).suffix),
                })
            # 数据集参考素材：`dataset_ref_<i>` 是文件名，标签同样按序号取。
            if dataset_case_id:
                for index in range(_MAX_DATASET_REFS):
                    filename = (fields.get(f"dataset_ref_{index}") or "").strip()
                    if not filename:
                        continue
                    ref_path = dataset_asset_path(dataset_case_id, filename)
                    label = (fields.get(f"dataset_label_{index}") or "").strip()
                    references.append({
                        "path": str(ref_path),
                        "label": label or None,
                        "kind": kind_for_suffix(ref_path.suffix),
                    })

            video_path, refs, _slug = finalize_staging(
                staging, prompt=prompt, video_path=video_source, references=references
            )

            case_files = build_upload_case_files(prompt=prompt, video_path=video_path, references=refs)
            # 排查缓存为何不命中：把真实请求算出的 slug 与 refresh 打出来，
            # 好和 runtime/results/ 里已有的文件名直接对照。
            _refresh = (fields.get("refresh") or "0") in {"1", "true", "yes"}
            print(f"[upload_analyze] slug={case_files.slug} refresh={_refresh} "
                  f"refs={len(refs)} prompt_len={len(prompt)} "
                  f"video={Path(video_source).name} dataset={dataset_case_id or '-'}",
                  flush=True)
            # use_vlm / allow_fallback 不再透传：质检改由 video_judge capability 执行，
            # 而它们没进对外契约——那是本地调试开关，不该出现在别人也要调的能力签名里。
            job_id = _job_start(
                {
                    "case_files": case_files,
                    "refresh": _refresh,
                    "provider": normalize_provider(fields.get("provider") or DEFAULT_PROVIDER),
                    "mode": (fields.get("mode") or "").strip() or None,
                }
            )
            self._send_json({
                "job_id": job_id,
                "case_id": case_files.case_id,
                "slug": case_files.slug,
                "mode": "I2VA" if any(r["kind"] == "image" for r in refs) else "T2VA",
                "references": [
                    {"label": r["label"], "kind": r["kind"], "expected_role": r["expected_role"]}
                    for r in refs
                ],
            })
        except Exception:
            cleanup_upload(staging)
            raise

    def do_POST(self) -> None:  # noqa: N802
        parsed = urlparse(self.path)
        path = parsed.path.rstrip("/") or "/"
        try:
            if path == "/api/upload_analyze":
                self._upload_analyze()
                return
            if path == "/api/history_title":
                payload = self._read_json_body()
                title = history_set_title(
                    str(payload.get("id") or ""), str(payload.get("title") or "")
                )
                self._send_json({"ok": True, "title": title, "title_custom": bool(title)})
                return
            if path == "/api/history_delete":
                payload = self._read_json_body()
                self._send_json(
                    history_delete_record(
                        str(payload.get("id") or ""),
                        with_assets=bool(payload.get("with_assets", True)),
                    )
                )
                return
            self._send_json({"error": "未知接口"}, HTTPStatus.NOT_FOUND)
        except ValueError as error:
            self._send_json({"error": str(error)}, HTTPStatus.BAD_REQUEST)
        except Exception as error:  # pragma: no cover
            self._send_json({"error": f"保存失败：{error}"}, HTTPStatus.INTERNAL_SERVER_ERROR)

    def do_GET(self) -> None:  # noqa: N802
        parsed = urlparse(self.path)
        query = parse_qs(parsed.query)
        path = parsed.path.rstrip("/") or "/"
        try:
            if path == "/healthz":
                # 容器云的存活检查打这里。路径与响应体都按仓库既有约定来
                # （agentk-capability-demo、maas-worker 都是 /healthz + {"status":"ok"}），
                # 平台侧的期望值配置照抄即可。
                #
                # 不要把这个检查指到 /api/health：那个端点是给页面看的诊断信息，
                # 恒返回 200 且 body 里没有 status 字段，期望值匹配不上；而且它报的是
                # 「配置齐不齐」，不是「进程活没活」——拿配置问题去触发容器重启是错的靶子。
                #
                # 只回一个常量就够，不必去探 worker：start.sh 让两个进程同生共死，
                # HTTP 还能应答就意味着 worker 也在。
                #
                # compact 必须留着：探针是字符串精确比对，多一个空格就判失败。
                self._send_json({"status": "ok"}, compact=True)
                return
            if path == "/api/health":
                agentk = load_agentk_config()
                blob = load_blob_config()
                self._send_json(
                    {
                        "ok": True,
                        "default_provider": DEFAULT_PROVIDER,
                        "providers": {
                            # audio_support 是排查用的只读信息：报告里某维度为什么
                            # 是「未核实」，先看这里当时走的是哪一档。
                            # AgentK 通道额外依赖对象存储：understand_media 只收 URL，
                            # 没有 blob 就算身份齐了也跑不通，所以一并暴露。
                            "agentk": {
                                "configured": agentk.configured and blob.configured,
                                "model": agentk.model if agentk.configured else None,
                                "display_name": "AgentK",
                                "api_configured": agentk.configured,
                                "blob_configured": blob.configured,
                                "base_url": agentk.base_url or None,
                                "max_media": agentk.max_media,
                                "audio_support": audio_support("agentk"),
                                # 服务名：排查鉴权失败时第一眼要看的就是
                                # 「网关认到的是哪个身份」。凭据本身不外露。
                                "service_name": agentk.service_name or None,
                            },
                        },
                    }
                )
                return
            if path == "/api/dataset_cases":
                self._send_json({"cases": list_dataset_cases()})
                return
            if path == "/api/dataset_case":
                self._send_json(dataset_case_detail(query.get("case_id", [""])[0]))
                return
            if path == "/api/dataset_media":
                path_media, mime_type = dataset_media_path(
                    query.get("case_id", [""])[0], query.get("file", [""])[0]
                )
                self._send_static_asset(path_media, mime_type)
                return
            if path == "/api/dataset_thumb":
                # 列表卡片只有 76×56px，直传原图（单张 4MB）是浏览器卡顿的主因。
                path_thumb, mime_type = dataset_thumb_path(
                    query.get("case_id", [""])[0], query.get("file", [""])[0]
                )
                self._send_static_asset(path_thumb, mime_type)
                return
            if path == "/api/history":
                limit = int(query.get("limit", ["200"])[0] or 200)
                self._send_json({"items": list_history(limit)})
                return
            if path == "/api/history_item":
                self._send_json(history_load_record(query.get("id", [""])[0]))
                return
            if path == "/api/job":
                job_id = query.get("job_id", [""])[0]
                cursor = int(query.get("cursor", ["0"])[0] or 0)
                self._send_json(_job_snapshot(job_id, cursor))
                return
            if path == "/api/report":
                run_id = query.get("run_id", [""])[0]
                if not run_id or "/" in run_id or ".." in run_id or "\\" in run_id:
                    raise ValueError("非法 run_id")
                report_path = (REPORT_ROOT / f"{run_id}.html").resolve()
                try:
                    report_path.relative_to(REPORT_ROOT.resolve())
                except ValueError as error:
                    raise ValueError("非法 run_id") from error
                self._send_file(report_path, "text/html; charset=utf-8")
                return
            if path == "/api/trace":
                run_id = query.get("run_id", [""])[0]
                if not run_id or "/" in run_id or ".." in run_id or "\\" in run_id:
                    raise ValueError("非法 run_id")
                TRACE_ROOT.mkdir(parents=True, exist_ok=True)
                trace_path = (TRACE_ROOT / f"{run_id}.json").resolve()
                try:
                    trace_path.relative_to(TRACE_ROOT.resolve())
                except ValueError as error:
                    raise ValueError("非法 run_id") from error
                if not trace_path.exists():
                    raise FileNotFoundError(f"轨迹不存在：{run_id}")
                self._send_file(trace_path, "application/json; charset=utf-8")
                return
            # UI 里已经没有本地案例集，但 batch/* 跑出来的报告 HTML 仍以这个
            # URL 引用磁盘上的案例素材；去掉它那些报告打开就是空白播放器。
            # 纯静态取文件，不触发任何模型调用。
            if path == "/api/case-media":
                path_media, mime_type = media_path(
                    query.get("case_id", [""])[0],
                    query.get("asset", [""])[0],
                    query.get("ref_id", [None])[0],
                )
                # 同 upload_media：不支持 206 时视频组件的进度条拖不动。
                self._send_static_asset(path_media, mime_type)
                return
            if path == "/api/upload_media":
                path_media, mime_type = upload_media_path(
                    query.get("slug", [""])[0],
                    query.get("asset", [""])[0],
                    query.get("ref_id", [None])[0],
                )
                # 必须走 Range 版：不支持 206 时浏览器不肯 seek，
                # 视频组件的进度条拖不动、点问题帧也停在首帧。
                self._send_static_asset(path_media, mime_type)
                return
            if path.startswith("/runtime/"):
                filename = Path(path).name
                self._send_file(RUNTIME_ROOT / filename)
                return
            # 质检是本服务唯一的页面，所以根路径直接给 studio.html；
            # /index.html 是历史书签的入口，一并落到同一页。
            requested = path.lstrip("/")
            if requested in {"", "index.html"}:
                requested = INDEX_PAGE
            static_path = (STATIC_ROOT / requested).resolve()
            if STATIC_ROOT.resolve() not in static_path.parents and static_path != (STATIC_ROOT / INDEX_PAGE).resolve():
                self._send_json({"error": "非法路径"}, HTTPStatus.FORBIDDEN)
                return
            self._send_file(static_path)
        except FileNotFoundError as error:
            self._send_json({"error": str(error)}, HTTPStatus.NOT_FOUND)
        except ValueError as error:
            # UnicodeEncodeError subclasses ValueError; never treat protocol encode glitches as 404.
            if isinstance(error, UnicodeError):
                self._send_json({"error": f"分析失败：{error}"}, HTTPStatus.INTERNAL_SERVER_ERROR)
                return
            self._send_json({"error": str(error)}, HTTPStatus.NOT_FOUND)
        except Exception as error:  # pragma: no cover
            self._send_json({"error": f"分析失败：{error}"}, HTTPStatus.INTERNAL_SERVER_ERROR)


def main() -> None:
    RUNTIME_ROOT.mkdir(parents=True, exist_ok=True)
    # 必须在这里、必须在起服务之前：质检跑在子线程，而 KESS 只能在主线程完成初始化。
    warm_up_transport()
    with ThreadingHTTPServer((HOST, PORT), VideoJudgeHandler) as server:
        print(f"Video Judge Demo: http://{HOST}:{PORT}")
        server.serve_forever()


if __name__ == "__main__":
    main()
