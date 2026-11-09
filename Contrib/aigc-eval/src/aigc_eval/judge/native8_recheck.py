"""4c 增量取证的候选筛选与事实合并，外加指令 checklist 的程序化固化。

这一层的全部设计都围绕一条分工：**4b 是唯一的事实草案作者，4c 只能做增量**。
具体落成三条不可破坏的不变量：

1. **4c 无权整维覆盖**。它提交的是「候选疑点的确证/否证」，而不是一份新的八维结论。
   `apply_native_incremental_recheck` 因此只认候选清单里出现过的
   `(dimension_id, key)`；不在候选里的确证一律进 `ignored`，绝不新开子判据。
   音频同理：4c 复听只能把异常**确认**上去，`pass` / `not_observable` 不回写
   —— 「这次没听见」不能推翻 4b「听见了」。
2. **checklist 对 4c 不可写**。编译约束的响应情况与 core/detail 重要度是**观看前**
   就由编译器定死的事实源，`enforce_native_instruction_checklist` 跑在所有专项之后，
   按 constraint_id 强行覆盖模型的挂载结果。4c 想确证一条指令违背，必须报出一个
   候选允许的 constraint_id，重要度仍从约束表取，模型自报的值被丢弃。
3. **一条约束只能绑一条 issue**。native8 的校验器会因「一约束多 issue」直接判报告
   不合规，因为定档按 issue 聚合，拆成两条等于把同一处未响应扣两次（瑕疵攒够三条
   还会升级成恶劣错误）。`collapse_instruction_constraint_issues` 在写盘前做这次收敛，
   合并证据后让多余 issue 从指令维度解绑，而不是让整个 case 因校验失败重试。

候选本身**不是问题结论**：`not_observable`、观察器的 focus 区间、checklist 的
`unknown` 都只表示「没看清」。4b 已经明确的 violated / pass 刻意不进复核 ——
复核一切会让成本翻倍，换来的只是对已定论结果的反复确认。
"""

from __future__ import annotations

import json
import re
import subprocess
from copy import deepcopy
from pathlib import Path
from typing import Any

from aigc_eval.judge.v2v_align import align_issues as v2v_align_issues


# 音频三个子判据是 4c 复听的全部作用域。写成常量而不是从 DIMENSIONS 反查，
# 是因为复听提示词里的字段名与这里必须逐字一致，放在一处才好对齐。
AUDIO_RECHECK_KEYS = (
    "audio_signal_quality", "audio_av_sync", "audio_temporal_consistency",
)

# 编译约束的类别 → 指令遵循子判据。中英文双写是因为编译器的 category 既可能来自
# 模型（中文）也可能来自启发式规则（英文）。查不到的类别落到动作/关系，
# 那是覆盖面最广的一档，不至于把约束丢掉。
_INSTRUCTION_CATEGORY_TO_KEY = {
    "主体": "instruction_subject_attributes", "subject": "instruction_subject_attributes",
    "动作": "instruction_action_relation", "空间": "instruction_action_relation",
    "时序": "instruction_action_relation", "action": "instruction_action_relation",
    "运镜": "instruction_camera", "camera": "instruction_camera",
    "风格": "instruction_style_environment", "环境": "instruction_style_environment",
    "environment": "instruction_style_environment", "多镜头": "instruction_multishot",
    "文字": "instruction_requested_text_audio", "音频": "instruction_requested_text_audio",
    "台词": "instruction_requested_text_audio", "音效": "instruction_requested_text_audio",
    "音乐": "instruction_requested_text_audio", "text": "instruction_requested_text_audio",
    "audio": "instruction_requested_text_audio", "编辑": "instruction_v2v_edit",
    "源保真": "instruction_v2v_edit", "edit": "instruction_v2v_edit",
}

# issue 择优顺序：专项取证 > 主判 > 程序推断。合并重复 issue 时保留排在前面的那条，
# 它的证据链最完整。未知来源排最后。
_SOURCE_PRIORITY = {"specialist": 0, "unified": 1, "programmatic": 2}

# 动态质量的 motion_type 白名单。不在其中的值无法定档，会被按子判据补一个默认值。
_MOTION_TYPES = {
    "physics_violation", "structural_break", "temporal_disappearance",
    "uncoordinated", "camera_jitter", "discontinuous_cut",
}


def audio_signal_profile(video_path: Path) -> dict[str, Any]:
    """用 ffmpeg 提取客观静音和响度线索；只作复核证据，不直接判质量好坏。"""
    profile: dict[str, Any] = {"silence_spans": []}
    try:
        silence = subprocess.run(
            [
                "ffmpeg", "-hide_banner", "-nostats", "-i", str(video_path),
                "-af", "silencedetect=noise=-45dB:d=0.15", "-f", "null", "-",
            ],
            capture_output=True, text=True, check=False,
        ).stderr
        starts = [float(x) for x in re.findall(r"silence_start:\s*([0-9.]+)", silence)]
        ends = [float(x) for x in re.findall(r"silence_end:\s*([0-9.]+)", silence)]
        profile["silence_spans"] = [
            {"start": round(start, 3), "end": round(end, 3), "duration": round(end - start, 3)}
            for start, end in zip(starts, ends) if end >= start
        ][:20]
        volume = subprocess.run(
            [
                "ffmpeg", "-hide_banner", "-nostats", "-i", str(video_path),
                "-af", "volumedetect", "-f", "null", "-",
            ],
            capture_output=True, text=True, check=False,
        ).stderr
        for key in ("mean_volume", "max_volume"):
            match = re.search(rf"{key}:\s*(-?(?:inf|[0-9.]+))\s*dB", volume)
            if match:
                profile[key] = match.group(1)
        stats = subprocess.run(
            [
                "ffmpeg", "-hide_banner", "-nostats", "-i", str(video_path),
                "-af", "astats=metadata=0:reset=0", "-f", "null", "-",
            ],
            capture_output=True, text=True, check=False,
        ).stderr
        # astats 每个声道各打印一组，取最后一组（Overall）作为整轨口径。
        for output_key, label in (
            ("peak_level_db", "Peak level dB"),
            ("rms_level_db", "RMS level dB"),
            ("rms_peak_db", "RMS peak dB"),
            ("noise_floor_db", "Noise floor dB"),
            ("crest_factor", "Crest factor"),
        ):
            matches = re.findall(rf"{re.escape(label)}:\s*(-?(?:inf|[0-9.]+))", stats)
            if matches:
                profile[output_key] = matches[-1]
    except (OSError, ValueError):
        # 没装 ffmpeg 或输出无法解析时标记缺失而不是返回空 profile：
        # 「测不到」与「测到没有静音」对复核结论的含义完全不同。
        profile["unavailable"] = True
    return profile


# 模型自报"这次没真的听到音频"时写进证据的原因。单独成常量是因为报告页与测试都要认它。
AUDIO_NOT_HEARD_REASON = (
    "模型自报本次未接收到可听的音频信号（可能被平台在服务端转成了文字），"
    "因此三项音频判据均无听觉依据，一律记未核实。"
)


def normalize_audio_recheck(raw: Any) -> dict[str, dict[str, Any]]:
    """收紧 4c 原生音频事实；三档仅由后续 duration_seconds 聚合。

    模型给的 severity/score 一概不接，只留定档与证据校验要用的字段。violated 但
    缺时刻或缺时长的，直接降为 `not_observable` —— 没有时长就没法分档，硬收下
    等于凭空扣分。

    **``can_hear_audio`` 为假时整体退成 not_observable。** 这一条防的是平台的静默
    降级：2026-09-01 放开音频入口后，主模型上约一半的调用会在服务端把音频转成文字再
    交给模型——接口通、台词对、还带时间戳，只有音色/语调/音质/配乐拿不到，报告里
    看不出任何异常。此时模型往往仍会填出一份完整结论，而那份结论没有听觉依据。
    宁可记未核实：音频阶段现在固定走 audio_model（实测稳定），真触发这条说明平台
    行为又变了，而"未核实"会让人去查，一份编出来的 pass 不会。
    """
    if not isinstance(raw, dict):
        return {}
    # 只认显式的 False，不按真假值判断：字段缺失是"模型没答"，不是"没听到"。把缺失
    # 也当没听到，会在模型偶尔漏字段时整维清空——那是用一个更常见的故障替掉一个更罕见的。
    if raw.get("can_hear_audio") is False:
        return {
            key: {
                "verdict": "not_observable",
                "confidence": "",
                "time": "",
                "duration_seconds": None,
                "evidence": AUDIO_NOT_HEARD_REASON,
            }
            for key in AUDIO_RECHECK_KEYS
        }
    normalized: dict[str, dict[str, Any]] = {}
    for key in AUDIO_RECHECK_KEYS:
        item = raw.get(key)
        if not isinstance(item, dict):
            continue
        verdict = str(item.get("verdict") or "")
        if verdict not in {"pass", "violated", "not_observable", "not_applicable"}:
            continue
        raw_duration = item.get("duration_seconds")
        duration: float | None = None
        # bool 是 int 的子类，True 会被当成 1.0 秒混进时长。
        if isinstance(raw_duration, (int, float)) and not isinstance(raw_duration, bool):
            duration = max(0.0, float(raw_duration))
        confidence = str(item.get("confidence") or "").lower()
        if verdict == "violated" and (
            confidence == "low"
            or (confidence and (duration is None or not str(item.get("time") or "").strip()))
        ):
            verdict = "not_observable"
        normalized[key] = {
            "verdict": verdict,
            "confidence": confidence,
            "time": str(item.get("time") or "").strip(),
            "duration_seconds": duration,
            "evidence": str(item.get("evidence") or "").strip()[:600],
        }
    return normalized


def merge_audio_confirmation(
    first: dict[str, dict[str, Any]], confirm: Any
) -> dict[str, dict[str, Any]]:
    """两次音频结论仅在一致时确认；冲突保守记 not_observable。

    音频是唯一靠单次采样极易误报的维度（编码噪底、静音段都能被听成缺陷），
    所以要求两次独立复听互相印证。不一致时不取任何一方，退回未决。
    """
    second = normalize_audio_recheck(confirm)
    merged = {key: dict(value) for key, value in first.items()}
    for key, item in first.items():
        if item.get("verdict") != "violated":
            continue
        other = second.get(key)
        if not other or other.get("verdict") != "violated":
            merged[key] = {
                **item,
                "verdict": "not_observable",
                "evidence": "两次独立复听未形成一致结论",
            }
            continue
        merged[key] = other
    return merged


def _native_subcriterion(
    verdict_bundle: dict[str, Any], dimension_id: str, key: str
) -> dict[str, Any] | None:
    """定位草案里的某条子判据，返回可原地改写的引用（不存在则 None）。"""
    for dimension in verdict_bundle.get("rubric") or []:
        if not isinstance(dimension, dict) or dimension.get("dimension_id") != dimension_id:
            continue
        for subcriterion in dimension.get("subcriteria") or []:
            if isinstance(subcriterion, dict) and subcriterion.get("key") == key:
                return subcriterion
    return None


def _merge_unique(primary: dict[str, Any], other: dict[str, Any], keys: tuple[str, ...]) -> None:
    """按 JSON 规范形按序去重地把 other 的列表字段并进 primary。

    用序列化后的字符串做去重键，是因为证据行是嵌套 dict、不可哈希，而合并重复
    issue 时同一条证据常以完全相同的结构出现两次。
    """
    for key in keys:
        values = [*(primary.get(key) or []), *(other.get(key) or [])]
        unique: list[Any] = []
        seen: set[str] = set()
        for value in values:
            marker = json.dumps(value, ensure_ascii=False, sort_keys=True)
            if marker not in seen:
                seen.add(marker)
                unique.append(deepcopy(value))
        if unique:
            primary[key] = unique


def apply_native_audio_increment(
    verdict_bundle: dict[str, Any], audio_recheck: Any
) -> dict[str, Any]:
    """4c 音频只增量确认异常；pass/not_observable 不得覆盖 4b。

    原地改写 `verdict_bundle`，返回审计字典（确认了哪些 issue、忽略了哪些子判据）。
    """
    recheck = normalize_audio_recheck(audio_recheck)
    issues = verdict_bundle.setdefault("issues", [])
    issue_by_id = {
        str(item.get("issue_id")): item
        for item in issues if isinstance(item, dict) and item.get("issue_id")
    }
    audit: dict[str, Any] = {"confirmed_issue_ids": [], "ignored_keys": []}
    for native_key in AUDIO_RECHECK_KEYS:
        finding = recheck.get(native_key)
        subcriterion = _native_subcriterion(verdict_bundle, "audio_quality", native_key)
        if not finding or subcriterion is None:
            continue
        verdict = str(finding.get("verdict") or "not_observable")
        if verdict != "violated":
            audit["ignored_keys"].append(native_key)
            continue
        referenced = [
            issue_by_id[str(issue_id)]
            for issue_id in (subcriterion.get("issue_ids") or [])
            if str(issue_id) in issue_by_id
        ]
        if referenced:
            # 4b 已经挂了 issue：复听只补强时长与置信，取两者中更长的那个时长，
            # 因为定档按「异常持续多久」分档，缩短它相当于替模型减轻结论。
            issue = referenced[0]
            issue_id = str(issue["issue_id"])
            facts = issue.setdefault("facts", {})
            facts["duration_seconds"] = max(
                float(facts.get("duration_seconds") or 0.0),
                max(0.0, float(finding.get("duration_seconds") or 0.0)),
            )
            facts["confidence"] = str(finding.get("confidence") or facts.get("confidence") or "medium")
            evidence = issue.setdefault("evidence", [])
            row = {
                "time": str(finding.get("time") or ""),
                "detail": str(finding.get("evidence") or "音频专项确认异常"),
            }
            if row not in evidence:
                evidence.append(row)
            issue["source"] = "specialist"
        else:
            index = len(issues) + 1
            issue_id = f"ISSUE-AUDIO-{index:03d}"
            while issue_id in issue_by_id:
                index += 1
                issue_id = f"ISSUE-AUDIO-{index:03d}"
            issue = {
                "issue_id": issue_id,
                "facts": {
                    "duration_seconds": max(0.0, float(finding.get("duration_seconds") or 0.0)),
                    "confidence": str(finding.get("confidence") or "medium"),
                },
                "evidence": [{
                    "time": str(finding.get("time") or ""),
                    "detail": str(finding.get("evidence") or "音频专项确认异常"),
                }],
                "source": "specialist",
            }
            issues.append(issue)
            issue_by_id[issue_id] = issue
        subcriterion["verdict"] = "violated"
        subcriterion["issue_ids"] = list(dict.fromkeys([
            *(str(value) for value in (subcriterion.get("issue_ids") or [])), issue_id,
        ]))
        audit["confirmed_issue_ids"].append(issue_id)
    return audit


def enforce_native_instruction_checklist(
    verdict_bundle: dict[str, Any],
    native_scan: dict[str, Any],
    constraints: list[dict[str, Any]],
) -> None:
    """在所有专项之后按 constraint_id 固化未响应约束，并收敛重复 issue。

    必须跑在 4c 之后：checklist 是观看前定死的事实源，4c 无权改写它，也无权改写
    core/detail 重要度 —— 重要度来自编译器对 prompt 的解析，让模型自报会让
    「细节没做到」被说成「核心没做到」。
    """
    by_id = {
        str(item.get("id") or item.get("constraint_id")): item
        for item in constraints if isinstance(item, dict)
    }
    issues = verdict_bundle.setdefault("issues", [])
    issue_by_id = {
        str(item.get("issue_id")): item
        for item in issues if isinstance(item, dict) and item.get("issue_id")
    }
    existing_ids = set(issue_by_id)
    instruction_subcriteria = [
        subcriterion
        for dimension in verdict_bundle.get("rubric") or []
        if isinstance(dimension, dict)
        and dimension.get("dimension_id") == "instruction_following"
        for subcriterion in dimension.get("subcriteria") or []
        if isinstance(subcriterion, dict)
    ]
    instruction_issue_ids = {
        str(issue_id)
        for subcriterion in instruction_subcriteria
        for issue_id in (subcriterion.get("issue_ids") or [])
    }
    pending: list[tuple[dict[str, Any], str, dict[str, Any]]] = []
    for item in native_scan.get("instruction_checklist") or []:
        # 只处理明确的 responded=False。`unknown` 属于 4c 的候选，不在这里定罪。
        if not isinstance(item, dict) or item.get("responded") is not False:
            continue
        constraint_id = str(item.get("constraint_id") or "")
        constraint = by_id.get(constraint_id, {})
        category = str(constraint.get("category") or item.get("constraint_category") or "")
        native_key = _INSTRUCTION_CATEGORY_TO_KEY.get(category, "instruction_action_relation")
        pending.append((item, native_key, constraint))
    pending_count_by_key: dict[str, int] = {}
    for _, native_key, _ in pending:
        pending_count_by_key[native_key] = pending_count_by_key.get(native_key, 0) + 1

    desired_keys_by_issue: dict[str, set[str]] = {}
    duplicate_ids: set[str] = set()
    for item, native_key, constraint in pending:
        constraint_id = str(item.get("constraint_id") or "")
        subcriterion = _native_subcriterion(verdict_bundle, "instruction_following", native_key)
        if subcriterion is None:
            continue
        importance = str(
            constraint.get("requirement_importance")
            or item.get("requirement_importance") or "detail"
        ).lower()
        normalized_importance = "core" if importance in {"core", "hard"} else "detail"

        referenced = [
            issue_by_id[issue_id]
            for issue_id in instruction_issue_ids
            if issue_id in issue_by_id
        ]
        direct = [
            issue for issue in referenced
            if str((issue.get("facts") or {}).get("constraint_id") or "") == constraint_id
            or constraint_id in [str(value) for value in (issue.get("ref_ids") or [])]
        ]
        direct.sort(key=lambda issue: (
            _SOURCE_PRIORITY.get(str(issue.get("source") or ""), 3),
            -len(issue.get("evidence") or []),
            str(issue.get("issue_id") or ""),
        ))
        reusable: dict[str, Any] | None = direct[0] if direct else None
        # 同一 constraint_id 已经由多条 issue 表示时，保留证据更强的 specialist，
        # 合并审计信息并让其余 issue 从指令维度解绑。
        if reusable is not None:
            for duplicate in direct[1:]:
                duplicate_id = str(duplicate.get("issue_id") or "")
                if duplicate_id:
                    duplicate_ids.add(duplicate_id)
                _merge_unique(
                    reusable, duplicate,
                    ("evidence", "counterevidence_checked", "evidence_frames"),
                )
                reusable["ref_ids"] = list(dict.fromkeys([
                    *(str(value) for value in (reusable.get("ref_ids") or [])),
                    *(str(value) for value in (duplicate.get("ref_ids") or [])),
                ]))
        # 只剩一条未响应约束、且该子判据下恰好挂着一条没绑任何约束的 issue 时，
        # 认定它说的就是这条约束。歧义（多于一条）时宁可新建，不猜。
        if reusable is None and pending_count_by_key.get(native_key) == 1:
            local_referenced = [
                issue_by_id[str(issue_id)]
                for issue_id in (subcriterion.get("issue_ids") or [])
                if str(issue_id) in issue_by_id
            ]
            unbound = [
                issue for issue in local_referenced
                if not str((issue.get("facts") or {}).get("constraint_id") or "")
                and not (issue.get("ref_ids") or [])
                and (issue.get("facts") or {}).get("requirement_importance") in {"core", "detail"}
            ]
            if len(unbound) == 1:
                reusable = unbound[0]

        if reusable is not None:
            issue_id = str(reusable["issue_id"])
            facts = reusable.setdefault("facts", {})
            if not str(facts.get("constraint_id") or ""):
                facts["constraint_id"] = constraint_id
            # 重要度只允许往严的方向覆盖：约束表说 core 就是 core，
            # 模型给的非法值也一并纠正。
            if normalized_importance == "core" or facts.get("requirement_importance") not in {"core", "detail"}:
                facts["requirement_importance"] = normalized_importance
            reusable["ref_ids"] = list(dict.fromkeys([
                *(reusable.get("ref_ids") or []), constraint_id,
            ]))
            checklist_detail = str(item.get("evidence") or "").strip()
            if checklist_detail:
                evidence = reusable.setdefault("evidence", [])
                if not any(
                    str(row.get("detail") or "").strip() == checklist_detail
                    for row in evidence if isinstance(row, dict)
                ):
                    evidence.append({"time": "全片", "detail": checklist_detail})
        else:
            issue_id = f"ISSUE-REQ-{constraint_id or len(issues) + 1}"
        if issue_id not in existing_ids:
            created = {
                "issue_id": issue_id,
                "facts": {
                    "requirement_importance": normalized_importance,
                    "constraint_id": constraint_id,
                },
                "evidence": [{"time": "全片", "detail": str(item.get("evidence") or "编译约束未响应")}],
                "source": "programmatic",
                "ref_ids": [constraint_id],
            }
            issues.append(created)
            issue_by_id[issue_id] = created
            existing_ids.add(issue_id)
            instruction_issue_ids.add(issue_id)
        desired_keys_by_issue.setdefault(issue_id, set()).add(native_key)

    # 对已按 constraint_id 绑定的 issue，模型先前挂错的指令子判据不再保留；
    # 同一 issue 可因多个约束被绑定到多个正确子判据。
    rebound_ids = set(desired_keys_by_issue)
    for subcriterion in instruction_subcriteria:
        key = str(subcriterion.get("key") or "")
        kept = [
            str(issue_id) for issue_id in (subcriterion.get("issue_ids") or [])
            if str(issue_id) not in rebound_ids and str(issue_id) not in duplicate_ids
        ]
        bound = [
            issue_id for issue_id, desired_keys in desired_keys_by_issue.items()
            if key in desired_keys
        ]
        subcriterion["issue_ids"] = list(dict.fromkeys([*kept, *bound]))
        if bound:
            subcriterion["verdict"] = "violated"
        elif subcriterion.get("verdict") == "violated" and not kept:
            subcriterion["verdict"] = "pass"


def collapse_instruction_constraint_issues(verdict_bundle: dict[str, Any]) -> None:
    """同一编译约束在指令维度只保留一个 issue，避免随机拆分导致整 case 重试。

    `validate_native8_report` 会把「一条约束绑多条 issue」判为不合规（定档会重复
    扣分）。与其让整条 case 因校验失败重跑，不如在这里确定性地合并：保留证据最强的
    那条，其余合并证据后从指令子判据上解绑。
    """
    issues = [row for row in (verdict_bundle.get("issues") or []) if isinstance(row, dict)]
    by_id = {
        str(row.get("issue_id") or ""): row for row in issues if row.get("issue_id")
    }
    subcriteria = [
        sub
        for dimension in (verdict_bundle.get("rubric") or [])
        if isinstance(dimension, dict) and dimension.get("dimension_id") == "instruction_following"
        for sub in (dimension.get("subcriteria") or []) if isinstance(sub, dict)
    ]
    referenced_ids = {
        str(issue_id) for sub in subcriteria for issue_id in (sub.get("issue_ids") or [])
    }
    # 约束绑定既可能写在 facts.constraint_id 也可能写在 ref_ids，校验器两处都看，
    # 这里必须用同一套口径，否则收敛完仍会被判不合规。
    constraints_by_issue: dict[str, set[str]] = {}
    for issue_id in referenced_ids:
        issue = by_id.get(issue_id)
        if not issue:
            continue
        facts = issue.get("facts") or {}
        constraints_by_issue[issue_id] = {
            str(facts.get("constraint_id") or "").strip(),
            *(str(value).strip() for value in (issue.get("ref_ids") or [])),
        } - {""}
    issue_ids_by_constraint: dict[str, list[str]] = {}
    for issue_id, constraint_ids in constraints_by_issue.items():
        for constraint_id in constraint_ids:
            issue_ids_by_constraint.setdefault(constraint_id, []).append(issue_id)

    for constraint_id, issue_ids in issue_ids_by_constraint.items():
        if len(issue_ids) < 2:
            continue
        issue_ids.sort(key=lambda issue_id: (
            _SOURCE_PRIORITY.get(str(by_id[issue_id].get("source") or ""), 3),
            -len(by_id[issue_id].get("evidence") or []),
            issue_id,
        ))
        primary_id = issue_ids[0]
        primary = by_id[primary_id]
        primary.setdefault("ref_ids", [])
        if constraint_id not in [str(value) for value in primary["ref_ids"]]:
            primary["ref_ids"].append(constraint_id)
        for duplicate_id in issue_ids[1:]:
            duplicate = by_id[duplicate_id]
            _merge_unique(
                primary, duplicate,
                ("evidence", "counterevidence_checked", "evidence_frames", "observer_fact_ids"),
            )
            # 合并只允许把重要度抬高：拆分出来的任一条说这是核心要求，就按核心算。
            if (duplicate.get("facts") or {}).get("requirement_importance") == "core":
                primary.setdefault("facts", {})["requirement_importance"] = "core"

            remaining_constraints = constraints_by_issue.get(duplicate_id, set()) - {constraint_id}
            duplicate["ref_ids"] = [
                value for value in (duplicate.get("ref_ids") or [])
                if str(value) != constraint_id
            ]
            duplicate_facts = duplicate.setdefault("facts", {})
            if str(duplicate_facts.get("constraint_id") or "") == constraint_id:
                duplicate_facts["constraint_id"] = sorted(remaining_constraints)[0] if remaining_constraints else ""
            # 这条 issue 还绑着别的约束，说明它自己也是一条独立问题，保留挂载。
            if remaining_constraints:
                continue
            for subcriterion in subcriteria:
                ids = [str(value) for value in (subcriterion.get("issue_ids") or [])]
                if duplicate_id not in ids:
                    continue
                ids = [value for value in ids if value != duplicate_id]
                if primary_id not in ids:
                    ids.append(primary_id)
                subcriterion["issue_ids"] = ids


def apply_native_incremental_recheck(
    verdict_bundle: dict[str, Any],
    recheck: Any,
    candidates: list[dict[str, Any]],
    constraints: list[dict[str, Any]],
    mode: str | None = None,
) -> dict[str, Any]:
    """把 4c 增量证据精确应用到候选 issue；不允许整维覆盖。

    原地改写 `verdict_bundle`，返回审计字典。审计里的 `ignored` 是刻意保留的：
    4c 越界（报了不在候选里的子判据、编了不存在的 constraint_id）必须留痕，
    否则调不出「为什么这条证据没被采纳」。
    """
    audit: dict[str, Any] = {
        "confirmed_issue_ids": [], "rejected_candidate_issue_ids": [],
        "unresolved_candidates": [], "ignored": [],
    }
    if not isinstance(recheck, dict):
        return audit
    candidate_by_pair = {
        (str(item.get("dimension_id") or ""), str(item.get("key") or "")): item
        for item in candidates if isinstance(item, dict)
    }
    candidate_issue_ids = {
        str(issue_id)
        for item in candidate_by_pair.values()
        for issue_id in (item.get("issue_ids") or [])
        if str(issue_id)
    }
    constraint_by_id = {
        str(item.get("id") or item.get("constraint_id") or ""): item
        for item in constraints if isinstance(item, dict)
    }
    target_issues = verdict_bundle.setdefault("issues", [])
    occupied = {
        str(item.get("issue_id")): item
        for item in target_issues if isinstance(item, dict) and item.get("issue_id")
    }

    # 否证先行：4c 撤掉一条候选 issue 后，若子判据再无支撑，退回 not_observable
    # 而不是 pass —— 4b 确实看到了可疑现象，只是这次没证实。
    for value in recheck.get("rejected_candidate_issue_ids") or []:
        issue_id = str(value.get("issue_id") if isinstance(value, dict) else value or "")
        if issue_id not in candidate_issue_ids:
            audit["ignored"].append({"type": "rejection", "issue_id": issue_id})
            continue
        rejection_pairs = {
            pair for pair, candidate in candidate_by_pair.items()
            if issue_id in [str(item) for item in (candidate.get("issue_ids") or [])]
        }
        for dimension_id, key in rejection_pairs:
            subcriterion = _native_subcriterion(verdict_bundle, dimension_id, key)
            if subcriterion is None:
                continue
            ids = [str(item) for item in (subcriterion.get("issue_ids") or [])]
            subcriterion["issue_ids"] = [item for item in ids if item != issue_id]
            if subcriterion.get("verdict") == "violated" and not subcriterion["issue_ids"]:
                subcriterion["verdict"] = "not_observable"
        audit["rejected_candidate_issue_ids"].append(issue_id)

    for item in recheck.get("unresolved_candidates") or []:
        if not isinstance(item, dict):
            continue
        pair = (str(item.get("dimension_id") or ""), str(item.get("key") or ""))
        if pair in candidate_by_pair:
            audit["unresolved_candidates"].append(deepcopy(item))
        else:
            audit["ignored"].append({"type": "unresolved", "pair": list(pair)})

    for index, item in enumerate(recheck.get("confirmed_issues") or [], 1):
        if not isinstance(item, dict):
            continue
        dimension_id = str(item.get("dimension_id") or "")
        key = str(item.get("key") or "")
        pair = (dimension_id, key)
        candidate = candidate_by_pair.get(pair)
        subcriterion = _native_subcriterion(verdict_bundle, dimension_id, key)
        evidence = [
            deepcopy(row) for row in (item.get("evidence") or [])
            if isinstance(row, dict) and str(row.get("detail") or "").strip()
        ]
        if candidate is None or subcriterion is None or not evidence:
            audit["ignored"].append({"type": "confirmed", "pair": list(pair), "reason": "invalid_target_or_evidence"})
            continue
        old_id = str(item.get("issue_id") or f"RECHECK-{index:03d}")
        # 同一 issue 既被否证又被确证时以否证为准：自相矛盾的一轮取证不该产生扣分。
        if old_id in audit["rejected_candidate_issue_ids"]:
            audit["ignored"].append({
                "type": "confirmed", "pair": list(pair),
                "reason": "same_issue_was_rejected",
            })
            continue
        created_new = False
        if old_id in occupied and old_id in candidate_issue_ids:
            issue_id = old_id
            current = occupied[issue_id]
        else:
            # 通用 4c 没有 V2V 源视频，无法证明一个新发现是编辑新增/加重还是源片继承。
            # 已有 4b 候选仍可补证；没有候选 issue_id 的新问题只能保持 unresolved。
            if str(mode or "").upper() == "V2V":
                audit["ignored"].append({
                    "type": "confirmed", "pair": list(pair),
                    "reason": "v2v_requires_existing_candidate_issue",
                })
                continue
            issue_id = old_id if old_id.startswith("ISSUE-") and old_id not in occupied else f"ISSUE-4C-{index:03d}"
            suffix = index
            while issue_id in occupied:
                suffix += 1
                issue_id = f"ISSUE-4C-{suffix:03d}"
            current = {"issue_id": issue_id, "facts": {}, "evidence": [], "source": "specialist"}
            target_issues.append(current)
            occupied[issue_id] = current
            created_new = True

        original_facts = deepcopy(current.get("facts") or {})
        merged_facts = deepcopy(original_facts)
        if isinstance(item.get("facts"), dict):
            merged_facts.update(deepcopy(item["facts"]))
        if "source_relation" in original_facts:
            # 4c 未携带源视频，无权把 4b 的“源片继承”关系改成编辑新增。
            merged_facts["source_relation"] = original_facts["source_relation"]
        if dimension_id == "instruction_following":
            # 指令违背只认候选清单允许的 constraint_id，重要度一律回读约束表：
            # checklist 是观看前定死的事实源，4c 不得改写。
            constraint_id = str(merged_facts.get("constraint_id") or "")
            allowed_ids = {str(value) for value in (candidate.get("constraint_ids") or [])}
            constraint = constraint_by_id.get(constraint_id)
            if not constraint or constraint_id not in allowed_ids:
                if created_new:
                    target_issues.remove(current)
                    occupied.pop(issue_id, None)
                audit["ignored"].append({"type": "confirmed", "pair": list(pair), "reason": "invalid_constraint_id"})
                continue
            importance = str(constraint.get("requirement_importance") or "detail").lower()
            merged_facts["requirement_importance"] = "core" if importance in {"core", "hard"} else "detail"
            merged_facts["constraint_id"] = constraint_id
            current["ref_ids"] = list(dict.fromkeys([
                *(str(value) for value in (current.get("ref_ids") or [])), constraint_id,
            ]))
        else:
            # 非指令维度的 requirement_importance 不参与定档，模型夹带的值会误导
            # 报告阅读者，只保留 4b 原有的。
            if "requirement_importance" in original_facts:
                merged_facts["requirement_importance"] = original_facts["requirement_importance"]
            else:
                merged_facts.pop("requirement_importance", None)
        # 动态与故事两维的定档完全依赖枚举值。模型爱写自由文本，值非法就按子判据
        # 补一个最保守的等价枚举，否则整条证据会因「缺分档事实」被退回复核。
        if dimension_id == "motion_quality" and merged_facts.get("motion_type") not in _MOTION_TYPES:
            merged_facts["motion_type"] = {
                "motion_coordination_stability": "uncoordinated",
                "motion_temporal_disappearance": "temporal_disappearance",
                "motion_unrequested_cut": "discontinuous_cut",
            }.get(key, merged_facts.get("motion_type"))
        if dimension_id == "story_logic":
            merged_facts["story_conflict_type"] = {
                "unjustified_scene_transition": "causal_break",
                "causality_conflict": "causal_break",
                "spatial_relationship_contradiction": "semantic_contradiction",
            }.get(merged_facts.get("story_conflict_type"), merged_facts.get("story_conflict_type"))
        current["facts"] = merged_facts
        current["evidence"] = list(dict.fromkeys([
            *(
                json.dumps(row, ensure_ascii=False, sort_keys=True)
                for row in (current.get("evidence") or []) if isinstance(row, dict)
            ),
            *(json.dumps(row, ensure_ascii=False, sort_keys=True) for row in evidence),
        ]))
        current["evidence"] = [json.loads(row) for row in current["evidence"]]
        for field in ("counterevidence_checked", "evidence_frames"):
            if isinstance(item.get(field), list):
                current[field] = deepcopy(item[field])
        if str(item.get("decision_summary") or "").strip():
            current["decision_summary"] = str(item["decision_summary"])
        current["source"] = "specialist"
        subcriterion["verdict"] = "violated"
        subcriterion["issue_ids"] = list(dict.fromkeys([
            *(str(value) for value in (subcriterion.get("issue_ids") or [])), issue_id,
        ]))
        audit["confirmed_issue_ids"].append(issue_id)
    return audit


def build_native_recheck_candidates(
    uni: dict[str, Any], focus: list[dict[str, Any]]
) -> list[dict[str, Any]]:
    """只接收 4b 未决、观察器 focus 和指令 unknown；候选本身不扣分。

    三个入口对应三种「没看清」：4b 自认未决、观察器标出的低置信区间、编译约束
    未闭环。4b 已明确的 violated / pass 刻意不进候选 —— 复核已定论的结论只是
    重复付费。返回的候选清单同时是 4c 的**权限白名单**：不在其中的子判据，
    4c 提交任何证据都不会被采纳。
    """
    # 观察器只按粗粒度 fact_type 报疑点（它看不到 28 个子判据的定义），
    # 这里展开成子判据集合，让候选与 4c 的作用域是同一套 key。
    focus_keys_by_type = {
        "picture": {
            "picture_clarity_codec", "picture_text_clarity", "picture_exposure_color",
            "picture_composition_static", "picture_structural_visibility",
        },
        "motion": {
            "motion_physics_structure", "motion_coordination_stability",
            "motion_temporal_disappearance", "motion_unrequested_cut",
        },
        "text": {"text_legibility_gibberish", "text_position_correctness"},
        "consistency": {
            "visual_inshot_temporal", "visual_object_disappearance",
            "visual_multishot_continuity",
        },
        "story": {"story_causality_continuity"},
        "reference": {
            "reference_input_fidelity", "reference_v2v_preservation", "reference_v2v_motion",
        },
    }
    by_pair: dict[tuple[str, str], dict[str, Any]] = {}

    def add(
        dimension_id: str, key: str, verdict: Any, trigger: str,
        issue_ids: list[Any] | None = None, constraint_ids: list[str] | None = None,
    ) -> None:
        pair = (str(dimension_id), str(key))
        row = by_pair.setdefault(pair, {
            "dimension_id": pair[0], "key": pair[1],
            "first_verdict": verdict, "issue_ids": [], "triggers": [],
        })
        row["issue_ids"] = list(dict.fromkeys([
            *row["issue_ids"], *(str(value) for value in (issue_ids or []) if str(value)),
        ]))
        if trigger not in row["triggers"]:
            row["triggers"].append(trigger)
        if constraint_ids:
            row["constraint_ids"] = list(dict.fromkeys([
                *(row.get("constraint_ids") or []), *constraint_ids,
            ]))

    subcriteria: dict[tuple[str, str], dict[str, Any]] = {}
    for dimension in uni.get("rubric") or []:
        if not isinstance(dimension, dict):
            continue
        dimension_id = str(dimension.get("dimension_id") or "")
        for subcriterion in dimension.get("subcriteria") or []:
            if not isinstance(subcriterion, dict):
                continue
            key = str(subcriterion.get("key") or "")
            verdict = subcriterion.get("verdict")
            subcriteria[(dimension_id, key)] = subcriterion
            # 音频有独立复听；参考一致性需要重新携带参考素材/源视频，不能让只看到
            # 输出视频的 4c 改判。两者都不进入通用疑点复核。
            if dimension_id in {"audio_quality", "reference_consistency"} or verdict == "not_applicable":
                continue
            if verdict == "not_observable":
                add(dimension_id, key, verdict, "4b_unresolved")

    focus_keys = {
        key
        for row in focus if isinstance(row, dict)
        for fact_type in (row.get("fact_types") or [])
        for key in focus_keys_by_type.get(str(fact_type), set())
    }
    focus_keys.update(
        str(key)
        for row in focus if isinstance(row, dict)
        for key in (row.get("subcriteria") or [])
    )
    for (dimension_id, key), subcriterion in subcriteria.items():
        if dimension_id in {"audio_quality", "reference_consistency"}:
            continue
        if key in focus_keys and subcriterion.get("verdict") != "not_applicable":
            add(
                dimension_id, key, subcriterion.get("verdict"),
                "observer_focus_interval", subcriterion.get("issue_ids"),
            )

    # 编译约束必须逐条闭环。4b 显式返回 unknown 时，按约束类别定向复核，
    # 不把整组指令维度一股脑交给 4c。
    for item in uni.get("instruction_checklist") or []:
        if not isinstance(item, dict) or item.get("responded") != "unknown":
            continue
        key = _INSTRUCTION_CATEGORY_TO_KEY.get(
            str(item.get("constraint_category") or ""), "instruction_action_relation"
        )
        subcriterion = subcriteria.get(("instruction_following", key), {})
        add(
            "instruction_following", key, subcriterion.get("verdict"),
            "instruction_unknown", subcriterion.get("issue_ids"),
            [str(item.get("constraint_id") or "")],
        )
    return list(by_pair.values())


def apply_native_v2v_alignment(
    verdict_bundle: dict[str, Any], align: dict[str, Any] | None, *, has_edit_ops: bool
) -> None:
    """把“输出与源视频几乎相同且存在编辑要求”写成原生编辑达成事实。

    这条结论由程序的 SSIM 对齐独立确证，不经模型，所以 source 记 `programmatic`
    并直接判 violated —— 「要求改而什么都没改」不需要理解 prompt 语义就能成立。
    """
    findings = v2v_align_issues(align, has_edit_ops=has_edit_ops)
    if not findings:
        return
    subcriterion = _native_subcriterion(
        verdict_bundle, "instruction_following", "instruction_v2v_edit"
    )
    if subcriterion is None:
        return
    issue_id = "ISSUE-V2V-ALIGN"
    issues = verdict_bundle.setdefault("issues", [])
    if not any(isinstance(item, dict) and item.get("issue_id") == issue_id for item in issues):
        issues.append({
            "issue_id": issue_id,
            "facts": {"requirement_importance": "core", "source_relation": "output_unchanged"},
            "evidence": [{"time": "全片", "detail": str(findings[0].get("detail") or "编辑未执行")}],
            "source": "programmatic",
        })
    subcriterion["verdict"] = "violated"
    subcriterion["issue_ids"] = list(dict.fromkeys([
        *(subcriterion.get("issue_ids") or []), issue_id,
    ]))
