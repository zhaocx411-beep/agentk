"""native8-v1 判定链里的两个纯模型角色：事实观察器与问题定位器。

- `GeminiObserver`（阶段 4）**只记录事实**：看到什么写什么，加上「这里看不清」的
  低置信区间。它不给结论、不给维度、不给严重度 —— 判定权全部在 4b 原生八维主判
  （`zoom_inspector`）与确定性聚合（`native8`）那一侧。旧的 v41 观察器会顺手输出
  `preliminary_issues`，等于让同一个模型既当证人又当法官，结论没法回溯到证据。
- `GeminiGroundingLocator`（定档之后）**只补坐标**：按最终 native8 `ISSUE-ID` 把已
  成立的问题框到画面上，不重新裁决事实，也不新增问题。

两个类都只依赖 client 的 `generate_json` 鸭子接口，所以三个 provider 通吃；
类型标注写 GeminiClient 只是历史原因，不代表真实依赖。
"""
from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from aigc_eval.judge.evidence import best_issue_time, issue_frame_targets
from aigc_eval.judge.evidence import _parse_time as _interval_midpoint
from aigc_eval.judge.integrity import extract_native_frames
from aigc_eval.judge.media_parts import (
    _image_parts,
    _keyframe_parts,
    reference_parts,
    select_references,
    video_part,
)
from aigc_eval.judge.rubric_prompts import (
    GROUNDING_SYSTEM,
    OBSERVER_SYSTEM,
    v2v_prompt_suffix,
)
from aigc_eval.judge.settings import (
    GROUNDING_MAX_FRAMES,
    GROUNDING_MAX_SIDE,
    SEND_WHOLE_VIDEO,
    VIDEO_MAX_MB,
)
from aigc_eval.judge.v2v_align import align_digest as v2v_align_digest
from aigc_eval.providers.media_io import encode_file
from aigc_eval.providers.model_call import JudgeClient


def normalize_observer_facts(result: dict[str, Any]) -> list[dict[str, Any]]:
    """给观察事实和疑点分配同一命名空间内的稳定审计 ID。"""
    facts: list[dict[str, Any]] = []
    index = 1
    for kind, key, text_key in (
        ("observation", "observations", "fact"),
        ("focus", "focus_intervals", "reason"),
    ):
        rows = [row for row in (result.get(key) or []) if isinstance(row, dict)]
        result[key] = rows
        for row in rows:
            fact_id = f"OBS-{index:03d}"
            index += 1
            row["fact_id"] = fact_id
            constraint_ids = list(dict.fromkeys(
                str(value).strip() for value in (row.get("constraint_ids") or [])
                if str(value).strip()
            ))
            contradicted_constraint_ids = list(dict.fromkeys(
                str(value).strip() for value in (row.get("contradicted_constraint_ids") or [])
                if str(value).strip()
            ))
            row["constraint_ids"] = constraint_ids
            row["contradicted_constraint_ids"] = contradicted_constraint_ids
            facts.append({
                "fact_id": fact_id,
                "kind": kind,
                "time": str(row.get("time") or row.get("frame_time") or ""),
                "fact": str(row.get(text_key) or "").strip(),
                "confidence": str(row.get("confidence") or ""),
                "tags": list(row.get("tags") or row.get("fact_types") or []),
                "subcriteria": list(row.get("subcriteria") or []),
                "constraint_ids": constraint_ids,
                "contradicted_constraint_ids": contradicted_constraint_ids,
            })
    result["observer_facts"] = facts
    return facts


class GeminiObserver:
    def __init__(self, client: JudgeClient):
        self.client = client

    def observe(
        self,
        *,
        prompt: str,
        mode: str,
        prompt_graph: dict[str, Any],
        metadata: dict[str, Any],
        integrity: dict[str, Any],
        frames: list[Path],
        frame_meta: list[dict[str, Any]] | None,
        motion: dict[str, Any] | None,
        shot_segments: list[dict[str, Any]] | None,
        contact_path: Path | None,
        references: list[dict[str, Any]],
        video_path: Path | None = None,
        source_video_path: Path | None = None,
        align_profile: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        # Prefer sending the whole video (native video understanding); fall back
        # to sampled key frames when the video is too large for one request.
        send_native = False
        if SEND_WHOLE_VIDEO and getattr(self.client, "supports_video", True) and video_path and video_path.exists():
            send_native = video_path.stat().st_size / 1e6 <= VIDEO_MAX_MB
        # V2V 的「改动事实 / 保留事实」是**成对**的观察，没有源视频对照就写不出来
        # （只能写成对生成视频的单方面描述，等于没判）。所以源视频必须进这一步，
        # 且只在能整段送视频时才成立 —— 退化到关键帧模式时拿不到源视频的逐时刻基线，
        # 此时观察者不做 V2V 对照，权威核验交给 4b；若 4b 也无法获得完整源视频则
        # 对应参考子判据必须为 not_observable。
        is_v2v = (
            mode == "V2V"
            and send_native
            and source_video_path is not None
            and Path(source_video_path).exists()
        )

        payload = {
            "mode": mode,
            "prompt": prompt,
            "metadata": metadata,
            "integrity_precheck": integrity,
            "constraint_summary": prompt_graph.get("constraints", []),
            "shot_plan": prompt_graph.get("shots", []),
            "shot_segments": shot_segments or [],
            "audio_requirements": prompt_graph.get("audio_requirements", {}),
            "reference_expectations": prompt_graph.get("reference_expectations", []),
            "input_note": (
                "已附上完整视频，请直接观看视频判断。视频为压缩采样，细节看不清时请在 focus_intervals "
                "里列出时间点，系统会抽原生分辨率大图给你复核。"
                if send_native
                else "已附上按运动量加密抽取的关键帧与缩略拼图（无法提供完整视频时的替代）。"
            ),
            "high_motion_windows": (motion or {}).get("windows", []),
            "resolution_note": (
                f"本视频源分辨率为 {metadata.get('width')}×{metadata.get('height')}"
                + (
                    "（属于较低分辨率，手指、纹理等细节本就偏软，这是分辨率所致，不是生成缺陷）。"
                    if int(metadata.get("width") or 0) * int(metadata.get("height") or 0) < 1200 * 1600
                    else "。"
                )
                + "判断结构类问题时请以此校准：只有结构本身错了（多指少指、解剖走向不可能、形态破碎到认不出）"
                "才算异常；仅仅是不够锐利、边缘发软、动态模糊，一律不算，看不准就记 not_observable。"
            ),
            "instruction": "请只输出可见事实 JSON，不要打分。",
        }
        parts: list[dict[str, Any]] = [{"text": json.dumps(payload, ensure_ascii=False, indent=2)}]
        if is_v2v:
            # 源视频排在生成视频之前：模型按出现顺序建立「原视频 → 结果」的对照方向，
            # 倒序会让它把源视频当成编辑后的那一个，改动方向直接反过来。
            parts.append({"text": "【源视频】被编辑的原视频，也是「不该改的部分」的基线："})
            parts.append(video_part(Path(source_video_path), detail=False))  # type: ignore[arg-type]
        if send_native:
            # 只送视频：整段视频已含全部帧，再塞低清缩略拼图既冗余、又比视频本身还贵
            # （实测：视频 300 tokens，拼图 1080 tokens）。
            parts.append({"text": "【生成视频】编辑后的结果：" if is_v2v else "完整视频："})
            parts.append(video_part(video_path, detail=False))  # type: ignore[arg-type]
        else:
            if contact_path and contact_path.exists():
                parts.append({"text": "全片缩略拼图："})
                parts.append({"inline_data": encode_file(contact_path)})
            if frame_meta:
                parts.extend(_keyframe_parts(frame_meta[:8]))
            else:
                parts.extend(_image_parts(frames[:8], "关键帧"))
        parts.extend(reference_parts(select_references(
            references,
            stage="observe",
            prompt=prompt,
            exclude_path=Path(source_video_path) if is_v2v else None,
        )))
        if is_v2v:
            digest = v2v_align_digest(align_profile)
            if digest:
                parts.append({"text": digest})
        result = self.client.generate_json(
            parts,
            system_instruction=OBSERVER_SYSTEM + v2v_prompt_suffix("observer", mode if is_v2v else "T2V"),
            stage="observer",
            title="4 · 看视频、记录画面",
        )
        result.setdefault("observations", [])
        result.setdefault("audio_facts", {})
        result.setdefault("reference_facts", [])
        result.setdefault("focus_intervals", [])
        normalize_observer_facts(result)
        result["_video_mode"] = "native" if send_native else "frames"
        return result


class GeminiGroundingLocator:
    """按最终 native8 ISSUE-ID 框选问题，只定位、不重新裁决事实。"""

    def __init__(self, client: JudgeClient):
        self.client = client

    @staticmethod
    def build_native_targets(native_report: dict[str, Any]) -> list[dict[str, Any]]:
        """Use final native8 ISSUE-IDs as the sole grounding identity."""
        dimensions_by_issue: dict[str, list[str]] = {}
        for dimension in native_report.get("rubric") or []:
            if not isinstance(dimension, dict):
                continue
            for issue_id in dimension.get("feedback") or []:
                dimensions_by_issue.setdefault(str(issue_id), []).append(
                    str(dimension.get("dimension_id") or "")
                )
        targets: list[dict[str, Any]] = []
        for issue in native_report.get("issues") or []:
            if not isinstance(issue, dict) or not issue.get("issue_id"):
                continue
            issue_id = str(issue["issue_id"])
            dimensions = list(dict.fromkeys(dimensions_by_issue.get(issue_id, [])))
            # 音频与纯全局属性没有可画的空间框，保留在 issue 文字证据中即可。
            if dimensions and all(dim == "audio_quality" for dim in dimensions):
                continue
            occurrences = [row for row in (issue.get("evidence") or []) if isinstance(row, dict)]
            evidence_frames = issue_frame_targets(issue)
            problem_frames = [row for row in evidence_frames if row.get("role") == "problem"]
            representative_frames = [row for row in evidence_frames if row.get("role") == "representative"]
            primary = (problem_frames or representative_frames or evidence_frames or [{}])[0]
            targets.append({
                "ref_id": issue_id,
                "title": f"{issue_id} · {', '.join(issue.get('ref_ids') or []) or issue.get('source') or '问题'}",
                "time": primary.get("time") if primary else best_issue_time(issue),
                "evidence_frames": evidence_frames,
                "evidence": "；".join(
                    str(row.get("detail") or "") for row in occurrences if row.get("detail")
                ),
                "dimension": ",".join(dimensions),
            })
        return targets

    @staticmethod
    def plan_times(targets: list[dict[str, Any]], cap: int) -> list[float]:
        primary_times: list[float] = []
        context_times: list[float] = []
        for target in targets:
            t = _interval_midpoint(target.get("time"))
            if t is not None and (rounded := round(t, 2)) not in primary_times:
                primary_times.append(rounded)
            for row in target.get("evidence_frames") or []:
                frame_time = _interval_midpoint(row.get("time"))
                if frame_time is None:
                    continue
                rounded = round(frame_time, 2)
                if rounded not in primary_times and rounded not in context_times:
                    context_times.append(rounded)
        # 先保证每个问题的 problem/representative 帧，再补 before/after 上下文。
        return (primary_times + context_times)[:cap]

    @staticmethod
    def validate_grounding(item: dict[str, Any], target: dict[str, Any]) -> dict[str, Any]:
        """Reject boxes placed on a normal before/after context frame."""
        frames = target.get("evidence_frames") or []
        problem_times = [
            _interval_midpoint(row.get("time"))
            for row in frames
            if isinstance(row, dict) and row.get("role") == "problem"
        ]
        representative_times = [
            _interval_midpoint(row.get("time"))
            for row in frames
            if isinstance(row, dict) and row.get("role") == "representative"
        ]
        allowed = [float(t) for t in (problem_times or representative_times) if t is not None]
        if not allowed and (fallback := _interval_midpoint(target.get("time"))) is not None:
            allowed = [fallback]
        selected = _interval_midpoint(item.get("time"))
        locatable = item.get("locatable") is not False
        if selected is None or not allowed or min(abs(selected - t) for t in allowed) > 0.08:
            locatable = False
        bbox = item.get("bbox") if locatable else None
        return {
            "ref_id": str(target["ref_id"]),
            "time": selected,
            "target": item.get("target"),
            "bbox": bbox,
            "locatable": locatable,
            "validation_note": None if locatable else "定位未落在模型声明的 problem/representative 帧，已禁止画框。",
        }

    def locate(
        self,
        *,
        video_path: Path,
        frame_dir: Path,
        targets: list[dict[str, Any]],
        prompt: str = "",
    ) -> dict[str, Any]:
        times = self.plan_times(targets, GROUNDING_MAX_FRAMES)
        if not times:
            return {"grounding": [], "frames": [], "note": "所有问题都没有可用时间戳，无法定位。"}
        frames = extract_native_frames(video_path, frame_dir, times, max_side=GROUNDING_MAX_SIDE)
        if not frames:
            return {"grounding": [], "frames": [], "note": "未能抽取定位帧。"}
        payload = {
            "prompt": prompt,
            "targets": [
                {
                    "ref_id": t["ref_id"],
                    "title": t.get("title"),
                    "time": t.get("time"),
                    "evidence_frames": t.get("evidence_frames"),
                    "evidence": t.get("evidence"),
                    "dimension": t.get("dimension"),
                }
                for t in targets
            ],
        }
        parts: list[dict[str, Any]] = [{"text": json.dumps(payload, ensure_ascii=False, indent=2)}]
        for frame in frames:
            frame_time = float(frame["time"])
            relations: list[str] = []
            for target in targets:
                for row in target.get("evidence_frames") or []:
                    row_time = _interval_midpoint(row.get("time"))
                    if row_time is not None and abs(row_time - frame_time) <= 0.08:
                        relations.append(
                            f"{target['ref_id']} role={row.get('role')} observation={row.get('observation') or ''}"
                        )
            label = "；".join(relations) or "上下文帧"
            parts.append({"text": f"原生帧 t={frame_time:.2f}s（{label}）："})
            parts.append({"inline_data": encode_file(frame["path"])})
        result = self.client.generate_json(
            parts,
            system_instruction=GROUNDING_SYSTEM,
            stage="grounding",
            title="· 问题画面定位",
        )
        known = {str(t["ref_id"]) for t in targets}
        cleaned: list[dict[str, Any]] = []
        seen: set[str] = set()
        for item in result.get("grounding") or []:
            if not isinstance(item, dict):
                continue
            ref_id = str(item.get("ref_id") or "")
            if ref_id not in known or ref_id in seen:
                continue
            seen.add(ref_id)
            target = next(t for t in targets if str(t["ref_id"]) == ref_id)
            cleaned.append(self.validate_grounding(item, target))
        return {
            "grounding": cleaned,
            "frames": [{"path": str(frame["path"]), "time": frame["time"]} for frame in frames],
            "note": result.get("note"),
        }
