from __future__ import annotations

from collections import OrderedDict
from copy import deepcopy
from datetime import UTC, datetime
from typing import Any


class NarratoExecutionHistory:
    """Process-local execution snapshots for the browser demo."""

    def __init__(self, limit: int = 50) -> None:
        self.limit = limit
        self._records: OrderedDict[tuple[str, str], dict[str, Any]] = OrderedDict()

    def add(
        self,
        pipeline: str,
        title: str,
        started: dict[str, Any],
        request: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        session_id = str(started.get("sessionId") or "")
        execution_id = str(started.get("executionId") or "")
        if not session_id or not execution_id:
            raise ValueError("execution history requires sessionId and executionId")
        now = _valid_timestamp(started.get("createdAt")) or _timestamp()
        record = {
            "sessionId": session_id,
            "executionId": execution_id,
            "pipeline": pipeline,
            "title": title,
            "status": str(started.get("status") or "running"),
            "error": "",
            "events": [],
            "output": None,
            "request": deepcopy(request),
            "cancelRequested": False,
            "cancelRequestedAt": "",
            "createdAt": now,
            "updatedAt": now,
        }
        key = (session_id, execution_id)
        self._records[key] = record
        self._records.move_to_end(key)
        while len(self._records) > self.limit:
            self._records.popitem(last=False)
        return deepcopy(record)

    def update(
        self,
        session_id: str,
        execution_id: str,
        snapshot: dict[str, Any],
    ) -> dict[str, Any]:
        key = (session_id, execution_id)
        record = self._records.get(key)
        if record is None:
            now = _timestamp()
            record = {
                "sessionId": session_id,
                "executionId": execution_id,
                "pipeline": "unknown",
                "title": "Narrato execution",
                "cancelRequested": False,
                "cancelRequestedAt": "",
                "createdAt": now,
            }
            self._records[key] = record
        normalized = deepcopy(snapshot)
        if isinstance(normalized.get("events"), list):
            observed_at = _timestamp()
            previous_times = {
                _event_key(event, index): _valid_timestamp(event.get("observedAt"))
                for index, event in enumerate(record.get("events", []))
                if isinstance(event, dict) and _valid_timestamp(event.get("observedAt"))
            }
            for index, event in enumerate(normalized["events"]):
                if not isinstance(event, dict):
                    continue
                if _valid_timestamp(event.get("createdAt")) or _valid_timestamp(
                    event.get("observedAt")
                ):
                    continue
                event["observedAt"] = previous_times.get(_event_key(event, index), observed_at)
        record.update(normalized)
        record["sessionId"] = session_id
        record["executionId"] = execution_id
        if str(record.get("status") or "").lower() in {"completed", "failed", "cancelled"}:
            record["cancelRequested"] = False
        record["updatedAt"] = _timestamp()
        self._records.move_to_end(key)
        return deepcopy(record)

    def get(self, session_id: str, execution_id: str) -> dict[str, Any] | None:
        record = self._records.get((session_id, execution_id))
        return deepcopy(record) if record is not None else None

    def mark_cancel_requested(
        self, session_id: str, execution_id: str
    ) -> dict[str, Any] | None:
        key = (session_id, execution_id)
        record = self._records.get(key)
        if record is None:
            return None
        now = _timestamp()
        record["cancelRequested"] = True
        record["cancelRequestedAt"] = now
        record["updatedAt"] = now
        self._records.move_to_end(key)
        return deepcopy(record)

    def list(self) -> list[dict[str, Any]]:
        return [
            {
                "sessionId": record["sessionId"],
                "executionId": record["executionId"],
                "pipeline": record.get("pipeline", "unknown"),
                "title": record.get("title", "Narrato execution"),
                "status": record.get("status", "running"),
                "cancelRequested": bool(record.get("cancelRequested")),
                "error": record.get("error", ""),
                "hasOutput": record.get("output") is not None,
                "createdAt": record["createdAt"],
                "updatedAt": record.get("updatedAt", record["createdAt"]),
            }
            for record in reversed(self._records.values())
        ]


def _timestamp() -> str:
    return datetime.now(UTC).isoformat(timespec="seconds")


def _valid_timestamp(value: Any) -> str:
    if not isinstance(value, str) or not value.strip():
        return ""
    try:
        datetime.fromisoformat(value.strip())
    except ValueError:
        return ""
    return value.strip()


def _event_key(event: dict[str, Any], index: int) -> str:
    data = event.get("data") if isinstance(event.get("data"), dict) else {}
    return str(
        event.get("id")
        or data.get("callId")
        or f"{data.get('requestKind', 'event')}:{data.get('name', event.get('step', 'event'))}:{index}"
    )
