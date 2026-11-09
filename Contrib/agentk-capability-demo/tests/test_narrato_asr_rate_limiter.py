from agentk_capability_demo.agent100.narrato.workflows.asr_rate_limiter import (
    ASRRateLimiterWorkflow,
)


def _request(request_id: str, workflow_id: str = "workflow-1") -> dict[str, str]:
    return {
        "requestId": request_id,
        "workflowId": workflow_id,
        "runId": f"run-{workflow_id}",
    }


def test_rate_limiter_queues_requests_once_and_grants_fifo():
    limiter = ASRRateLimiterWorkflow()
    limiter._configure(
        {
            "minIntervalSeconds": 11,
            "globalConcurrency": 1,
            "leaseTimeoutSeconds": 900,
        }
    )

    limiter.request_permit(_request("request-1"))
    limiter.request_permit(_request("request-1"))
    limiter.request_permit(_request("request-2", "workflow-2"))

    first = limiter._grant_next(100)
    assert first["requestId"] == "request-1"
    assert [item["requestId"] for item in limiter._pending] == ["request-2"]
    assert limiter._next_allowed_timestamp == 111
    assert limiter._leases["request-1"]["expiresAt"] == 1000


def test_rate_limiter_requires_release_and_minimum_start_interval():
    limiter = ASRRateLimiterWorkflow()
    limiter._configure(
        {
            "minIntervalSeconds": 11,
            "globalConcurrency": 1,
            "leaseTimeoutSeconds": 900,
        }
    )
    limiter.request_permit(_request("request-1"))
    limiter.request_permit(_request("request-2", "workflow-2"))
    limiter._grant_next(100)

    assert not limiter._has_capacity()
    limiter.release_permit({"requestId": "request-1"})
    assert limiter._has_capacity()
    assert limiter._seconds_until_rate_slot(105) == 6
    assert not limiter._can_grant(105)
    assert limiter._can_grant(111)


def test_rate_limiter_reaps_expired_leases():
    limiter = ASRRateLimiterWorkflow()
    limiter._configure(
        {
            "minIntervalSeconds": 11,
            "globalConcurrency": 1,
            "leaseTimeoutSeconds": 60,
        }
    )
    limiter.request_permit(_request("request-1"))
    limiter._grant_next(100)

    assert limiter._reap_expired_leases(159) == 0
    assert limiter._reap_expired_leases(160) == 1
    assert limiter._has_capacity()


def test_rate_limiter_restores_continue_as_new_state():
    limiter = ASRRateLimiterWorkflow()
    limiter._configure(
        {
            "minIntervalSeconds": 12,
            "globalConcurrency": 2,
            "leaseTimeoutSeconds": 120,
            "pending": [_request("request-2", "workflow-2")],
            "leases": {
                "request-1": {
                    **_request("request-1"),
                    "expiresAt": 220,
                }
            },
            "nextAllowedTimestamp": 112,
            "grantsInRun": 999,
        }
    )

    assert limiter._min_interval_seconds == 12
    assert limiter._global_concurrency == 2
    assert limiter._lease_timeout_seconds == 120
    assert limiter._pending[0]["requestId"] == "request-2"
    assert limiter._leases["request-1"]["expiresAt"] == 220
    assert limiter._next_allowed_timestamp == 112
    assert limiter._grants_in_run == 999
