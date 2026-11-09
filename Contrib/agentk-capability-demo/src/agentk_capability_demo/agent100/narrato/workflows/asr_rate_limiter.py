from __future__ import annotations

from typing import Any

from temporalio import workflow

ASR_RATE_LIMITER_WORKFLOW_TYPE = "narrato.asr.rate_limiter.v1"
ASR_RATE_LIMIT_REQUEST_SIGNAL = "request_asr_permit"
ASR_RATE_LIMIT_RELEASE_SIGNAL = "release_asr_permit"
ASR_PERMIT_GRANTED_SIGNAL = "asr_permit_granted"

_CONTINUE_AS_NEW_AFTER_GRANTS = 1000


@workflow.defn(name=ASR_RATE_LIMITER_WORKFLOW_TYPE)
class ASRRateLimiterWorkflow:
    """Namespace-wide FIFO limiter for one shared ASR provider deployment."""

    def __init__(self) -> None:
        self._min_interval_seconds = 11.0
        self._global_concurrency = 1
        self._lease_timeout_seconds = 900.0
        self._pending: list[dict[str, str]] = []
        self._leases: dict[str, dict[str, Any]] = {}
        self._next_allowed_timestamp = 0.0
        self._grants_in_run = 0

    def _configure(self, state: dict[str, Any]) -> None:
        self._min_interval_seconds = float(state.get("minIntervalSeconds", 11.0))
        self._global_concurrency = int(state.get("globalConcurrency", 1))
        self._lease_timeout_seconds = float(state.get("leaseTimeoutSeconds", 900.0))
        self._next_allowed_timestamp = float(state.get("nextAllowedTimestamp", 0.0))
        self._grants_in_run = int(state.get("grantsInRun", 0))

        restored_pending = [
            dict(item) for item in state.get("pending", []) if isinstance(item, dict)
        ]
        restored_leases = {
            str(request_id): dict(value)
            for request_id, value in (state.get("leases") or {}).items()
            if isinstance(value, dict)
        }
        # Preserve signals that arrive around a continue-as-new handoff while
        # restoring the carried queue and leases.
        for item in self._pending:
            request_id = str(item.get("requestId") or "")
            if request_id and not any(
                request_id == str(restored.get("requestId") or "")
                for restored in restored_pending
            ):
                restored_pending.append(item)
        restored_leases.update(self._leases)
        self._pending = restored_pending
        self._leases = restored_leases

    @workflow.signal(name=ASR_RATE_LIMIT_REQUEST_SIGNAL)
    def request_permit(self, request: dict[str, Any]) -> None:
        request_id = str(request.get("requestId") or "").strip()
        workflow_id = str(request.get("workflowId") or "").strip()
        run_id = str(request.get("runId") or "").strip()
        if not request_id or not workflow_id:
            return
        if request_id in self._leases or any(
            request_id == item["requestId"] for item in self._pending
        ):
            return
        self._pending.append(
            {"requestId": request_id, "workflowId": workflow_id, "runId": run_id}
        )

    @workflow.signal(name=ASR_RATE_LIMIT_RELEASE_SIGNAL)
    def release_permit(self, release: dict[str, Any]) -> None:
        request_id = str(release.get("requestId") or "").strip()
        if request_id:
            self._leases.pop(request_id, None)

    def _has_capacity(self) -> bool:
        return len(self._leases) < self._global_concurrency

    def _seconds_until_rate_slot(self, now: float) -> float:
        return max(0.0, self._next_allowed_timestamp - now)

    def _can_grant(self, now: float) -> bool:
        return bool(self._pending) and self._has_capacity() and not self._seconds_until_rate_slot(now)

    def _grant_next(self, now: float) -> dict[str, str]:
        request = self._pending.pop(0)
        request_id = request["requestId"]
        self._leases[request_id] = {
            **request,
            "expiresAt": now + self._lease_timeout_seconds,
        }
        self._next_allowed_timestamp = now + self._min_interval_seconds
        self._grants_in_run += 1
        return request

    def _reap_expired_leases(self, now: float) -> int:
        expired = [
            request_id
            for request_id, lease in self._leases.items()
            if float(lease["expiresAt"]) <= now
        ]
        for request_id in expired:
            self._leases.pop(request_id, None)
        return len(expired)

    def _continue_state(self) -> dict[str, Any]:
        return {
            "minIntervalSeconds": self._min_interval_seconds,
            "globalConcurrency": self._global_concurrency,
            "leaseTimeoutSeconds": self._lease_timeout_seconds,
            "pending": self._pending,
            "leases": self._leases,
            "nextAllowedTimestamp": self._next_allowed_timestamp,
            "grantsInRun": 0,
        }

    @workflow.run
    async def run(self, initial_state: dict[str, Any]) -> None:
        self._configure(initial_state)
        while True:
            now = workflow.now().timestamp()
            self._reap_expired_leases(now)

            if self._can_grant(now):
                request = self._grant_next(now)
                handle = workflow.get_external_workflow_handle(
                    request["workflowId"], run_id=request["runId"] or None
                )
                try:
                    await handle.signal(ASR_PERMIT_GRANTED_SIGNAL, request["requestId"])
                except Exception:
                    # The requesting workflow may have completed or been canceled.
                    self._leases.pop(request["requestId"], None)
                if (
                    self._grants_in_run >= _CONTINUE_AS_NEW_AFTER_GRANTS
                    and workflow.all_handlers_finished()
                ):
                    workflow.continue_as_new(self._continue_state())
                continue

            if self._pending and self._has_capacity():
                await workflow.sleep(self._seconds_until_rate_slot(now))
                continue

            if self._pending and self._leases:
                next_expiry = min(float(item["expiresAt"]) for item in self._leases.values())
                try:
                    await workflow.wait_condition(
                        self._has_capacity,
                        timeout=max(0.001, next_expiry - now),
                    )
                except TimeoutError:
                    pass
                continue

            if self._leases:
                next_expiry = min(float(item["expiresAt"]) for item in self._leases.values())
                try:
                    await workflow.wait_condition(
                        lambda: bool(self._pending) or not self._leases,
                        timeout=max(0.001, next_expiry - now),
                    )
                except TimeoutError:
                    pass
                continue

            await workflow.wait_condition(lambda: bool(self._pending))
