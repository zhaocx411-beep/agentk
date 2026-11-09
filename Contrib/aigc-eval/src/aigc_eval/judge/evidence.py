"""确定性证据层：给每条成立的 issue 配上时刻 + 原生帧（可带红框）。

存在的意义是让 reviewer 一眼判断「模型说的缺陷是真的，还是幻觉」。整个阶段跑在
定档**之后**，只补图不改判定，因此永远 best-effort —— 抽帧失败不该让整条 case 挂掉。

两条与旧版（v41）的关键差异：

1. **只按 `ISSUE-ID` 挂载。** 旧版要在 rubric key / rule_id / hard_failure type
   三套命名间做匹配，匹配不上的问题永远拿不到框。native8 里 issue_id 是唯一主键，
   证据卡、bbox、报告页跳转全走它。
2. **支持多帧。** 模型会为可视觉定位的问题给出 2~4 个 `evidence_frames`
   （前态 / 问题态 / 后态），报告页并排展示。缺失时退回单帧的旧行为。

bbox 只来自专门的 grounding 定位阶段 —— 它是唯一真的看像素并输出坐标的环节，
4b/4c 都是文本裁决，拿不到坐标。
"""

from __future__ import annotations

import re
import subprocess
from pathlib import Path
from typing import Any

from aigc_eval.judge.integrity import extract_native_frames
from aigc_eval import paths

RUNTIME_ROOT = paths.RUNTIME_ROOT
# 与 pipeline.work_dir 同一套布局：中间产物收进 runtime/work/<slug>__<source>/<run_id>/，
# 不平铺在 runtime/ 顶层。证据图由 report/html 以 base64 内嵌，不经 URL 路由，
# 所以移进子目录不影响报告。
WORK_ROOT = paths.WORK_ROOT

# 一条 issue 最多配几帧。上限存在的理由是报告体积：单份报告已 250KB+，
# 每帧内嵌 base64 都要算进去。
MAX_FRAMES_PER_ISSUE = 4

# 「定位器选的帧」和「模型自选的帧」落在同一时刻时视为同一帧，避免重复抽同一画面。
_SAME_FRAME_TOLERANCE = 0.05


def _parse_time(value: Any) -> float | None:
    """'00:02.4' / '2.4s' / '00:01.0-00:01.8' / 2.4 → 秒（区间取中点）。"""
    if isinstance(value, (int, float)):
        return float(value)
    if not isinstance(value, str) or not value.strip():
        return None
    parts = value.split("-")

    def one(token: str) -> float | None:
        m = re.match(r"^(?:(\d+):)?(\d+(?:\.\d+)?)\s*s?$", token.strip())
        if not m:
            return None
        return float(m.group(1) or 0) * 60 + float(m.group(2))

    start = one(parts[0])
    if start is None:
        return None
    if len(parts) > 1:
        end = one(parts[1])
        if end is not None and end >= start:
            return round((start + end) / 2, 3)
    return round(start, 3)


def best_issue_time(issue: dict[str, Any]) -> float | None:
    """挑一条 issue 最具体的可用时刻。

    按区间宽度排序而不是取第一条：模型经常同时给「00:01.2-00:01.4」和「全片」两条
    证据，取中点会落在视频正中间——那里通常什么都没有。宽区间排在窄区间之后，
    纯文本描述里挖出的 t=X 排最后。
    """
    ranked: list[tuple[float, float]] = []
    for item in issue.get("evidence") or []:
        if not isinstance(item, dict):
            continue
        raw = item.get("time")
        parsed = _parse_time(raw)
        if parsed is not None:
            width = 0.0
            if isinstance(raw, str) and "-" in raw:
                left, right = raw.split("-", 1)
                start, end = _parse_time(left), _parse_time(right)
                width = (
                    max(0.0, float(end) - float(start))
                    if start is not None and end is not None
                    else 9999.0
                )
            ranked.append((width, parsed))
            continue
        # 「全片（如 t=1.5s、t=4.0s）」这类证据的 time 字段没法解析，
        # 但正文里举出的时刻仍然可用。
        detail = str(item.get("detail") or "")
        match = re.search(r"(?:t\s*=\s*)?(?:(\d+):)?(\d+(?:\.\d+)?)\s*s", detail, re.IGNORECASE)
        if match:
            ranked.append((10_000.0, float(match.group(1) or 0) * 60 + float(match.group(2))))
    return round(min(ranked)[1], 3) if ranked else None


def issue_frame_targets(
    issue: dict[str, Any], *, limit: int = MAX_FRAMES_PER_ISSUE
) -> list[dict[str, Any]]:
    """取模型指定的精确证据帧，没有则退回单个时刻。"""
    targets: list[dict[str, Any]] = []
    seen: set[float] = set()
    for row in issue.get("evidence_frames") or []:
        if not isinstance(row, dict):
            continue
        value = _parse_time(row.get("time"))
        if value is None:
            continue
        key = round(value, 3)
        if key in seen:
            continue
        seen.add(key)
        targets.append({
            "time": key,
            "role": str(row.get("role") or "representative"),
            "observation": str(row.get("observation") or "")[:600],
        })
        if len(targets) >= limit:
            break
    if not targets and (fallback := best_issue_time(issue)) is not None:
        targets.append({"time": fallback, "role": "representative", "observation": ""})
    return targets


def _valid_bbox(bbox: Any) -> list[float] | None:
    """归一化的 [x1,y1,x2,y2]（0~1，x1<x2、y1<y2），否则 None。"""
    if not isinstance(bbox, (list, tuple)) or len(bbox) != 4:
        return None
    try:
        x1, y1, x2, y2 = (float(v) for v in bbox)
    except (TypeError, ValueError):
        return None
    x1, x2 = sorted((max(0.0, min(1.0, x1)), max(0.0, min(1.0, x2))))
    y1, y2 = sorted((max(0.0, min(1.0, y1)), max(0.0, min(1.0, y2))))
    # 退化成一条线或一个点的框画出来只是噪声。
    if x2 - x1 < 0.01 or y2 - y1 < 0.01:
        return None
    return [round(x1, 4), round(y1, 4), round(x2, 4), round(y2, 4)]


def annotate_frame_bbox(src: Path, bbox: list[float], out: Path) -> Path:
    """在 ``src`` 上按归一化 ``bbox`` 画红框；ffmpeg 失败则退回无框原帧。"""
    x1, y1, x2, y2 = bbox
    draw = (
        f"drawbox=x=iw*{x1:.4f}:y=ih*{y1:.4f}:"
        f"w=iw*{(x2 - x1):.4f}:h=ih*{(y2 - y1):.4f}:"
        f"color=red@1.0:t=max(3\\,iw/240)"
    )
    completed = subprocess.run(
        ["ffmpeg", "-y", "-v", "error", "-i", str(src), "-vf", draw, "-q:v", "2", str(out)],
        capture_output=True,
        text=True,
        check=False,
    )
    if completed.returncode == 0 and out.exists():
        return out
    return src


def _index_zoom_frames(observer_bundle: dict[str, Any]) -> dict[float, str]:
    """已抽过的高清帧按时刻建索引，避免为同一时刻重复调 ffmpeg。"""
    zoom_frames_by_time: dict[float, str] = {}
    for fr in observer_bundle.get("_zoom_frames") or []:
        if isinstance(fr, dict) and fr.get("path") and fr.get("time") is not None:
            try:
                zoom_frames_by_time[round(float(fr["time"]), 2)] = str(fr["path"])
            except (TypeError, ValueError):
                continue
    return zoom_frames_by_time


def _index_grounding(grounding: Any) -> list[dict[str, Any]]:
    return [g for g in (grounding or []) if isinstance(g, dict) and g.get("ref_id")]


def _match_locator(
    *,
    ref_id: str | None,
    grounding: list[dict[str, Any]],
) -> dict[str, Any] | None:
    """取该 issue 的 bbox。只认 issue_id 完全相同的定位结果。

    不做任何近似匹配：把定位到**另一个**问题的框借过来，会让 reviewer 顺着红框
    去看一个不相关的位置，比没有框更糟。
    """
    for g in grounding:
        if not ref_id or g.get("ref_id") != ref_id:
            continue
        if g.get("locatable") is False:
            continue
        bbox = _valid_bbox(g.get("bbox"))
        if bbox is None:
            continue
        return {
            "time": _parse_time(g.get("time")),
            "bbox": bbox,
            "note": None,
            "stage": "grounding",
            "confirmed": None,
            "target": g.get("target"),
        }
    return None


def build_native_evidence(
    *,
    video_path: Path,
    slug: str,
    source: str,
    run_id: str,
    issues: list[dict[str, Any]],
    observer_bundle: dict[str, Any],
    grounding: list[dict[str, Any]] | None = None,
) -> list[dict[str, Any]]:
    """按最终 native8 issues 生成证据卡，全程以 ISSUE-ID 为键。"""
    zoom_frames_by_time = _index_zoom_frames(observer_bundle)
    grounding_index = _index_grounding(grounding)
    evidence_dir = WORK_ROOT / f"{slug}__{source}" / run_id / "evidence"
    evidence_dir.mkdir(parents=True, exist_ok=True)
    # 同一 run 重跑时先清掉上一轮的图：文件名按 issue_id 编号，
    # 上一轮多出来的那几张会以「幽灵证据」的形式留在目录里。
    for old in evidence_dir.glob("*.jpg"):
        old.unlink(missing_ok=True)

    cards: list[dict[str, Any]] = []
    frame_cache: dict[float, Path] = {}

    for issue in issues:
        if not isinstance(issue, dict) or not issue.get("issue_id"):
            continue
        issue_id = str(issue["issue_id"])
        targets = issue_frame_targets(issue)
        locator = _match_locator(ref_id=issue_id, grounding=grounding_index)
        loc_time = (locator or {}).get("time")
        # 定位器选的帧是唯一带框的那张，必须在场；模型自选帧里没有它就插到最前面。
        if loc_time is not None and not any(
            abs(row["time"] - loc_time) <= _SAME_FRAME_TOLERANCE for row in targets
        ):
            targets = [
                {"time": loc_time, "role": "problem", "observation": "定位器选定帧"},
                *targets,
            ][:MAX_FRAMES_PER_ISSUE]
        bbox = (locator or {}).get("bbox")
        occurrences = [row for row in (issue.get("evidence") or []) if isinstance(row, dict)]
        note = (locator or {}).get("note") or "；".join(
            str(row.get("detail") or "") for row in occurrences if row.get("detail")
        )

        rendered_frames: list[dict[str, Any]] = []
        for frame_index, target in enumerate(targets, 1):
            frame_time = float(target["time"])
            key_t = round(frame_time, 2)
            src_frame: Path | None = None
            if key_t in zoom_frames_by_time:
                candidate = Path(zoom_frames_by_time[key_t])
                if candidate.exists():
                    src_frame = candidate
            if src_frame is None:
                src_frame = frame_cache.get(key_t)
            if src_frame is None:
                extracted = extract_native_frames(video_path, evidence_dir, [frame_time])
                if extracted:
                    src_frame = Path(extracted[0]["path"])
                    frame_cache[key_t] = src_frame
            if src_frame is None:
                continue

            # extract_native_frames 按自己的规则命名，复制成按 issue_id 的稳定名，
            # 便于从报告反查文件。复制失败不致命，继续用原文件。
            stable = evidence_dir / f"{issue_id}_{frame_index:02d}_src.jpg"
            if src_frame != stable:
                try:
                    stable.write_bytes(src_frame.read_bytes())
                    src_frame = stable
                except OSError:
                    pass

            use_bbox = bool(
                bbox
                and loc_time is not None
                and abs(frame_time - float(loc_time)) <= _SAME_FRAME_TOLERANCE
            )
            boxed = evidence_dir / f"{issue_id}_{frame_index:02d}_boxed.jpg"
            image_path = annotate_frame_bbox(src_frame, bbox, boxed) if use_bbox else src_frame
            rendered_frames.append({
                **target,
                "image_path": str(image_path),
                # annotate_frame_bbox 失败时会退回原帧，此时并没有框可显示。
                "has_box": use_bbox and image_path == boxed,
                "bbox": bbox if use_bbox else None,
            })

        first_frame = rendered_frames[0] if rendered_frames else {}
        cards.append({
            "scope": "issue",
            "issue_id": issue_id,
            "ref_id": issue_id,
            "title": f"{issue_id} · {', '.join(issue.get('ref_ids') or []) or issue.get('source') or '问题'}",
            "time": occurrences[0].get("time") if occurrences else None,
            "occurrences": occurrences,
            "decision_summary": issue.get("decision_summary"),
            "counterevidence_checked": issue.get("counterevidence_checked") or [],
            "frames": rendered_frames,
            # 下面这几个是单帧时代的字段，报告页与批处理仍在读，保留为首帧的投影。
            "frame_time": first_frame.get("time"),
            "image_path": first_frame.get("image_path"),
            "has_box": any(row.get("has_box") for row in rendered_frames),
            "bbox": next((row.get("bbox") for row in rendered_frames if row.get("bbox")), None),
            "note": note,
            "target": (locator or {}).get("target"),
            "source_stage": (locator or {}).get("stage"),
            "confirmed": (locator or {}).get("confirmed"),
        })
    return cards
