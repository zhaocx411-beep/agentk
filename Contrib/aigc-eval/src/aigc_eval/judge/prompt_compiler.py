"""把用户提示词拆成一条条可逐项核对的要求。

两条实现并存且都要保留：`compile_prompt_vlm` 让模型来拆，语义更准；
`compile_prompt_heuristic` 是纯规则兜底，在模型不可用或输出不成形时接手。
报告里的 `prompt_graph.compiler` 会记下这次实际用的是哪条，评测时不至于
把两种拆法的结果混在一起比。

**这里只产出「prompt 显式要求了什么」，不产出任何质量约束。** 基础质量、参考
一致性、总体响应在 native8 的固定八维里本来就各有子判据，再作为指令约束注入
一遍，同一个缺陷会在「指令遵循」和它本来所属的维度上各扣一次。观看前固化的
逐约束事实源（instruction_checklist）也以这里的输出为准，掺进兜底约束会让
checklist 出现无法被视频证伪的条目。
"""
from __future__ import annotations

import json
import re
from typing import Any

from aigc_eval.judge.media_parts import reference_parts, select_references
from aigc_eval.judge.rubric_prompts import PROMPT_COMPILER_SYSTEM, v2v_prompt_suffix
from aigc_eval.providers.model_call import JudgeClient

# 编辑操作枚举对应原生子判据 `instruction_v2v_edit`。
# 模型给出枚举外的词一律归 other——归错类只影响 checklist 的措辞，
# 而丢掉这条操作会让「漏改」检不出来，后者严重得多。
EDIT_OPERATIONS = frozenset(
    {"replace", "remove", "add", "restyle", "enhance", "continuation", "other"}
)


def _constraint(
    constraint_id: str,
    category: str,
    statement: str,
    source: str,
    hardness: str = "hard",
    dimension: str = "instruction_following",
) -> dict[str, str]:
    requirement_importance = "detail" if hardness == "soft" else "core"
    return {
        "id": constraint_id,
        "category": category,
        "statement": statement,
        "source": source,
        "hardness": hardness,
        "dimension": dimension,
        "requirement_importance": requirement_importance,
    }


def compile_prompt_heuristic(prompt: str, mode: str, references: list[dict[str, Any]]) -> dict[str, Any]:
    """通用基线拆解器（不针对任何具体 case 做关键词硬编码）。

    只负责从 prompt 结构本身可确定的显式要求；不追加基础质量、参考一致性或
    总体响应兜底约束。prompt 中**具体的主体、动作、空间/时序要求**一律交给 VLM
    拆解器 (compile_prompt_vlm) 产出——本函数不猜、不写死，避免过拟合到特定 case。
    """
    constraints: list[dict[str, str]] = []
    events: list[dict[str, Any]] = []

    text_forbidden = bool(re.search(
        r"(?:无|不要|不得|禁止|不出现|不需要|不含)[^，。；\n]{0,8}(?:字幕|文字|文本|文案|压字)",
        prompt,
    ))
    text_required = not text_forbidden and bool(
        re.search(r"[“\"][^“”\"]+[”\"]", prompt) or "字幕" in prompt or "文字" in prompt
    )
    if text_required:
        constraints.append(
            _constraint("TEXT-01", "文字", "prompt 要求的文字应出现且内容正确", "prompt 原文", "hard", "instruction_following")
        )

    # 通用主体抽取（离线兜底用；VLM 路径会用模型返回的 entities 覆盖）。
    entities = [item for item in re.findall(r"[\u4e00-\u9fff]{2,}", prompt)[:6]] or ["主要主体"]

    # V2V 回退只保留操作载体，不能再造一条可重复计分的总体要求。
    edit_ops: list[dict[str, Any]] = []
    if mode == "V2V":
        # 启发式拆不出具体操作，但「prompt 要求了某种编辑」这个事实本身就足以驱动
        # `v2v_align_issues` 的「编辑未执行」检测，所以给一条 other 型占位操作，
        # 而不是留空 —— 留空会让最硬的那条确定性拦截在回退路径上静默失效。
        if prompt.strip():
            edit_ops.append(
                {
                    "operation": "other",
                    "target": "（未拆解，见 prompt 原文）",
                    "scope": "",
                    "expectation": prompt.strip()[:400],
                }
            )

    return {
        "entities": list(dict.fromkeys(entities)),
        "events": events,
        "shots": [],
        "audio_requirements": {"dialogue": [], "sound_effects": [], "music": [], "ambient": [], "timing_requirements": []},
        "constraints": constraints,
        "edit_ops": edit_ops,
        "physics_exceptions": [],
        "text_requirements": {"required": text_required, "expected_text": None},
        "reference_expectations": [
            {
                "reference_id": r["reference_id"],
                "expected_role": r.get("expected_role", ""),
                "required": r.get("required", True),
            }
            for r in references
        ],
        "compiler": "heuristic",
    }


def _coerce_constraints(raw: Any) -> list[dict[str, Any]]:
    """把拆解器返回的 constraints 规整成 dict 列表。

    Keye 偶发把 constraints 写成字符串数组（``["镜头推进到男人身上", ...]``），
    而下游一律按 dict 取字段，于是 ``c.get("id")`` 直接 AttributeError 把整条
    case 打挂（全量 158 条里因此挂了 3 条）。模型格式跑偏不该丢掉整个 case：
    字符串补成最小可用的约束体，其余非法项丢掉。

    `GENERAL-01` 是旧协议的总体兜底约束，一并丢弃：它无法被单帧证伪，进了
    checklist 只会得到一条恒为 unknown 的条目。
    """
    items: list[dict[str, Any]] = []
    if not isinstance(raw, list):
        return items
    for index, entry in enumerate(raw, start=1):
        if isinstance(entry, dict):
            item = dict(entry)
            if str(item.get("id") or "") == "GENERAL-01":
                continue
            importance = str(
                item.get("requirement_importance") or ""
            ).strip().lower()
            if importance not in {"core", "detail"}:
                importance = "detail" if str(item.get("hardness") or "").lower() == "soft" else "core"
            item["requirement_importance"] = importance
            items.append(item)
        elif isinstance(entry, str) and entry.strip():
            items.append(
                _constraint(
                    f"VLM-{index:02d}", "提示词要求", entry.strip(), "prompt", "hard",
                    "instruction_following",
                )
            )
    return items


def _coerce_edit_ops(raw: Any) -> list[dict[str, Any]]:
    """把编译器返回的 `edit_ops` 规整成固定结构。

    与 `_coerce_constraints` 同样的动机：模型偶发把它写成字符串数组。这里字符串
    补成 `operation=other` 的最小操作体，而不是丢掉 —— 「prompt 要求了编辑」这个
    事实本身就足以驱动 `align_issues` 的未执行检测，即使操作类型没识别出来。
    """
    items: list[dict[str, Any]] = []
    if not isinstance(raw, list):
        return items
    for entry in raw[:20]:
        if isinstance(entry, str) and entry.strip():
            entry = {"operation": "other", "target": entry.strip()}
        if not isinstance(entry, dict):
            continue
        operation = str(entry.get("operation") or "other").strip().lower()
        target = str(entry.get("target") or "").strip()
        if not target and not entry.get("expectation"):
            continue
        items.append(
            {
                "operation": operation if operation in EDIT_OPERATIONS else "other",
                "target": target[:200],
                "scope": str(entry.get("scope") or "")[:200],
                "expectation": str(entry.get("expectation") or "")[:400],
                "requirement_importance": (
                    str(entry.get("requirement_importance") or "").strip().lower()
                    if str(entry.get("requirement_importance") or "").strip().lower() in {"core", "detail"}
                    else "detail" if str(entry.get("hardness") or "").lower() == "soft" else "core"
                ),
            }
        )
    return items


def compile_prompt_vlm(
    client: JudgeClient,
    prompt: str,
    mode: str,
    references: list[dict[str, Any]],
) -> dict[str, Any]:
    ref_desc = [
        {
            "reference_id": r["reference_id"],
            "expected_role": r.get("expected_role"),
            "required": r.get("required", True),
        }
        for r in references
    ]
    user_text = json.dumps(
        {"mode": mode, "prompt": prompt, "reference_assets": ref_desc},
        ensure_ascii=False,
        indent=2,
    )
    parts: list[dict[str, Any]] = [{"text": user_text}]
    parts.extend(reference_parts(select_references(references, stage="compile", prompt=prompt)))
    result = client.generate_json(
        parts,
        system_instruction=PROMPT_COMPILER_SYSTEM + v2v_prompt_suffix("compile", mode),
        stage="prompt_compiler",
        title="3 · 拆解提示词要求",
    )
    # 模型漏拆 Prompt 原文中的明确要求时，用同样的观看前规则补齐。
    baseline = compile_prompt_heuristic(prompt, mode, references)
    if not isinstance(result, dict):
        result = {}
    result["constraints"] = _coerce_constraints(result.get("constraints"))
    existing_ids = {c.get("id") for c in result["constraints"]}
    for item in baseline["constraints"]:
        if item["id"] not in existing_ids:
            result["constraints"].append(item)
    result.setdefault("entities", baseline["entities"])
    result.setdefault("events", baseline.get("events", []))
    result.setdefault("shots", baseline.get("shots", []))
    result.setdefault("audio_requirements", baseline.get("audio_requirements", {}))
    result.setdefault("reference_expectations", baseline["reference_expectations"])
    result.setdefault("text_requirements", baseline["text_requirements"])
    # V2V 编辑操作清单。非 V2V 恒为空列表，因此下游可以无条件取而不必判 mode；
    # 也让「模型没给 edit_ops」与「这个模式没有 edit_ops」在结构上统一（都是 []）。
    # 模型漏给时回落到 baseline 的占位操作：否则「编辑未执行」这条确定性拦截会失效。
    result["edit_ops"] = (
        _coerce_edit_ops(result.get("edit_ops")) or baseline.get("edit_ops") or []
        if mode == "V2V"
        else []
    )
    result["compiler"] = "vlm"
    return result
