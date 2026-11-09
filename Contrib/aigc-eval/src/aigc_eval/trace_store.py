"""Persist and sanitize Gemini multi-step evaluation trajectories."""

from __future__ import annotations

import json
import re
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from aigc_eval import paths


TRACE_ROOT = paths.TRACE_ROOT


def _utc_stamp() -> str:
    return datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S")


def sanitize_parts(parts: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Replace bulky base64 payloads with compact media descriptors."""
    sanitized: list[dict[str, Any]] = []
    for part in parts:
        if not isinstance(part, dict):
            sanitized.append({"unsupported": str(type(part))})
            continue
        if "text" in part and "inline_data" not in part:
            sanitized.append({"type": "text", "text": part.get("text", "")})
            continue
        if "inline_data" in part:
            blob = part.get("inline_data") or {}
            data = blob.get("data") or ""
            sanitized.append(
                {
                    "type": "media",
                    "mime_type": blob.get("mime_type", "unknown"),
                    "bytes_b64": len(data),
                    "bytes_approx": max(0, int(len(data) * 3 / 4)),
                    "note": "inline base64 omitted in trace to keep files small",
                }
            )
            continue
        sanitized.append({"type": "other", "keys": sorted(part.keys())})
    return sanitized


class TraceSession:
    """Collects one evaluation run's model I/O steps and writes them to disk."""

    def __init__(self, *, case_id: str, mode: str, engine: str, model: str | None = None):
        # case_id may contain "/" and CJK; keep the run_id filesystem-safe.
        safe = re.sub(r"[^0-9A-Za-z_.-]", "_", case_id)[:48]
        self.run_id = f"{_utc_stamp()}_{safe}_{uuid.uuid4().hex[:8]}"
        self.case_id = case_id
        self.mode = mode
        self.engine = engine
        self.model = model
        self.created_at = datetime.now(timezone.utc).isoformat()
        self.steps: list[dict[str, Any]] = []
        self._path: Path | None = None

    def begin_step(self, stage: str, title: str) -> float:
        return time.perf_counter()

    def record_step(
        self,
        *,
        stage: str,
        title: str,
        system_instruction: str | None,
        parts: list[dict[str, Any]],
        raw_output: str | None = None,
        parsed_output: dict[str, Any] | None = None,
        error: str | None = None,
        started_at: float | None = None,
        temperature: float | None = None,
        source: str = "model",
    ) -> dict[str, Any]:
        elapsed_ms = None
        if started_at is not None:
            elapsed_ms = int((time.perf_counter() - started_at) * 1000)
        text_parts = [p.get("text", "") for p in parts if isinstance(p, dict) and "text" in p and "inline_data" not in p]
        media_count = sum(1 for p in parts if isinstance(p, dict) and "inline_data" in p)
        step = {
            "index": len(self.steps) + 1,
            "stage": stage,
            "title": title,
            "source": source,
            "model": self.model,
            "temperature": temperature,
            "elapsed_ms": elapsed_ms,
            "input": {
                "system_instruction": system_instruction or "",
                "parts_sanitized": sanitize_parts(parts),
                "text_preview": "\n\n".join(t for t in text_parts if t)[:12000],
                "media_count": media_count,
            },
            "output": {
                "raw_text": (raw_output or "")[:50000],
                "parsed_json": parsed_output,
            },
            "error": error,
        }
        self.steps.append(step)
        return step

    def record_tool_step(
        self,
        *,
        stage: str,
        title: str,
        detail: str,
        payload: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        step = {
            "index": len(self.steps) + 1,
            "stage": stage,
            "title": title,
            "source": "tool",
            "model": None,
            "temperature": None,
            "elapsed_ms": None,
            "input": {
                "system_instruction": "",
                "parts_sanitized": [{"type": "text", "text": detail}],
                "text_preview": detail,
                "media_count": 0,
            },
            "output": {
                "raw_text": json.dumps(payload or {}, ensure_ascii=False, indent=2)[:20000],
                "parsed_json": payload,
            },
            "error": None,
        }
        self.steps.append(step)
        return step

    def save(self, extra: dict[str, Any] | None = None) -> Path:
        TRACE_ROOT.mkdir(parents=True, exist_ok=True)
        document = {
            "run_id": self.run_id,
            "case_id": self.case_id,
            "mode": self.mode,
            "engine": self.engine,
            "model": self.model,
            "created_at": self.created_at,
            "step_count": len(self.steps),
            "steps": self.steps,
            "extra": extra or {},
        }
        path = TRACE_ROOT / f"{self.run_id}.json"
        path.write_text(json.dumps(document, ensure_ascii=False, indent=2), encoding="utf-8")
        self._path = path
        return path

    def to_state(self) -> dict[str, Any]:
        """导出成可 JSON 序列化的状态，供跨进程/跨 activity 续用。

        质检拆成多个 Temporal activity 之后，同一次 run 的 trace 是由不同进程分段写
        的，而 trace 必须是一条完整链（审计要能回答"这个结论经过了哪几步"）。所以
        每段结束时导出、下一段开头恢复，run_id 保持不变。
        """
        return {
            "run_id": self.run_id,
            "case_id": self.case_id,
            "mode": self.mode,
            "engine": self.engine,
            "model": self.model,
            "created_at": self.created_at,
            "steps": self.steps,
        }

    @classmethod
    def from_state(cls, state: dict[str, Any]) -> TraceSession:
        """按 to_state 的产物恢复。run_id 与已有步骤原样接上，不重新生成。"""
        session = cls(
            case_id=str(state.get("case_id") or ""),
            mode=str(state.get("mode") or ""),
            engine=str(state.get("engine") or ""),
            model=state.get("model"),
        )
        session.run_id = str(state.get("run_id") or session.run_id)
        session.created_at = str(state.get("created_at") or session.created_at)
        session.steps = list(state.get("steps") or [])
        return session

    def public_view(self) -> dict[str, Any]:
        return {
            "run_id": self.run_id,
            "saved_path": str(self._path) if self._path else None,
            "saved_url": f"./api/trace?run_id={self.run_id}" if self._path else None,
            "step_count": len(self.steps),
            "steps": self.steps,
        }
