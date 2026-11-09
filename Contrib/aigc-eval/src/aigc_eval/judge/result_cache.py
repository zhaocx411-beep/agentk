"""pointwise 判定结果的磁盘缓存。

缓存键含 provider / mode / use_vlm 与 `CACHE_VERSION`：换通道或换模式会各自
命中独立的缓存，互不污染，这样同一条 case 在不同通道下的结果可以并存对比。
只调 settings 里的阈值**不会**改变键，需要重跑时带 refresh=True。
"""
from __future__ import annotations

import json
import re
from typing import Any

from aigc_eval.config import DEFAULT_PROVIDER, normalize_provider
from aigc_eval.judge.cases import CaseFiles, get_case_files
from aigc_eval.judge.layout import RESULTS_ROOT
from aigc_eval.judge.settings import CACHE_VERSION


def _cache_key(slug: str, source: str, use_vlm: bool, provider: str, mode: str | None) -> str:
    engine_key = normalize_provider(provider) if use_vlm else "mock"
    return f"{slug}__{source}__{engine_key}__{mode or 'auto'}__{CACHE_VERSION}"


def load_cached(
    case_id: str, *, source: str = "kling", use_vlm: bool = True,
    provider: str = DEFAULT_PROVIDER, mode: str | None = None,
    case_files: CaseFiles | None = None,
) -> dict[str, Any] | None:
    """Return a previously saved report for this (case, source, engine, mode),
    or None. Never runs the model — used to show history without re-analyzing."""
    if case_files is not None:
        source = re.sub(r"[^0-9A-Za-z_.-]", "_", source or "upload") or "upload"
    else:
        try:
            case_files = get_case_files(case_id)
        except (FileNotFoundError, ValueError):
            return None
        source = "seedance" if source == "seedance" else "kling"
    path = RESULTS_ROOT / f"{_cache_key(case_files.slug, source, use_vlm, provider, mode)}.json"
    if not path.exists():
        return None
    try:
        report = json.loads(path.read_text(encoding="utf-8"))
    except Exception:  # noqa: BLE001 - corrupt cache is simply "no history"
        return None
    report["from_cache"] = True
    return report
