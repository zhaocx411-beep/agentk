"""按 usage 明细核算单条 case 的调用成本。

注意 AgentK 通道拿不到 token 用量（两个能力的返回都不含 usage），那些记录
会带 `tokens_unavailable` 标记。这里如实累加成 0 并保留标记，**不猜**、
也不折算——把缺失当成真实的零成本，比报一个编出来的数字危险得多。
"""
from __future__ import annotations

from typing import Any

from aigc_eval.judge.settings import CURATED_CASES, PRICE_INPUT_PER_M, PRICE_OUTPUT_PER_M


def _profile_for(case_id: str) -> str:
    return CURATED_CASES.get(case_id, {}).get("profile", "generic")


def _status_label(status: str) -> str:
    return {"blocked": "拦截", "needs_review": "建议复核", "pass": "通过"}.get(status, "建议复核")


def build_cost(usage: list[dict[str, Any]]) -> dict[str, Any]:
    """Cost of analyzing one video: how many model calls, of what input kind,
    and how many tokens each consumed."""
    calls = list(usage or [])
    by_kind: dict[str, int] = {"text": 0, "image": 0, "video": 0}
    for call in calls:
        kind = str(call.get("input_kind") or "text")
        by_kind[kind] = by_kind.get(kind, 0) + 1
    prompt_by_modality: dict[str, int] = {}
    for call in calls:
        for modality, count in (call.get("prompt_tokens_by_modality") or {}).items():
            prompt_by_modality[modality] = prompt_by_modality.get(modality, 0) + int(count or 0)
    prompt_tokens = sum(int(c.get("prompt_tokens") or 0) for c in calls)
    output_tokens = sum(int(c.get("output_tokens") or 0) for c in calls)
    thoughts_tokens = sum(int(c.get("thoughts_tokens") or 0) for c in calls)
    # 思考 token 按输出价计费（Gemini 计费口径）
    billed_output = output_tokens + thoughts_tokens
    input_cost = prompt_tokens / 1_000_000 * PRICE_INPUT_PER_M
    output_cost = billed_output / 1_000_000 * PRICE_OUTPUT_PER_M
    return {
        "calls": len(calls),
        "calls_by_input": by_kind,
        "prompt_tokens": prompt_tokens,
        "output_tokens": output_tokens,
        "thoughts_tokens": thoughts_tokens,
        "total_tokens": sum(int(c.get("total_tokens") or 0) for c in calls),
        "prompt_tokens_by_modality": prompt_by_modality,
        "price": {
            "input_per_m": PRICE_INPUT_PER_M,
            "output_per_m": PRICE_OUTPUT_PER_M,
            "input_cost": round(input_cost, 4),
            "output_cost": round(output_cost, 4),
            "total_cost": round(input_cost + output_cost, 4),
        },
        "media_bytes": sum(int(c.get("media_bytes") or 0) for c in calls),
        "images_sent": sum(int(c.get("images") or 0) for c in calls),
        "videos_sent": sum(int(c.get("videos") or 0) for c in calls),
        "elapsed_ms": sum(int(c.get("elapsed_ms") or 0) for c in calls),
        "detail": [
            {
                "step": c.get("title"),
                "input_kind": c.get("input_kind"),
                "images": c.get("images"),
                "videos": c.get("videos"),
                "media_bytes": c.get("media_bytes"),
                "prompt_tokens": c.get("prompt_tokens"),
                "output_tokens": c.get("output_tokens"),
                "thoughts_tokens": c.get("thoughts_tokens"),
                "total_tokens": c.get("total_tokens"),
                "elapsed_ms": c.get("elapsed_ms"),
                "attempt": c.get("attempt"),
            }
            for c in calls
        ],
    }
