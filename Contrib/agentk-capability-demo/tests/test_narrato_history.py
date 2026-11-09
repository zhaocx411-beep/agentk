from agentk_capability_demo.agent100.narrato.history import NarratoExecutionHistory


def test_history_keeps_recent_snapshots_and_returns_copies():
    history = NarratoExecutionHistory(limit=2)
    history.add(
        "documentary_script",
        "first",
        {"sessionId": "ses_1", "executionId": "exe_1", "status": "running"},
        {"pipeline": "documentary_script", "args": {"videoUri": "https://cdn.test/a.mp4"}},
    )
    history.update(
        "ses_1",
        "exe_1",
        {"status": "completed", "events": [{"type": "finished"}], "output": {"version": "2"}},
    )
    history.add(
        "documentary_video",
        "second",
        {"sessionId": "ses_2", "executionId": "exe_2", "status": "running"},
    )

    cached = history.get("ses_1", "exe_1")
    assert cached is not None
    assert cached["output"] == {"version": "2"}
    assert cached["events"][0]["observedAt"]
    assert cached["request"]["args"]["videoUri"] == "https://cdn.test/a.mp4"
    cached["output"]["version"] = "mutated"
    assert history.get("ses_1", "exe_1")["output"] == {"version": "2"}
    assert [item["executionId"] for item in history.list()] == ["exe_2", "exe_1"]

    history.add(
        "documentary_render",
        "third",
        {"sessionId": "ses_3", "executionId": "exe_3", "status": "running"},
    )
    assert history.get("ses_1", "exe_1") is None
    assert [item["executionId"] for item in history.list()] == ["exe_3", "exe_2"]


def test_history_preserves_event_times_across_repeated_snapshots():
    history = NarratoExecutionHistory()
    history.add(
        "documentary_script",
        "timed",
        {"sessionId": "ses_1", "executionId": "exe_1", "status": "running"},
    )
    history.update(
        "ses_1",
        "exe_1",
        {"status": "running", "events": [{"id": "evt_1", "type": "progress"}]},
    )
    first_time = history.get("ses_1", "exe_1")["events"][0]["observedAt"]
    history.update(
        "ses_1",
        "exe_1",
        {"status": "running", "events": [{"id": "evt_1", "type": "progress"}]},
    )
    assert history.get("ses_1", "exe_1")["events"][0]["observedAt"] == first_time


def test_history_prefers_execution_and_native_event_times_when_available():
    history = NarratoExecutionHistory()
    history.add(
        "documentary_script",
        "timed",
        {
            "sessionId": "ses_1",
            "executionId": "exe_1",
            "status": "running",
            "createdAt": "2026-08-27T04:14:59+00:00",
        },
    )
    history.update(
        "ses_1",
        "exe_1",
        {
            "status": "running",
            "events": [
                {"id": "native", "type": "progress", "createdAt": "2026-08-27T04:15:31+00:00"},
                {"id": "invalid", "type": "progress", "createdAt": "not-a-time"},
            ],
        },
    )

    record = history.get("ses_1", "exe_1")
    assert record["createdAt"] == "2026-08-27T04:14:59+00:00"
    assert "observedAt" not in record["events"][0]
    assert record["events"][1]["observedAt"]


def test_history_tracks_cancel_request_until_execution_is_terminal():
    history = NarratoExecutionHistory()
    history.add(
        "documentary_video",
        "cancel me",
        {"sessionId": "ses_1", "executionId": "exe_1", "status": "running"},
    )

    cancelling = history.mark_cancel_requested("ses_1", "exe_1")

    assert cancelling is not None
    assert cancelling["cancelRequested"] is True
    assert cancelling["cancelRequestedAt"]
    assert history.list()[0]["cancelRequested"] is True

    history.update("ses_1", "exe_1", {"status": "cancelled", "events": []})
    assert history.list()[0]["cancelRequested"] is False


def test_history_cancel_request_requires_an_exact_record():
    history = NarratoExecutionHistory()
    assert history.mark_cancel_requested("missing", "missing") is None


def test_history_rejects_missing_execution_identity():
    history = NarratoExecutionHistory()

    try:
        history.add("documentary_script", "bad", {"status": "running"})
    except ValueError as error:
        assert "sessionId and executionId" in str(error)
    else:
        raise AssertionError("missing execution identity must be rejected")
