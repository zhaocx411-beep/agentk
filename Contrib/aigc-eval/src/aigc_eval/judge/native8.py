"""原生八维（native8-v1）报告协议：维度契约、事实校验与确定性三档聚合。

这是在线主链路**唯一**的聚合入口，也是整个项目唯一的质量事实源。分工是刻意的：

- 模型（4b 主判 / 4c 增量取证）只负责产出**原子事实** —— 一条 issue 描述「什么时刻、
  什么对象、发生了什么」，外加一组结构化 facts 供分档使用；
- 本模块负责**校验与定档** —— 检查 issue 引用是否成立、本维事实是否齐备、证据是否够硬，
  然后按 v1.1 规则算出 `0 / 0.5 / 1`。

模型给出的 score / tier / severity **一律忽略**。让模型直接给分是上一版（v41 五维加权）的
做法，它的问题不是不准，而是不可审计：同一条证据换次采样就能得出不同总分，没法回答
「这 0.5 分是哪条事实换来的」。现在每个非 1.0 的维度都能回溯到具体 issue_id。

三条硬约束，改本模块时不要破坏：

1. **八个维度恒定输出**，顺序固定（`DIMENSIONS`）。哪怕不适用也要以 `applicable=False`
   出现 —— 「没评过」和「没问题」必须可区分，缺项会被下游当成通过。
2. **violated 必须挂 issue_id**。缺 issue、缺本维分档事实、或证据不够硬的 violated，
   一律降为 `not_observable` 并记入 `review_required`，绝不猜测扣分。
3. **不产出总分**。`score_total` / `final` / `annotation` 这类旧协议字段属于泄漏，
   由 `validate_native8_report` 拦截。
"""

from __future__ import annotations

from copy import deepcopy
from typing import Any


SCHEMA_VERSION = "native8-v1"

# 八个维度及其固定子判据。这份表就是对外契约：报告里的 rubric 必须与它逐项对齐，
# 顺序也不能变（`validate_native8_report` 会逐项比对）。新增子判据等于改契约，
# 需要同步 rubric_prompts 的提示词、报告渲染与缓存版本。
DIMENSIONS: tuple[dict[str, Any], ...] = (
    {"id": "instruction_following", "name": "指令遵循", "subcriteria": (
        ("instruction_subject_attributes", "主体数量与属性"),
        ("instruction_action_relation", "动作、对象与时空关系"),
        ("instruction_camera", "运镜"),
        ("instruction_style_environment", "风格与环境"),
        ("instruction_multishot", "多镜头计划"),
        ("instruction_requested_text_audio", "明确要求的文字与音频"),
        ("instruction_v2v_edit", "V2V 编辑达成"),
    )},
    {"id": "reference_consistency", "name": "参考一致性", "subcriteria": (
        ("reference_input_fidelity", "输入参考主体与静态细节"),
        ("reference_v2v_preservation", "V2V 源视频保真"),
        ("reference_v2v_motion", "V2V 动态继承"),
    )},
    {"id": "visual_consistency", "name": "视觉一致性", "subcriteria": (
        ("visual_inshot_temporal", "同镜头时序一致性"),
        ("visual_object_disappearance", "物体凭空消失"),
        ("visual_multishot_continuity", "跨镜头一致性"),
    )},
    {"id": "audio_quality", "name": "音频综合质量", "subcriteria": (
        ("audio_signal_quality", "音质"),
        ("audio_av_sync", "音画同步"),
        ("audio_temporal_consistency", "音频前后一致性"),
    )},
    {"id": "picture_quality", "name": "画面质量", "subcriteria": (
        ("picture_clarity_codec", "清晰度与编解码"),
        ("picture_text_clarity", "文字清晰度（模糊/压缩）"),
        ("picture_exposure_color", "曝光与色彩"),
        ("picture_composition_static", "构图与静态合理性"),
        ("picture_structural_visibility", "正常速度可见的结构崩坏"),
    )},
    {"id": "motion_quality", "name": "动态质量", "subcriteria": (
        ("motion_physics_structure", "物理与结构合理性"),
        ("motion_coordination_stability", "协调与稳定性"),
        ("motion_temporal_disappearance", "动态过程中的物体无因消失"),
        ("motion_unrequested_cut", "非 Prompt 要求的突兀切镜"),
    )},
    {"id": "text_quality", "name": "文字质量", "subcriteria": (
        ("text_legibility_gibberish", "字形与乱码"),
        ("text_position_correctness", "位置与正确性"),
    )},
    {"id": "story_logic", "name": "故事逻辑连贯性", "subcriteria": (
        ("story_causality_continuity", "因果、承接、动机和内容常识"),
    )},
)

DIMENSION_NAMES = tuple(item["name"] for item in DIMENSIONS)

_VALID_VERDICTS = {"pass", "violated", "not_observable", "not_applicable"}

# issue 的来源：4b 主判 / 4c 专项复核 / 程序确定性测量（如 V2V 的 SSIM 对齐）。
_VALID_SOURCES = {"unified", "specialist", "programmatic"}

# facts 白名单。模型爱夹带自由字段（severity、score、reason…），照收会让分档
# 逻辑悄悄依赖上未定义的键。只留分档与证据校验真正要用的那些，其余丢弃。
_FACT_KEYS = {
    "requirement_importance", "object_scope", "normal_speed_visible",
    "duration_seconds", "text_importance", "motion_type", "affects_adoption",
    "same_shot", "state_before", "state_after", "change_cause",
    "source_relation", "event_origin", "confidence", "defect_type",
    "text_issue_type", "requirement_id", "constraint_id",
    "cut_required_by_prompt", "motion_continuity_broken",
    "prompt_change_excluded", "occlusion_or_exit_excluded",
    "viewpoint_explanation_excluded", "continuous_frames_confirmed",
    "story_conflict_type", "story_event_before", "story_event_after",
    "independent_of_prompt_failure", "primary_event_changed",
}

# 「没有精确时刻」的几种写法。证据必须能指回具体时刻才算可核查，
# 「全片」等同于没定位。
_VAGUE_TIMES = {"", "全片", "n/a", "N/A"}


def _evidence(row: dict[str, Any]) -> list[dict[str, Any]]:
    """把 issue 的证据归一成 `[{time, detail}]`。

    模型可能给列表、给单个字符串、或只在 observer_note 里留一句话，
    三种形状都得能落成同一结构，否则下游的证据强度判定会因形状差异误判。
    """
    raw = row.get("evidence")
    if isinstance(raw, list):
        return [
            deepcopy(item) if isinstance(item, dict) else {"detail": str(item)}
            for item in raw
            if item
        ]
    detail = str(raw or row.get("observer_note") or "").strip()
    if not detail:
        return []
    return [{"time": str(row.get("time") or "全片"), "detail": detail}]


def _has_dimension_evidence(
    dimension_id: str,
    target: str,
    facts: dict[str, Any],
    evidence: list[dict[str, Any]],
) -> bool:
    """这条 issue 的证据强度是否够支撑该子判据判 violated。

    这是**误报的主要闸门**。各子判据的门槛不同，因为它们的典型误报成因不同：
    遮挡/出画会被误报成「凭空消失」、视角变化会被误报成「跨镜头不一致」、
    prompt 本来就要求的变化会被误报成「时序不一致」。所以要求模型显式排除掉
    这些替代解释（那些 `*_excluded` 事实），而不是只给一句描述。

    门槛不满足时不是直接判 pass —— 调用方会降为 `not_observable` 并送去 4c 复核。
    """
    if not evidence or not any(str(item.get("detail") or "").strip() for item in evidence):
        return False

    precise_time = any(
        str(item.get("time") or "").strip() not in _VAGUE_TIMES
        for item in evidence
    )
    confidence = str(facts.get("confidence") or "").lower()

    if dimension_id == "audio_quality":
        return precise_time and bool(str(facts.get("confidence") or "").strip())

    # 突兀切镜：唯一可靠的判据是「prompt 没要求切」且「运动确实断了」。
    # 少了后者，正常的蒙太奇剪辑会被报成缺陷。
    if target == "motion_unrequested_cut":
        return (
            precise_time
            and confidence in {"high", "medium"}
            and facts.get("cut_required_by_prompt") is False
            and facts.get("motion_continuity_broken") is True
        )

    # 感知密集类（结构崩坏、文字模糊、物理违背…）：要求精确时刻 + 中高置信。
    # 结构崩坏另需连续帧确认 —— 单帧上的怪异姿态多半是运动模糊，不是崩坏。
    if target in {
        "picture_structural_visibility", "picture_text_clarity",
        "motion_physics_structure", "motion_coordination_stability",
        "motion_temporal_disappearance",
    }:
        if not (precise_time and confidence in {"high", "medium"}):
            return False
        if target == "picture_structural_visibility":
            return facts.get("continuous_frames_confirmed") is True
        return True

    # 同镜头内的时序不一致：必须确认在同一镜头、给出前后状态、认定是真实变化
    # 而非 prompt 要求的变化。「凭空消失」再加一道遮挡/出画排除。
    if target in {"visual_inshot_temporal", "visual_object_disappearance"}:
        valid = (
            facts.get("same_shot") is True
            and bool(str(facts.get("state_before") or "").strip())
            and bool(str(facts.get("state_after") or "").strip())
            and facts.get("change_cause") == "actual_change"
            and facts.get("prompt_change_excluded") is True
        )
        if target == "visual_object_disappearance":
            valid = valid and facts.get("occlusion_or_exit_excluded") is True
        return valid

    # 跨镜头一致性：镜头必须真的换了，且排除「只是换了视角」这个最常见的替代解释。
    if target == "visual_multishot_continuity":
        return (
            facts.get("same_shot") is False
            and bool(str(facts.get("state_before") or "").strip())
            and bool(str(facts.get("state_after") or "").strip())
            and facts.get("change_cause") == "actual_change"
            and facts.get("prompt_change_excluded") is True
            and facts.get("viewpoint_explanation_excluded") is True
        )

    # 故事逻辑只考核**模型自行补充**的情节：prompt 明确写了的情节即使不合理也不算错。
    # 而且必须是主线事件受影响，否则任何背景细节都能被说成逻辑问题。
    if target == "story_causality_continuity":
        return (
            facts.get("event_origin") == "generated_addition"
            and facts.get("independent_of_prompt_failure") is True
            and facts.get("primary_event_changed") is True
            and facts.get("story_conflict_type") in {
                "causal_break", "motivation_break", "semantic_contradiction",
                "common_sense_violation",
            }
            and bool(str(facts.get("story_event_before") or "").strip())
            and bool(str(facts.get("story_event_after") or "").strip())
        )

    return True


def _tier_for_issue(dimension_id: str, facts: dict[str, Any]) -> float | None:
    """单条事实在该维度下的档位。

    返回 None 表示**缺少本维的分档事实** —— 此时不能猜，调用方会把它退回复核。
    比如指令遵循要靠 `requirement_importance` 区分「核心要求没做到」（0 分）和
    「细节没做到」（0.5 分）；模型没给这个字段就无法定档，硬给 0.5 相当于凭空扣分。
    """
    if dimension_id == "instruction_following":
        value = facts.get("requirement_importance")
        return 0.0 if value == "core" else 0.5 if value == "detail" else None

    # 一致性两维按「变的是不是主体」定档：主体变了是硬伤，配角/背景变了算瑕疵。
    if dimension_id in {"reference_consistency", "visual_consistency"}:
        value = facts.get("object_scope")
        return 0.0 if value == "subject" else 0.5 if value == "non_subject" else None

    # 音频按持续时长定档：0.5s 以上的异常听得出来，短于此算瞬时杂音。
    if dimension_id == "audio_quality":
        value = facts.get("duration_seconds")
        return (0.0 if float(value) >= 0.5 else 0.5) if isinstance(value, (int, float)) else None

    if dimension_id == "picture_quality":
        # 正常速度看不见的问题不算问题：逐帧暂停找出来的瑕疵不影响观看。
        if facts.get("normal_speed_visible") is False:
            return 1.0
        if facts.get("affects_adoption") is True:
            return 0.0
        duration = facts.get("duration_seconds")
        if facts.get("normal_speed_visible") is True and isinstance(duration, (int, float)):
            return 0.0 if float(duration) > 0.5 else 0.5
        if facts.get("normal_speed_visible") is True and facts.get("affects_adoption") is False:
            return 0.5
        return None

    if dimension_id == "motion_quality":
        value = facts.get("motion_type")
        if value in {"physics_violation", "structural_break", "temporal_disappearance"}:
            return 0.0
        if value in {"uncoordinated", "camera_jitter", "discontinuous_cut"}:
            return 0.5
        return None

    if dimension_id == "text_quality":
        value = facts.get("text_importance")
        return 0.0 if value == "required_or_major" else 0.5 if value == "incidental_small" else None

    if dimension_id == "story_logic":
        value = facts.get("affects_adoption")
        return 0.0 if value is True else 0.5 if value is False else None

    return None


def _applicable(dimension_id: str, *, mode: str, has_reference: bool, has_audio: bool) -> bool:
    """程序化的适用性兜底。

    不适用的维度绝不交给模型判断 —— 没有参考图时问「与参考图一致吗」，模型会
    编一个答案出来。这里先把它钉成 `not_applicable`，模型的任何结论都被覆盖。
    """
    if dimension_id == "reference_consistency":
        return has_reference or mode in {"I2V", "V2V"}
    if dimension_id == "audio_quality":
        return has_audio
    return True


def finalize_native8_result(
    raw: dict[str, Any],
    *,
    mode: str,
    has_reference: bool,
    has_audio: bool,
) -> dict[str, Any]:
    """校验模型事实并确定性生成八维三档。

    输入是 4b/4c 合并后的事实草案，输出是对外报告的骨架（`issues` + `rubric` +
    待复核清单）。全过程无随机性、无模型调用，同一份草案永远得到同一份结论。
    """
    # ---- 第一步：归一 issues，建立 issue_id → issue 的索引 ----
    # issue_id 由模型给，但不能信：可能重复、可能不符合 ISSUE-NNN 形状。
    # 重编号而不是丢弃，否则子判据里的引用会集体失效。
    raw_issues = [item for item in (raw.get("issues") or []) if isinstance(item, dict)]
    issues: list[dict[str, Any]] = []
    issue_map: dict[str, dict[str, Any]] = {}
    for index, item in enumerate(raw_issues, 1):
        issue_id = str(item.get("issue_id") or f"ISSUE-{index:03d}").strip().upper()
        if not issue_id.startswith("ISSUE-") or issue_id in issue_map:
            issue_id = f"ISSUE-{index:03d}"
        facts = {
            key: deepcopy(value)
            for key, value in (item.get("facts") or {}).items()
            if key in _FACT_KEYS and value is not None
        }
        source = str(item.get("source") or "unified")
        issue = {
            "issue_id": issue_id,
            "facts": facts,
            "evidence": _evidence(item),
            "source": source if source in _VALID_SOURCES else "unified",
        }
        # 这几个字段不参与定档，但报告页与证据层要用，原样带过。
        for key in (
            "ref_ids", "specialist_reviews", "counterevidence_checked",
            "evidence_frames", "observer_fact_ids",
        ):
            if isinstance(item.get(key), list):
                issue[key] = deepcopy(item[key])
        if str(item.get("decision_summary") or "").strip():
            issue["decision_summary"] = str(item["decision_summary"])[:1000]
        issues.append(issue)
        issue_map[issue_id] = issue

    # ---- 第二步：按固定八维定档 ----
    raw_dimensions = {
        str(item.get("dimension_id")): item
        for item in (raw.get("rubric") or [])
        if isinstance(item, dict)
    }
    rubric: list[dict[str, Any]] = []
    referenced: set[str] = set()
    review_required: list[dict[str, str]] = []

    for dimension in DIMENSIONS:
        dim_id = dimension["id"]
        applicable = _applicable(
            dim_id, mode=mode.upper(), has_reference=has_reference, has_audio=has_audio
        )
        raw_subs = {
            str(item.get("key")): item
            for item in (raw_dimensions.get(dim_id, {}).get("subcriteria") or [])
            if isinstance(item, dict)
        }
        subcriteria: list[dict[str, Any]] = []
        # issue_id → 该 issue 在本维贡献的档位。按 issue 而非按子判据聚合，
        # 是为了让「一个问题同时违反两个子判据」只扣一次。
        dim_tiers: dict[str, float] = {}

        for key, name in dimension["subcriteria"]:
            raw_sub = raw_subs.get(key, {})
            verdict = str(raw_sub.get("verdict") or "not_observable")
            if verdict not in _VALID_VERDICTS:
                verdict = "not_observable"
            ids = list(dict.fromkeys(
                str(value).strip().upper()
                for value in (raw_sub.get("issue_ids") or [])
                if str(value).strip().upper() in issue_map
            ))

            if not applicable:
                verdict, ids = "not_applicable", []
            elif verdict == "violated":
                valid_ids: list[str] = []
                for issue_id in ids:
                    issue = issue_map[issue_id]
                    # V2V 里从源视频继承下来、且本次编辑不该改的部分，不算生成缺陷。
                    if (
                        mode.upper() == "V2V"
                        and issue["facts"].get("source_relation") == "inherited_unchanged"
                    ):
                        continue
                    tier = _tier_for_issue(dim_id, issue["facts"])
                    if (
                        tier is None
                        or tier == 1.0
                        or not _has_dimension_evidence(dim_id, key, issue["facts"], issue["evidence"])
                    ):
                        review_required.append({
                            "issue_id": issue_id,
                            "dimension_id": dim_id,
                            "subcriterion": key,
                            "reason": "incomplete_tier_or_dimension_evidence",
                        })
                        continue
                    valid_ids.append(issue_id)
                    previous = dim_tiers.get(issue_id)
                    dim_tiers[issue_id] = tier if previous is None else min(previous, tier)
                    referenced.add(issue_id)
                ids = valid_ids
                # 所有 issue 都没通过校验 → 这条子判据其实没有可用结论。
                # 判 pass 是错的（确实看到了可疑现象），所以退回 not_observable。
                if not ids:
                    verdict = "not_observable"
            else:
                ids = []

            subcriterion = {"key": key, "name": name, "verdict": verdict, "issue_ids": ids}
            if isinstance(raw_sub.get("decision_basis"), dict):
                subcriterion["decision_basis"] = deepcopy(raw_sub["decision_basis"])
            if isinstance(raw_sub.get("observer_fact_ids"), list):
                subcriterion["observer_fact_ids"] = list(dict.fromkeys(
                    str(value) for value in raw_sub["observer_fact_ids"] if str(value)
                ))
            if raw_sub.get("confidence") is not None:
                subcriterion["confidence"] = raw_sub["confidence"]
            subcriteria.append(subcriterion)

        # 维度定档：有任一 0 分事实即恶劣错误；否则瑕疵累积到 3 条也升级为恶劣错误
        # （单看每条都不严重，但一条视频里攒够三处瑕疵，观感上已经不可用了）。
        tiers = list(dim_tiers.values())
        blemishes = sum(value == 0.5 for value in tiers)
        escalated = not any(value == 0.0 for value in tiers) and blemishes >= 3
        if not applicable:
            score, tier_name = None, "不适用"
        elif any(value == 0.0 for value in tiers) or escalated:
            score, tier_name = 0.0, "恶劣错误"
        elif blemishes:
            score, tier_name = 0.5, "瑕疵"
        else:
            score, tier_name = 1.0, "无问题"

        unresolved = [
            item["key"] for item in subcriteria
            if applicable and item["verdict"] == "not_observable"
        ]
        rubric.append({
            "dimension_id": dim_id,
            "dimension_name": dimension["name"],
            "applicable": applicable,
            "score": score,
            "tier": tier_name,
            "escalated": escalated,
            "decision_status": "review_required" if unresolved else "resolved",
            "unresolved_subcriteria": unresolved,
            "subcriteria": subcriteria,
            "feedback": list(dim_tiers),
        })

    # 只保留真正被子判据引用的 issue：未被引用的要么是模型的自言自语，
    # 要么校验没过，留在报告里会让 reviewer 以为它影响了得分。
    review_ids = {item["issue_id"] for item in review_required}
    return {
        "schema_version": SCHEMA_VERSION,
        "analysis_status": "complete",
        "issues": [item for item in issues if item["issue_id"] in referenced],
        "rubric": rubric,
        "review_required": review_required,
        "review_candidates": [
            deepcopy(item) for item in issues if item["issue_id"] in review_ids
        ],
    }


def validate_native8_report(report: dict[str, Any]) -> list[str]:
    """输出契约校验，返回问题清单（空列表即合规）。

    存在的意义是**让字段漂移在写盘前就炸掉**：报告 JSON 被缓存、被 HTML 渲染、
    被 worker 当 output_schema 返回，改错一个字段名会在五个地方静默失真。
    """
    errors: list[str] = []

    if report.get("schema_version") != SCHEMA_VERSION:
        errors.append("schema_version must be native8-v1")

    rubric = report.get("rubric") or []
    if [row.get("dimension_id") for row in rubric] != [row["id"] for row in DIMENSIONS]:
        errors.append("rubric must contain the fixed eight dimensions in order")
    else:
        for row, spec in zip(rubric, DIMENSIONS):
            actual_keys = [item.get("key") for item in (row.get("subcriteria") or [])]
            expected_keys = [key for key, _ in spec["subcriteria"]]
            if actual_keys != expected_keys:
                errors.append(f"{spec['id']}: subcriteria must match the fixed native8 order")

    issue_ids = {row.get("issue_id") for row in report.get("issues") or []}
    issues_by_id = {
        str(row.get("issue_id")): row
        for row in report.get("issues") or []
        if isinstance(row, dict)
    }

    # 指令遵循的每条约束只能绑一条 issue。同一条约束被拆成多个 issue 时，
    # 定档会把它当多个独立问题重复扣分（瑕疵攒够 3 条还会升级），
    # 于是「一条要求没做到」被放大成「恶劣错误」。
    instruction_constraints: dict[str, set[str]] = {}
    for dimension in rubric:
        for criterion in dimension.get("subcriteria") or []:
            if criterion.get("verdict") == "violated" and not criterion.get("issue_ids"):
                errors.append(f"{criterion.get('key')}: violated without issue_ids")
            for issue_id in criterion.get("issue_ids") or []:
                if issue_id not in issue_ids:
                    errors.append(f"{criterion.get('key')}: unknown issue {issue_id}")
                elif dimension.get("dimension_id") == "instruction_following":
                    issue = issues_by_id.get(str(issue_id)) or {}
                    facts = issue.get("facts") or {}
                    constraint_ids = {
                        str(facts.get("constraint_id") or "").strip(),
                        *(str(value).strip() for value in (issue.get("ref_ids") or [])),
                    } - {""}
                    for constraint_id in constraint_ids:
                        instruction_constraints.setdefault(constraint_id, set()).add(str(issue_id))
    for constraint_id, bound_issue_ids in instruction_constraints.items():
        if len(bound_issue_ids) > 1:
            errors.append(
                f"instruction constraint {constraint_id} bound to multiple issues: "
                + ", ".join(sorted(bound_issue_ids))
            )

    # 证据卡与 bbox 都按 issue_id 挂载。悬空引用会让报告页出现点不开的证据。
    for card in report.get("evidence") or []:
        if not isinstance(card, dict):
            errors.append("evidence card is not an object")
            continue
        ref_id = str(card.get("issue_id") or card.get("ref_id") or "")
        if not ref_id:
            errors.append("evidence card missing issue_id")
        elif ref_id not in issue_ids:
            errors.append(f"evidence references missing issue: {ref_id}")

    for locator in report.get("grounding") or []:
        if not isinstance(locator, dict):
            errors.append("grounding locator is not an object")
            continue
        ref_id = str(locator.get("ref_id") or "")
        if not ref_id:
            errors.append("grounding locator missing issue ref_id")
        elif ref_id not in issue_ids:
            errors.append(f"grounding references missing issue: {ref_id}")

    # 旧五维协议的字段一律视为泄漏。它们同时出现会让下游不知道该信哪套，
    # 而「同时兼容两套」正是这次重构要消灭的状态。
    forbidden = {
        "annotation", "evaluations", "scores", "dim_scores",
        "dim_badcase", "score_total", "final",
    }
    leaked = sorted(forbidden.intersection(report))
    if leaked:
        errors.append("legacy fields leaked: " + ", ".join(leaked))

    return errors


def summarize_native8(report: dict[str, Any]) -> dict[str, Any]:
    """把一份 native8 报告压成一行可比对的统计，供批量、历史、评测共用。

    **`tier`/`score` 单独读是会误判的**，这是本函数存在的主要理由。它们的语义是
    「没有被确证的缺陷」，而不是「已核实通过」：28 个子判据全是 `not_observable`
    的维度同样拿到 score=1.0、tier=无问题 —— 一次都没看清，和看清了没问题，在这
    两个字段上长得一样。区分二者的是 `decision_status`（`resolved` /
    `review_required`）。任何只读 tier 的调用方都会把「没核实」当成「合格」，所以
    这里把两者一起投影出来，并单独给出 `unverified_dimensions`。

    没有综合分可返回，也不要在这里造一个：批量对比该看的是各维三档的分布、
    未核实维度数和待复核条数。
    """
    rubric = [row for row in (report.get("rubric") or []) if isinstance(row, dict)]
    subcriteria = [
        sub
        for row in rubric
        for sub in (row.get("subcriteria") or []) if isinstance(sub, dict)
    ]
    applicable = [row for row in rubric if row.get("applicable")]
    unverified = [row for row in applicable if row.get("decision_status") != "resolved"]
    return {
        "schema_version": report.get("schema_version"),
        "analysis_status": report.get("analysis_status"),
        "tiers": {
            str(row.get("dimension_id")): row.get("score")
            for row in rubric
        },
        "issue_count": len([i for i in (report.get("issues") or []) if isinstance(i, dict)]),
        "violated_subcriteria": sum(sub.get("verdict") == "violated" for sub in subcriteria),
        "unresolved_subcriteria": sum(sub.get("verdict") == "not_observable" for sub in subcriteria),
        "applicable_dimensions": len(applicable),
        # 结论站得住的维度数。批量跑完先看这个，再看三档分布 —— 覆盖率塌了的话，
        # 三档分布再好看也只说明模型没看清，不说明视频没问题。
        "resolved_dimensions": len(applicable) - len(unverified),
        "unverified_dimensions": len(unverified),
        "unverified_dimension_ids": [str(row.get("dimension_id")) for row in unverified],
        "review_required_count": len(report.get("review_required") or []),
        "review_candidate_count": len(report.get("review_candidates") or []),
    }
