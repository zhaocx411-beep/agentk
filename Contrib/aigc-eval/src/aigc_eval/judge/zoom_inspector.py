"""4b 原生八维主判：一次调用把整段视频与全片高清帧判成 native8-v1 事实草案。

native8-v1 下这里只剩**一条**代码路径。旧的分维度扫描器（一致性/指令/崩坏/画面/
文字各一个检测器、再按帧分块共约 37 次调用）连同它们的回退逻辑一并删除了 ——
那套设计是为绕开 Keye 的 32k 上下文，而 Gemini 每图固定 ~1080 token、上下文 1M，
一次装得下。更要紧的是回退本身有害：同一批数据里混进两种口径的结论，横向对比失效。
所以 `scan_unified` 失败就返回 None 或抛错，由批处理按 --retries 重试。

模块里两个环节共同构成 4b 的可审计契约：
1. `scan_unified` 拒绝掩盖模型的 checklist 漏项/乱序/重复/新增 —— 对不上就整次作废；
2. `validate_native8_audit` 把「结论有了但依据不全」的字段降级成 not_observable /
   unknown，交给 4c 增量取证，而不是让缺依据的结论直接进报告。
"""
from __future__ import annotations

import json
from copy import deepcopy
from pathlib import Path
from typing import Any

from aigc_eval.judge.evidence import _parse_time as _interval_midpoint
from aigc_eval.judge.integrity import (
    extract_native_frames,
    motion_profile,
    plan_sample_times,
)
from aigc_eval.judge.media_parts import (
    encode_image_capped,
    reference_kind,
    reference_parts,
    select_references,
    video_part,
)
from aigc_eval.judge.native8 import (
    SCHEMA_VERSION as NATIVE8_SCHEMA_VERSION,
    validate_native8_report,
)
from aigc_eval.judge.rubric_prompts import NATIVE8_SCAN_SYSTEM
from aigc_eval.judge.settings import (
    FOCUS_CONTEXT_SECONDS,
    FOCUS_INTERVAL_LIMIT,
    FOCUS_MIN_GLOBAL_FRAMES,
    UNIFIED_SCAN_MAX_SIDE,
)
from aigc_eval.judge.v2v_align import align_digest as v2v_align_digest
from aigc_eval.providers.media_io import encode_file
from aigc_eval.providers.model_call import JudgeClient


def canonical_instruction_checklist(constraints: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """把编译约束固化为4b唯一可填写的 checklist；模型不得增删或改写。"""
    checklist: list[dict[str, Any]] = []
    used_ids: set[str] = set()
    for index, constraint in enumerate(constraints, 1):
        if not isinstance(constraint, dict):
            continue
        constraint_id = str(
            constraint.get("id") or constraint.get("constraint_id") or f"C{index}"
        ).strip()
        if not constraint_id or constraint_id in used_ids:
            constraint_id = f"C{index}"
        used_ids.add(constraint_id)
        original_category = str(constraint.get("category") or "其他").strip()
        importance = str(
            constraint.get("requirement_importance") or "detail"
        ).lower()
        checklist.append({
            "constraint_id": constraint_id,
            "requirement": str(constraint.get("statement") or "").strip(),
            "constraint_category": original_category,
            "requirement_importance": "core" if importance == "core" else "detail",
        })
    return checklist


def validate_native8_audit(
    raw: dict[str, Any],
    observer_facts: list[dict[str, Any]],
) -> list[str]:
    """归一化 4b 审计依据；格式缺口降级复核，语义静默冲突才报错。"""
    errors: list[str] = []
    issues_by_id = {
        str(row.get("issue_id") or "").upper(): row
        for row in (raw.get("issues") or []) if isinstance(row, dict)
    }
    issue_ids = set(issues_by_id)
    fact_ids = {str(row.get("fact_id") or "") for row in observer_facts}
    focus_ids = [
        str(row.get("fact_id") or "") for row in observer_facts
        if row.get("kind") == "focus"
    ]
    returned_dispositions = [
        row for row in (raw.get("observer_fact_dispositions") or [])
        if isinstance(row, dict)
    ]
    returned_by_id = {
        str(row.get("observer_fact_id") or ""): row for row in returned_dispositions
    }
    dispositions: list[dict[str, Any]] = []
    for fact_id in focus_ids:
        row = deepcopy(returned_by_id.get(fact_id) or {})
        row["observer_fact_id"] = fact_id
        status = str(row.get("status") or "")
        frames = row.get("supporting_frames") or []
        basis = str(row.get("decision_basis") or "").strip()
        if status not in {"accepted", "rejected", "not_observable"}:
            status = "not_observable"
            basis = "4b 未逐条处置该观察疑点，程序转入复核"
        elif not basis:
            status = "not_observable"
            basis = "4b 未提供可审计处置依据，程序转入复核"
        elif status == "accepted":
            issue_id = str(row.get("issue_id") or "").upper()
            if issue_id not in issue_ids:
                status = "not_observable"
                basis = "4b 接受疑点但未关联有效 issue，程序转入复核"
            elif fact_id not in [
                str(value) for value in (issues_by_id[issue_id].get("observer_fact_ids") or [])
            ]:
                issues_by_id[issue_id].setdefault("observer_fact_ids", []).append(fact_id)
        elif status == "rejected" and not frames:
            status = "not_observable"
            basis = "4b 驳回疑点但未提供支持帧，程序转入复核"
        row.update({"status": status, "decision_basis": basis})
        if status != "accepted":
            row["issue_id"] = None
        dispositions.append(row)
    raw["observer_fact_dispositions"] = dispositions
    disposition_by_id = {row["observer_fact_id"]: row for row in dispositions}

    checklist_by_id = {
        str(item.get("constraint_id") or ""): item
        for item in (raw.get("instruction_checklist") or []) if isinstance(item, dict)
    }
    for item in checklist_by_id.values():
        if not isinstance(item, dict):
            continue
        constraint_id = str(item.get("constraint_id") or "")
        responded = item.get("responded")
        incomplete = False
        if not str(item.get("expected") or "").strip():
            item["expected"] = str(item.get("requirement") or "")
        for field in ("actual", "decision_basis"):
            if not str(item.get(field) or "").strip():
                incomplete = True
        if not isinstance(item.get("counterevidence_checked"), list):
            item["counterevidence_checked"] = []
        confidence = item.get("confidence")
        if not isinstance(confidence, (int, float)) or isinstance(confidence, bool) or not 0 <= float(confidence) <= 1:
            item["confidence"] = 0.0
            incomplete = True
        if responded is True and not str(item.get("evidence") or "").strip():
            incomplete = True
        if responded is False and str(item.get("issue_id") or "").upper() not in issue_ids:
            incomplete = True
        if incomplete:
            item.update({
                "responded": "unknown",
                "actual": str(item.get("actual") or "证据不足"),
                "decision_basis": str(
                    item.get("decision_basis") or "4b 未提供完整可审计依据，程序转入复核"
                ),
            })
        refs = [str(value) for value in (item.get("observer_fact_ids") or [])]
        if any(value not in fact_ids for value in refs):
            errors.append(f"{constraint_id}: unknown observer_fact_id")
        if item.get("conflict_with_observer") is True:
            resolution = item.get("conflict_resolution")
            if not isinstance(resolution, dict) or not str(
                resolution.get("rejection_reason") or ""
            ).strip():
                errors.append(f"{constraint_id}: observer conflict lacks resolution")
            elif (
                str(resolution.get("observer_fact_id") or "") not in refs
                or resolution.get("accepted") is not False
            ):
                errors.append(f"{constraint_id}: invalid observer conflict resolution")
            elif not (resolution.get("supporting_frames") or []):
                # 有解释但没有可核查帧时不允许 pass，进入既有 unknown → 4c 路径。
                item["responded"] = "unknown"

    for fact in observer_facts:
        if fact.get("kind") != "focus":
            continue
        fact_id = str(fact.get("fact_id") or "")
        disposition = disposition_by_id.get(fact_id, {})
        status = str(disposition.get("status") or "")
        related_ids = list(dict.fromkeys([
            *(fact.get("constraint_ids") or []),
            *(fact.get("contradicted_constraint_ids") or []),
        ]))
        contradicted_ids = {
            str(value) for value in (fact.get("contradicted_constraint_ids") or [])
        }
        for constraint_id in related_ids:
            item = checklist_by_id.get(str(constraint_id))
            if item is None:
                errors.append(f"{fact_id}: references unknown constraint {constraint_id}")
                continue
            refs = item.setdefault("observer_fact_ids", [])
            if fact_id not in [str(value) for value in refs]:
                refs.append(fact_id)
            if str(constraint_id) not in contradicted_ids:
                continue
            if status == "accepted":
                issue_id = str(disposition.get("issue_id") or "").upper()
                issue = issues_by_id.get(issue_id, {})
                issue_constraint = str((issue.get("facts") or {}).get("constraint_id") or "")
                if issue_constraint == str(constraint_id):
                    item.update({"responded": False, "issue_id": issue_id})
                else:
                    item["responded"] = "unknown"
            elif status == "not_observable" and item.get("responded") is True:
                item["responded"] = "unknown"
            elif status == "rejected" and item.get("responded") is True:
                item["conflict_with_observer"] = True
                item["conflict_resolution"] = {
                    "observer_fact_id": fact_id,
                    "accepted": False,
                    "rejection_reason": str(disposition.get("decision_basis") or ""),
                    "supporting_frames": list(disposition.get("supporting_frames") or []),
                }

    for dimension in raw.get("rubric") or []:
        if not isinstance(dimension, dict):
            continue
        for subcriterion in dimension.get("subcriteria") or []:
            if not isinstance(subcriterion, dict):
                continue
            key = str(subcriterion.get("key") or "")
            verdict = str(subcriterion.get("verdict") or "")
            basis = subcriterion.get("decision_basis")
            if not isinstance(basis, dict):
                basis = {}
                subcriterion["decision_basis"] = basis
            incomplete = False
            if not str(basis.get("actual") or "").strip() or not str(
                basis.get("evidence") or ""
            ).strip():
                incomplete = True
            if verdict == "violated":
                for field in ("expected", "why_this_dimension", "why_not_other_dimensions"):
                    if not str(basis.get(field) or "").strip():
                        incomplete = True
                if not (basis.get("evidence_frames") or []):
                    incomplete = True
                if not isinstance(basis.get("counterevidence_checked"), list):
                    incomplete = True
            if incomplete and verdict in {"pass", "violated"}:
                subcriterion["verdict"] = "not_observable"
                basis.update({
                    "actual": str(basis.get("actual") or "证据不足"),
                    "evidence": str(
                        basis.get("evidence") or "4b 未提供完整可审计依据，程序转入复核"
                    ),
                })
            refs = [str(value) for value in (subcriterion.get("observer_fact_ids") or [])]
            if any(value not in fact_ids for value in refs):
                errors.append(f"{key}: unknown observer_fact_id")
    return errors


class GeminiZoomInspector:
    """4b 原生八维主判：完整视频与全片关键帧只进入同一套协议。"""

    def __init__(self, client: JudgeClient):
        self.client = client

    def _scan_frames(
        self,
        video_path: Path,
        frame_dir: Path,
        metadata: dict[str, Any],
        *,
        base_fps: float,
        max_frames: int,
        dense_extra: int = 4,
        max_side: int | None = None,
        focus_intervals: list[dict[str, Any]] | None = None,
    ) -> list[dict[str, Any]]:
        duration = float(metadata.get("duration_seconds") or 0)
        if duration <= 0:
            return []
        try:
            samples = motion_profile(video_path)
        except Exception:  # noqa: BLE001
            samples = []
        plan = plan_sample_times(
            samples, duration, base_fps=base_fps, dense_extra=dense_extra,
            max_frames=max_frames, min_gap=0.25,
        )
        focus_plan = self.plan_focus_frames(focus_intervals, duration)
        plan = self.merge_focus_plan(plan, focus_plan, max_frames=max_frames)
        times = [item["time"] for item in plan]
        if not times:
            return []
        frames = extract_native_frames(video_path, frame_dir, times, max_side=max_side)
        result: list[dict[str, Any]] = []
        for frame in frames:
            row = {"path": str(frame["path"]), "time": frame["time"]}
            planned = min(plan, key=lambda item: abs(float(item["time"]) - float(frame["time"])))
            if abs(float(planned["time"]) - float(frame["time"])) <= 0.08 and planned.get("focus_role"):
                row.update({
                    "focus_role": planned["focus_role"],
                    "focus_types": planned.get("focus_types") or [],
                })
            result.append(row)
        return result

    @staticmethod
    def plan_focus_frames(
        focus_intervals: list[dict[str, Any]] | None,
        duration: float,
    ) -> list[dict[str, Any]]:
        """Convert observer uncertainty into neutral before/center/after frame requests."""
        centers: list[dict[str, Any]] = []
        contexts: list[dict[str, Any]] = []
        for row in focus_intervals or []:
            if not isinstance(row, dict) or len(centers) >= FOCUS_INTERVAL_LIMIT:
                continue
            center = _interval_midpoint(row.get("frame_time") or row.get("time"))
            if center is None or not 0 <= center < duration:
                continue
            if any(abs(center - float(item["time"])) < 0.2 for item in centers):
                continue
            fact_types = [
                str(value) for value in (row.get("fact_types") or [])
                if str(value) in {"picture", "motion", "text", "consistency"}
            ]
            centers.append({
                "time": round(center, 3), "focus_role": "center", "focus_types": fact_types,
            })
            for delta, role in ((-FOCUS_CONTEXT_SECONDS, "before"), (FOCUS_CONTEXT_SECONDS, "after")):
                value = round(min(max(0.0, center + delta), max(0.0, duration - 0.001)), 3)
                contexts.append({"time": value, "focus_role": role, "focus_types": fact_types})
        # 中心帧优先，预算不足时也不会只留下某个疑点的前态。
        return centers + contexts

    @staticmethod
    def merge_focus_plan(
        base_plan: list[dict[str, Any]],
        focus_plan: list[dict[str, Any]],
        *,
        max_frames: int,
    ) -> list[dict[str, Any]]:
        if max_frames <= 0:
            return []
        global_budget = min(FOCUS_MIN_GLOBAL_FRAMES, max_frames)
        focus_budget = max(0, max_frames - global_budget)
        selected_focus: list[dict[str, Any]] = []
        for row in focus_plan:
            if len(selected_focus) >= focus_budget:
                break
            if all(abs(float(row["time"]) - float(old["time"])) >= 0.12 for old in selected_focus):
                selected_focus.append(dict(row))
        remaining = [
            dict(row) for row in base_plan
            if all(abs(float(row["time"]) - float(focus["time"])) >= 0.12 for focus in selected_focus)
        ]
        base_budget = max_frames - len(selected_focus)
        if len(remaining) > base_budget and base_budget > 0:
            remaining = [
                remaining[round(index * (len(remaining) - 1) / (base_budget - 1))]
                for index in range(base_budget)
            ] if base_budget > 1 else [remaining[len(remaining) // 2]]
        merged = [*selected_focus, *remaining[:base_budget]]
        return sorted(merged, key=lambda row: float(row["time"]))

    @staticmethod
    def _shot_of(time: float, shot_segments: list[dict[str, Any]] | None) -> int:
        for index, segment in enumerate(shot_segments or [], start=1):
            start = float(segment.get("start", 0) or 0)
            end = float(segment.get("end", 0) or 0)
            if end > start and start <= time < end:
                return index
        return 1

    def scan_unified(
        self,
        *,
        video_path: Path,
        frame_dir: Path,
        metadata: dict[str, Any],
        max_frames: int,
        reference_assets: list[dict[str, Any]] | None,
        shot_segments: list[dict[str, Any]] | None,
        prompt: str = "",
        prompt_text: str = "",
        prompt_graph: dict[str, Any] | None = None,
        send_video: bool = True,
        mode: str = "T2V",
        source_video_path: Path | None = None,
        align_profile: dict[str, Any] | None = None,
        focus_intervals: list[dict[str, Any]] | None = None,
        observer_facts: list[dict[str, Any]] | None = None,
    ) -> dict[str, Any] | None:
        """一次调用完成原生八维主判，返回 native8-v1 事实草案。"""
        duration = float(metadata.get("duration_seconds") or 0)
        base_fps = 1.5 if duration <= 12 else 1.0
        frames = self._scan_frames(
            video_path, frame_dir, metadata, base_fps=base_fps, max_frames=max_frames,
            dense_extra=4, max_side=UNIFIED_SCAN_MAX_SIDE,
            focus_intervals=focus_intervals,
        )
        if not frames:
            return None

        image_refs = [
            r for r in select_references(reference_assets, stage="consistency", prompt=prompt)
            if reference_kind(r) == "image"
        ]
        has_ref = bool(image_refs)
        shot_count = len(shot_segments or [])
        is_v2v = mode == "V2V" and source_video_path is not None

        parts: list[dict[str, Any]] = []
        if image_refs:
            if len(image_refs) == 1:
                parts.append({"text": "【输入图】（视频首帧/主体应与它保持一致）："})
                parts.append({"inline_data": encode_image_capped(Path(image_refs[0]["path"]))})
            else:
                parts.append({"text": (
                    f"【参考图共 {len(image_refs)} 张】prompt 中以标签引用（如「参考 Image 4」）。"
                    "请按标签逐张核对对应主体，不要把不同标签的主体混为一谈："
                )})
                parts.extend(reference_parts(image_refs))

        # V2V 源视频必须排在生成视频**之前**：模型按出现顺序建立「原图 → 结果」的
        # 对照关系，倒序给会让它把源视频当成「编辑后」的那一个，判定直接反向。
        if is_v2v:
            parts.append({"text": (
                "【源视频】这是被编辑的**原视频**，也是判定的基线。下面还会给你生成视频，"
                "请始终按「源视频 → 生成视频」的方向做对照。"
            )})
            parts.append(video_part(source_video_path, detail=False))

        constraints = [
            c for c in ((prompt_graph or {}).get("constraints") or []) if isinstance(c, dict)
        ]
        instruction_contract = canonical_instruction_checklist(constraints)
        # Prompt 原文用于理解语义；编译约束是4b必须逐条填写的固定核对集合。
        if prompt_text:
            parts.append({"text": f"【prompt 原文】\n{prompt_text}"})
        if prompt_graph:
            if instruction_contract:
                lines = "\n".join(
                    f"- [{item['constraint_id']}] category={item['constraint_category']} "
                    f"importance={item['requirement_importance']} statement={item['requirement']}"
                    for item in instruction_contract
                )
                parts.append({"text": (
                    "【编译后的约束清单（4b 指令遵循的固定核对集合）】\n"
                    "必须按 ID、按原顺序逐条返回；不得合并、改写、新增或省略。\n"
                    f"{lines}"
                )})
            edit_ops = [o for o in (prompt_graph.get("edit_ops") or []) if isinstance(o, dict)]
            if is_v2v and edit_ops:
                lines = "\n".join(
                    f"- [{o.get('operation') or '?'}] 对象「{o.get('target') or ''}」"
                    f"→ 期望：{o.get('expectation') or ''}"
                    + (f"（范围：{o.get('scope')}）" if o.get("scope") else "")
                    for o in edit_ops[:20]
                )
                parts.append({"text": f"【拆解出的编辑操作清单（逐条核对是否达成）】\n{lines}"})

        audited_observer_facts = [
            row for row in (observer_facts or []) if isinstance(row, dict)
        ]
        if audited_observer_facts:
            parts.append({"text": (
                "【观察器可审计事实清单】\n"
                "这些是前一阶段的观察输入，不是最终结论。kind=focus 的疑点必须逐条接受、驳回或标记无法确认；"
                "不得静默忽略。\n"
                + json.dumps(audited_observer_facts, ensure_ascii=False, indent=2)
            )})

        # 客观对齐测量：程序算出的「哪里变了」，作为定位先验而非结论。
        if is_v2v:
            digest = v2v_align_digest(align_profile)
            if digest:
                parts.append({"text": digest})

        if send_video and video_path and video_path.exists():
            label = (
                "【生成视频（编辑后的结果，待质检）】用它做时序判断，并与上面的源视频逐时刻对照。"
                if is_v2v
                else "【整段视频】用它做时序判断：崩坏的前后突变、运镜是否真的发生、动态里的小字。"
            )
            parts.append({"text": label})
            parts.append(video_part(video_path, detail=True))

        shot_note = (
            f"共检测到 {shot_count} 个镜头。" if shot_count >= 2
            else "全片为单镜头，multishot_continuity 记 not_applicable。"
        )
        parts.append({"text": (
            f"【高清帧共 {len(frames)} 张】{shot_note}"
            f"请对**每一张**都写一行 per_frame，一张都不能漏。"
            + ("" if has_ref else "（无输入图，reference_input_fidelity 记 not_applicable。）")
        )})
        if any(frame.get("focus_role") for frame in frames):
            neutral_focus = [
                {
                    "time": frame["time"],
                    "role": frame.get("focus_role"),
                    "fact_types": frame.get("focus_types") or [],
                }
                for frame in frames if frame.get("focus_role")
            ]
            parts.append({"text": (
                "【观察器低置信度时刻的补充帧】以下时刻只是要求重新观察的取证调度信号，"
                "不是问题事实、不得据此倾向 violated。请根据像素和完整视频独立判定：\n"
                + json.dumps(neutral_focus, ensure_ascii=False)
            )})
        for frame in frames:
            t = float(frame["time"])
            shot = f" · 镜头#{self._shot_of(t, shot_segments)}" if shot_segments else ""
            focus = f" · 疑点复取/{frame.get('focus_role')}" if frame.get("focus_role") else ""
            parts.append({"text": f"高清帧 t={t:.2f}s{shot}{focus}："})
            parts.append({"inline_data": encode_file(Path(frame["path"]))})

        try:
            raw = self.client.generate_json(
                parts,
                system_instruction=NATIVE8_SCAN_SYSTEM,
                stage="native8_scan",
                title="4b · 原生八维主判", temperature=0.3,
                max_output_tokens=65536,
            )
        except (TypeError, AttributeError):
            # 这两类是「客户端接口不对」而不是「模型没答好」。吞掉它们会让签名不匹配
            # 这种纯代码缺陷伪装成 4b 无结果，排查时只能看到一句「没有返回有效 JSON」，
            # 而真正的原因要跑几次真实调用才浮出来。让它原样抛，立刻指向出错的那一行。
            raise
        except Exception:  # noqa: BLE001 - 模型侧失败：由调用方报错并交给 batch 重试
            return None
        if not isinstance(raw, dict):
            return None

        frame_refs = [
            {key: f[key] for key in ("path", "time", "focus_role", "focus_types") if key in f}
            for f in frames
        ]
        checklist_raw = raw.get("instruction_checklist") or []
        returned_ids = [
            str(item.get("constraint_id") or "")
            for item in checklist_raw if isinstance(item, dict)
        ]
        expected_ids = [item["constraint_id"] for item in instruction_contract]
        # 不能在程序归一化时掩盖模型漏项、乱序、重复或新增约束；让本次 4b 失败，
        # 由既有批处理重试机制重新执行，确保每条编译约束确实经过检查。
        if returned_ids != expected_ids:
            return None
        by_id = {
            str(item.get("constraint_id")): item
            for item in checklist_raw if isinstance(item, dict) and item.get("constraint_id")
        }
        raw["instruction_checklist"] = [
            {
                **contract,
                "responded": (
                    by_id.get(contract["constraint_id"], {}).get("responded")
                    if by_id.get(contract["constraint_id"], {}).get("responded") in {True, False, "unknown"}
                    else "unknown"
                ),
                "evidence": str(by_id.get(contract["constraint_id"], {}).get("evidence") or "")[:600],
                "observer_fact_ids": list(dict.fromkeys(
                    str(value) for value in (
                        by_id.get(contract["constraint_id"], {}).get("observer_fact_ids") or []
                    ) if str(value)
                )),
                "expected": str(by_id.get(contract["constraint_id"], {}).get("expected") or "")[:600],
                "actual": str(by_id.get(contract["constraint_id"], {}).get("actual") or "")[:600],
                "decision_basis": str(
                    by_id.get(contract["constraint_id"], {}).get("decision_basis") or ""
                )[:1000],
                "counterevidence_checked": list(
                    by_id.get(contract["constraint_id"], {}).get("counterevidence_checked") or []
                ),
                "conflict_with_observer": (
                    by_id.get(contract["constraint_id"], {}).get("conflict_with_observer") is True
                ),
                "confidence": by_id.get(contract["constraint_id"], {}).get("confidence"),
                "issue_id": by_id.get(contract["constraint_id"], {}).get("issue_id"),
                "conflict_resolution": deepcopy(
                    by_id.get(contract["constraint_id"], {}).get("conflict_resolution")
                ),
            }
            for contract in instruction_contract
        ]
        raw["per_frame"] = [r for r in (raw.get("per_frame") or []) if isinstance(r, dict)]
        raw["_frames"] = frame_refs
        raw["schema_version"] = NATIVE8_SCHEMA_VERSION
        audit_errors = validate_native8_audit(raw, audited_observer_facts)
        if audit_errors:
            raise ValueError("4b audit contract failed: " + "; ".join(audit_errors))
        if validate_native8_report(raw):
            return None
        return raw
