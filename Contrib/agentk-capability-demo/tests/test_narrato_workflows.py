from types import SimpleNamespace

import pytest
from temporalio.exceptions import ActivityError, ApplicationError

from agentk_capability_demo.agent100.narrato.agentk_core import CoreTemporalTypes
from agentk_capability_demo.agent100.narrato.contracts import (
    DOCUMENTARY_SCRIPT_SCHEMA,
    NARRATIVE_PLAN_SCHEMA,
    SCRIPT_GENERATION_SCHEMA,
)
from agentk_capability_demo.agent100.narrato.settings import Settings
from agentk_capability_demo.agent100.narrato.workflows import documentary


@pytest.fixture(autouse=True)
def _enable_opening_cadence_patch(monkeypatch):
    monkeypatch.setattr(
        documentary.workflow,
        "patched",
        lambda patch_id: (
            patch_id
            in {
                documentary.OPENING_CADENCE_PATCH_ID,
                documentary.VIDEO_PRECLIP_PATCH_ID,
                documentary.POST_EVENT_NARRATION_PATCH_ID,
            }
        ),
    )


async def _quiet_progress(*args, **kwargs):
    return None


def _profile():
    return {
        "primaryGenre": "悬疑片",
        "secondaryGenres": ["动作片"],
        "thesis": "人物为何冒险进入废弃车站。",
        "hook": "列车早已停运，站台上为什么还有脚步声？",
        "style": "紧凑、具体、逐步揭示",
        "pacing": "开场快，重要对白处留白",
        "globalTtsInstruction": "低沉克制，保留悬念。",
    }


def _plan(duration=20.0):
    return {
        **_profile(),
        "researchNeeded": False,
        "researchReason": "原创场景",
        "researchQueries": [],
        "storyBeats": [
            {
                "id": "beat-001",
                "startSeconds": 0,
                "endSeconds": duration,
                "summary": "人物进入废弃车站并寻找声音来源。",
                "narrationPurpose": "建立危机并推进调查",
                "importance": 5,
                "evidenceWindowIndexes": [0],
            }
        ],
    }


def _script(duration=20.0, start=1.0, end=15.0, *, emotion="fearful"):
    return {
        "version": "2",
        "sourceVideoUri": "https://cdn.test/source.mp4",
        "sourceDurationSeconds": duration,
        "narrativeProfile": _profile(),
        "narrationCues": [
            {
                "id": "cue-001",
                "startSeconds": start,
                "endSeconds": end,
                "evidenceEndSeconds": start,
                "narration": "停运的车站里，突然响起了第二个人的脚步。",
                "evidenceWindowIndexes": [0],
                "ttsInstruction": "前半句压低，脚步二字加重。",
                "importance": 5,
                "emotion": emotion,
            }
        ],
    }


def _probe(duration=20.0, *, audio=False):
    return {
        "fileSizeBytes": 100,
        "durationSeconds": duration,
        "videoStreams": [{"width": 1080, "height": 1920}],
        "audioStreams": [{}] if audio else [],
    }


def _activity_error(
    activity_type: str,
    *,
    error_type: str = "Transient",
    non_retryable: bool = False,
) -> ActivityError:
    error = ActivityError(
        f"{activity_type} failed",
        scheduled_event_id=1,
        started_event_id=2,
        identity="core-worker",
        activity_type=activity_type,
        activity_id="test",
        retry_state=None,
    )
    error.__cause__ = ApplicationError(
        "provider failure", type=error_type, non_retryable=non_retryable
    )
    return error


def test_opening_cadence_uses_stable_patch_id_without_renaming_workflow(monkeypatch):
    patch_ids = []
    monkeypatch.setattr(
        documentary.workflow,
        "patched",
        lambda patch_id: patch_ids.append(patch_id) or False,
    )

    assert documentary._opening_cadence_enabled() is False
    assert patch_ids == ["narrato-opening-cadence-2026-08-27"]
    assert documentary.GENERATE_WORKFLOW_TYPE == "narrato.documentary.generate_script.v2"


def test_positioning_hook_uses_independent_stable_patch_id(monkeypatch):
    patch_ids = []
    monkeypatch.setattr(
        documentary.workflow,
        "patched",
        lambda patch_id: patch_ids.append(patch_id) or False,
    )

    assert documentary._positioning_hook_enabled() is False
    assert patch_ids == ["narrato-positioning-hook-2026-08-27"]


def test_legacy_cadence_keeps_eighteen_second_retry_boundary():
    at_boundary = _script(32, 4, 14)
    over_boundary = _script(32.001, 4, 14)

    assert (
        documentary._cadence_gaps(
            at_boundary,
            [],
            opening_cadence=False,
        )
        == []
    )
    over_gap = documentary._cadence_gaps(
        over_boundary,
        [],
        opening_cadence=False,
    )
    assert over_gap == [
        {
            "startSeconds": 14.0,
            "endSeconds": 32.001,
            "durationSeconds": 18.001,
        }
    ]
    assert documentary._cadence_warning(over_gap, opening_cadence=False) == (
        "非对白画面存在超过18秒的连续无解说区间：14.000-32.001秒"
    )


def test_fallback_script_preserves_legacy_opening_and_enables_new_hook():
    plan = _plan(20)
    plan["storyBeats"][0]["startSeconds"] = 4
    assert plan["hook"] != plan["storyBeats"][0]["summary"]

    legacy = documentary._fallback_script(
        plan,
        video_uri="https://cdn.test/source.mp4",
        duration_seconds=20,
        window_count=1,
        opening_cadence=False,
    )
    current = documentary._fallback_script(
        plan,
        video_uri="https://cdn.test/source.mp4",
        duration_seconds=20,
        window_count=1,
        opening_cadence=True,
    )

    assert legacy["narrationCues"][0]["startSeconds"] == 4
    assert legacy["narrationCues"][0]["narration"] == plan["storyBeats"][0]["summary"]
    assert current["narrationCues"][0]["startSeconds"] == 0
    assert current["narrationCues"][0]["narration"] == plan["hook"]


def test_opening_hook_insertion_respects_five_hundred_cue_limit():
    plan = _plan(1004)
    value = _script(1004, 4, 5)
    value["narrationCues"] = [
        {
            "id": f"cue-{index:03d}",
            "startSeconds": 4 + 2 * (index - 1),
            "endSeconds": 5 + 2 * (index - 1),
            "narration": f"原解说{index}",
            "evidenceWindowIndexes": [0],
            "ttsInstruction": "自然清晰。",
            "importance": 3,
            "emotion": "calm",
        }
        for index in range(1, 501)
    ]

    result = documentary._ensure_opening_hook(value, plan, window_count=1)

    assert len(result["narrationCues"]) == 500
    assert result["narrationCues"][0]["narration"] == plan["hook"]
    assert result["narrationCues"][1]["narration"] == "原解说1"
    assert result["narrationCues"][-1]["narration"] == "原解说499"


async def test_generate_normal_path_has_only_plan_and_script_llm_stages(monkeypatch):
    calls = []

    async def execute_activity(name, payload, **options):
        calls.append((name, payload, options))
        if name == CoreTemporalTypes.PROBE_VIDEO:
            return _probe()
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": "人物走进废弃车站，远处出现另一个影子。"}
        if name == CoreTemporalTypes.GENERATE_TEXT:
            if payload["outputSchema"] == NARRATIVE_PLAN_SCHEMA:
                return {"result": _plan()}
            assert payload["outputSchema"] == SCRIPT_GENERATION_SCHEMA
            return {"result": _script()}
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    monkeypatch.setattr(documentary, "report_progress", _quiet_progress)
    documentary.configure(Settings(analysis_window_seconds=30))

    output = await documentary.GenerateDocumentaryScriptWorkflow().run(
        {
            "videoUri": "https://cdn.test/source.mp4",
            "instructions": "不要剧透",
            "analysisOptions": {"asrEnabled": False},
        }
    )

    llm_calls = [item for item in calls if item[0] == CoreTemporalTypes.GENERATE_TEXT]
    assert [item[1]["outputSchema"] for item in llm_calls] == [
        NARRATIVE_PLAN_SCHEMA,
        SCRIPT_GENERATION_SCHEMA,
    ]
    assert output["version"] == "2"
    assert output["narrativeProfile"]["primaryGenre"] == "悬疑片"
    assert output["narrationCues"][0]["id"] == "cue-001"
    assert "每一段ASR" in llm_calls[1][1]["prompt"]
    assert all(item[2]["task_queue"] == "agentk" for item in calls)


async def test_legacy_history_keeps_old_prompts_and_does_not_inject_opening_hook(
    monkeypatch,
):
    calls = []
    patch_ids = []

    async def execute_activity(name, payload, **options):
        calls.append((name, payload, options))
        if name == CoreTemporalTypes.PROBE_VIDEO:
            return _probe(20)
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": "人物走进废弃车站，远处出现另一个影子。"}
        if name == CoreTemporalTypes.GENERATE_TEXT:
            if payload["outputSchema"] == NARRATIVE_PLAN_SCHEMA:
                return {"result": _plan(20)}
            return {"result": _script(20, 4, 14)}
        raise AssertionError(name)

    monkeypatch.setattr(
        documentary.workflow,
        "patched",
        lambda patch_id: patch_ids.append(patch_id) or False,
    )
    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    monkeypatch.setattr(documentary, "report_progress", _quiet_progress)
    documentary.configure(Settings(analysis_window_seconds=30))

    output = await documentary.GenerateDocumentaryScriptWorkflow().run(
        {
            "videoUri": "https://cdn.test/source.mp4",
            "analysisOptions": {"asrEnabled": False},
        }
    )

    llm_calls = [item for item in calls if item[0] == CoreTemporalTypes.GENERATE_TEXT]
    assert patch_ids == [
        documentary.OPENING_CADENCE_PATCH_ID,
        documentary.POSITIONING_HOOK_PATCH_ID,
        documentary.VIDEO_PRECLIP_PATCH_ID,
        documentary.POST_EVENT_NARRATION_PATCH_ID,
    ]
    assert len(llm_calls) == 2
    assert all("最长约6秒" in call[1]["prompt"] for call in llm_calls)
    assert all("0-3秒" not in call[1]["prompt"] for call in llm_calls)
    assert output["narrationCues"][0]["startSeconds"] == 4
    assert output["narrationCues"][0]["narration"] != _plan(20)["hook"]
    assert "pacingWarning" not in output


async def test_sparse_script_retries_twice_then_returns_pacing_warning(monkeypatch):
    script_calls = []
    progress = []

    async def record_progress(*args):
        progress.append(args)

    async def execute_activity(name, payload, **options):
        if name == CoreTemporalTypes.PROBE_VIDEO:
            return _probe(60)
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": "人物在空旷道路上持续前行。"}
        if name == CoreTemporalTypes.GENERATE_TEXT:
            if payload["outputSchema"] == NARRATIVE_PLAN_SCHEMA:
                plan = _plan(60)
                plan["storyBeats"][0]["evidenceWindowIndexes"] = [0, 1]
                return {"result": plan}
            script_calls.append(payload)
            value = _script(60, 30, 32, emotion="calm")
            value["narrationCues"][0]["evidenceWindowIndexes"] = [0, 1]
            return {"result": value}
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    monkeypatch.setattr(documentary, "report_progress", record_progress)
    documentary.configure(Settings(analysis_window_seconds=30))

    output = await documentary.GenerateDocumentaryScriptWorkflow().run(
        {
            "videoUri": "https://cdn.test/source.mp4",
            "analysisOptions": {"asrEnabled": False},
        }
    )

    assert len(script_calls) == 3
    assert "problemGaps" in script_calls[1]["prompt"]
    assert "超过5秒" in script_calls[1]["prompt"]
    assert "不要为了补开场、覆盖率或缩短空档而提前剧情" in script_calls[1]["prompt"]
    assert "pacingWarning" in output
    assert "5秒" in output["pacingWarning"]
    assert any(event[2].get("pacingWarning") for event in progress)


async def test_late_opening_is_preserved_when_it_follows_evidence(monkeypatch):
    script_calls = []

    async def execute_activity(name, payload, **options):
        if name == CoreTemporalTypes.PROBE_VIDEO:
            return _probe(20)
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": "人物走进废弃车站，远处出现另一个影子。"}
        if name == CoreTemporalTypes.GENERATE_TEXT:
            if payload["outputSchema"] == NARRATIVE_PLAN_SCHEMA:
                return {"result": _plan(20)}
            script_calls.append(payload)
            return {"result": _script(20, 4, 15)}
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    monkeypatch.setattr(documentary, "report_progress", _quiet_progress)

    output = await documentary.GenerateDocumentaryScriptWorkflow().run(
        {
            "videoUri": "https://cdn.test/source.mp4",
            "analysisOptions": {"asrEnabled": False},
        }
    )

    assert len(script_calls) == 1
    assert output["narrationCues"][0]["startSeconds"] == 4
    assert output["narrationCues"][0]["evidenceEndSeconds"] == 4
    assert output["narrationCues"][0]["narration"] != _plan(20)["hook"]
    assert output["narrationCues"][0]["importance"] == 5
    assert "pacingWarning" not in output


async def test_timing_issues_retry_with_previous_script_then_use_last_result(monkeypatch):
    script_calls = []

    async def execute_activity(name, payload, **options):
        if name == CoreTemporalTypes.PROBE_VIDEO:
            return _probe(20)
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": "[0.000-4.000] 人物走进废弃车站。"}
        if name == CoreTemporalTypes.GENERATE_TEXT:
            if payload["outputSchema"] == NARRATIVE_PLAN_SCHEMA:
                return {"result": _plan(20)}
            script_calls.append(payload)
            value = _script(20, 4, 15)
            value["narrationCues"][0].pop("evidenceEndSeconds")
            return {"result": value}
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    monkeypatch.setattr(documentary, "report_progress", _quiet_progress)

    output = await documentary.GenerateDocumentaryScriptWorkflow().run(
        {
            "videoUri": "https://cdn.test/source.mp4",
            "analysisOptions": {"asrEnabled": False},
        }
    )

    assert len(script_calls) == 3
    assert "timingIssues=" in script_calls[1]["prompt"]
    assert "missingEvidenceEnd" in script_calls[1]["prompt"]
    assert "previousScript=" in script_calls[1]["prompt"]
    assert "停运的车站里" in script_calls[1]["prompt"]
    assert output["narrationCues"][0]["startSeconds"] == 4
    assert "evidenceEndSeconds" not in output["narrationCues"][0]
    assert "时间轴软检查仍有1项建议" in output["pacingWarning"]


async def test_generate_falls_back_to_structurally_valid_v2_script(monkeypatch):
    progress = []

    async def record_progress(*args):
        progress.append(args)

    async def execute_activity(name, payload, **options):
        if name == CoreTemporalTypes.PROBE_VIDEO:
            return _probe(5)
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": "一只猫走进画面。"}
        if name == CoreTemporalTypes.GENERATE_TEXT:
            raise _activity_error(name)
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    monkeypatch.setattr(documentary, "report_progress", record_progress)

    output = await documentary.GenerateDocumentaryScriptWorkflow().run(
        {
            "videoUri": "https://cdn.test/source.mp4",
            "analysisOptions": {"asrEnabled": False},
        }
    )

    assert output["version"] == "2"
    assert output["narrationCues"][0]["startSeconds"] == 4.5
    assert output["narrationCues"][0]["evidenceEndSeconds"] == 4.5
    assert output["narrationCues"][0]["narration"] == "一只猫走进画面。"
    assert {event[0] for event in progress if event[2].get("degraded")} >= {
        "plan_narrative",
        "generate_script",
    }


async def test_generate_workflow_acquires_and_releases_global_asr_lease(monkeypatch):
    workflow_instance = documentary.GenerateDocumentaryScriptWorkflow()
    requests = []
    releases = []

    async def wait_condition(predicate, **options):
        assert predicate()

    class Handle:
        async def signal(self, name, payload):
            if name == documentary.ASR_RATE_LIMIT_REQUEST_SIGNAL:
                requests.append((name, payload))
                workflow_instance.grant_asr_permit(payload["requestId"])
            else:
                releases.append((name, payload))

    monkeypatch.setattr(
        documentary.workflow,
        "info",
        lambda: SimpleNamespace(workflow_id="generate-1", run_id="run-1"),
    )
    monkeypatch.setattr(documentary.workflow, "wait_condition", wait_condition)
    monkeypatch.setattr(
        documentary.workflow, "get_external_workflow_handle", lambda workflow_id: Handle()
    )
    settings = Settings(asr_rate_limiter_workflow_id="staging-asr-limiter")

    assert await workflow_instance._acquire_asr_permit("permit-1", settings) is True
    await workflow_instance._release_asr_permit("permit-1", settings)
    assert requests[0][1] == {
        "requestId": "permit-1",
        "workflowId": "generate-1",
        "runId": "run-1",
    }
    assert releases == [(documentary.ASR_RATE_LIMIT_RELEASE_SIGNAL, {"requestId": "permit-1"})]


@pytest.mark.parametrize("error_type", ["InvalidArgument", "Configuration", "InvalidCoreOutput"])
def test_deterministic_activity_failures_are_not_degradable(error_type):
    assert not documentary._is_degradable_activity_error(
        _activity_error("test", error_type=error_type, non_retryable=True)
    )


def test_model_cannot_write_the_pacing_warning_channel():
    """pacingWarning 是工作流事后算出的批注，不能让模型自评填进去。"""

    assert "pacingWarning" not in SCRIPT_GENERATION_SCHEMA["properties"]
    assert SCRIPT_GENERATION_SCHEMA["additionalProperties"] is False
    # 产出契约仍然保留该字段，只是改由工作流填。
    assert "pacingWarning" in DOCUMENTARY_SCRIPT_SCHEMA["properties"]
    # 除这一个字段外两份 schema 必须完全一致，否则模型产出会不满足对外契约。
    assert SCRIPT_GENERATION_SCHEMA["required"] == DOCUMENTARY_SCRIPT_SCHEMA["required"]
    assert set(DOCUMENTARY_SCRIPT_SCHEMA["properties"]) - set(
        SCRIPT_GENERATION_SCHEMA["properties"]
    ) == {"pacingWarning"}


def test_transient_activity_failures_are_degradable():
    assert documentary._is_degradable_activity_error(_activity_error("test"))


def _upstream_rejection(name):
    """上游偶发拒绝：AgentK 标成不可重试，但并非我们的入参或配置有问题。"""

    error = _activity_error(name)
    error.__cause__ = ApplicationError(
        "HTTP 400: Request contains an invalid argument.",
        type="UpstreamRequestRejected",
        non_retryable=True,
    )
    return error


async def test_upstream_rejection_degrades_one_window_instead_of_failing_the_run(monkeypatch):
    """一个窗口被上游拒绝时退回 ASR 证据，不能拖垮整条流水线。"""

    async def activity(name, payload, **options):
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT:
            raise _upstream_rejection(name)
        raise AssertionError(name)

    monkeypatch.setattr(documentary, "_activity", activity)
    result = await documentary._understand_video_with_clip_fallback(
        {
            "videoUrl": "https://cdn.test/source.mp4",
            "startSeconds": 30.0,
            "endSeconds": 60.0,
        }
    )
    assert result is None


async def test_upstream_rejection_of_the_clipped_call_also_degrades(monkeypatch):
    async def activity(name, payload, **options):
        if name == CoreTemporalTypes.PROCESS_VIDEO:
            return {"artifacts": [{"type": "video", "uri": "https://cdn.test/clip.mp4"}]}
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT:
            raise _upstream_rejection(name)
        raise AssertionError(name)

    monkeypatch.setattr(documentary, "_activity", activity)
    result = await documentary._understand_video_with_clip_fallback(
        {
            "videoUrl": "https://cdn.test/source.mp4",
            "startSeconds": 30.0,
            "endSeconds": 60.0,
        },
        clip_first=True,
    )
    assert result is None


@pytest.mark.parametrize("error_type", ["InvalidArgument", "Configuration", "InvalidCoreOutput"])
async def test_contract_and_config_failures_still_abort_the_run(monkeypatch, error_type):
    """入参或配置错误必须继续炸出来，不能被降级吞掉。"""

    async def activity(name, payload, **options):
        error = _activity_error(name)
        error.__cause__ = ApplicationError("bad input", type=error_type, non_retryable=True)
        raise error

    monkeypatch.setattr(documentary, "_activity", activity)
    with pytest.raises(ActivityError):
        await documentary._understand_video_with_clip_fallback(
            {
                "videoUrl": "https://cdn.test/source.mp4",
                "startSeconds": 30.0,
                "endSeconds": 60.0,
            }
        )


async def test_video_understanding_trims_clip_after_inline_payload_413(monkeypatch):
    calls = []

    async def activity(name, payload, **options):
        calls.append((name, payload, options))
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT and len(calls) == 1:
            error = _activity_error(name)
            error.__cause__ = ApplicationError("HTTP 413: Payload Too Large", type="Transient")
            raise error
        if name == CoreTemporalTypes.PROCESS_VIDEO:
            return {"artifacts": [{"type": "video", "uri": "https://cdn.test/clip.mp4"}]}
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": "片段中人物走进房间。"}
        raise AssertionError(name)

    monkeypatch.setattr(documentary, "_activity", activity)
    result = await documentary._understand_video_with_clip_fallback(
        {
            "model": "gemini-3.1-flash-lite",
            "videoUrl": "https://cdn.test/source.mp4",
            "startSeconds": 30.0,
            "endSeconds": 60.0,
            "fps": 1,
            "prompt": "描述画面",
        }
    )
    assert result == {"text": "片段中人物走进房间。"}
    assert calls[1][1]["operations"] == [
        {"type": "trim", "startSeconds": 30.0, "durationSeconds": 30.0}
    ]


async def test_video_understanding_pretrims_known_oversized_source(monkeypatch):
    calls = []

    async def activity(name, payload, **options):
        calls.append((name, payload, options))
        if name == CoreTemporalTypes.PROCESS_VIDEO:
            return {"artifacts": [{"type": "video", "uri": "https://cdn.test/clip.mp4"}]}
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": "片段中人物走进房间。"}
        raise AssertionError(name)

    monkeypatch.setattr(documentary, "_activity", activity)
    result = await documentary._understand_video_with_clip_fallback(
        {
            "videoUrl": "https://cdn.test/source.mp4",
            "startSeconds": 30.0,
            "endSeconds": 60.0,
        },
        clip_first=True,
    )
    assert result == {"text": "片段中人物走进房间。"}
    assert [call[0] for call in calls] == [
        CoreTemporalTypes.PROCESS_VIDEO,
        CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT,
    ]


def _render_activity_fake(calls, expected_tts_activity):
    counters = {"process": 0}

    async def execute_activity(name, payload, **options):
        calls.append((name, payload, options))
        if name == CoreTemporalTypes.PROBE_VIDEO:
            return _probe(10)
        if name == expected_tts_activity:
            return {
                "artifacts": [{"type": "audio", "uri": "https://cdn.test/narration.mp3"}],
                "metadata": {"audioLengthMs": 2000},
            }
        if name == CoreTemporalTypes.PROCESS_VIDEO:
            counters["process"] += 1
            return {
                "artifacts": [
                    {"type": "video", "uri": f"https://cdn.test/clip-{counters['process']}.mp4"}
                ]
            }
        if name == CoreTemporalTypes.COMPOSE_VIDEO:
            return {"artifacts": [{"type": "video", "uri": "https://cdn.test/final.mp4"}]}
        raise AssertionError(name)

    return execute_activity


async def test_qwen_payload_combines_global_and_per_cue_instruction(monkeypatch):
    calls = []
    monkeypatch.setattr(
        documentary.workflow,
        "execute_activity",
        _render_activity_fake(calls, CoreTemporalTypes.GENERATE_SPEECH_QWEN),
    )
    monkeypatch.setattr(documentary, "report_progress", _quiet_progress)
    documentary.configure(Settings())

    output = await documentary.RenderDocumentaryVideoWorkflow().run(
        {
            "script": _script(10, 2, 5),
            "tts": {
                "provider": "qwen",
                "model": "qwen3-tts-instruct-flash",
                "voiceId": "Cherry",
            },
        }
    )

    tts_request = next(
        item[1] for item in calls if item[0] == CoreTemporalTypes.GENERATE_SPEECH_QWEN
    )
    assert tts_request["instructions"] == "低沉克制，保留悬念。 前半句压低，脚步二字加重。"
    assert output["durationSeconds"] == 10
    process_requests = [item[1] for item in calls if item[0] == CoreTemporalTypes.PROCESS_VIDEO]
    assert [
        (item["operations"][0]["startSeconds"], item["operations"][0]["durationSeconds"])
        for item in process_requests
    ] == [
        (0.0, 2.0),
        (2.0, 2.0),
        (4.0, 6.0),
    ]


async def test_qwen_flash_payload_omits_unsupported_instructions(monkeypatch):
    calls = []
    monkeypatch.setattr(
        documentary.workflow,
        "execute_activity",
        _render_activity_fake(calls, CoreTemporalTypes.GENERATE_SPEECH_QWEN),
    )
    monkeypatch.setattr(documentary, "report_progress", _quiet_progress)
    documentary.configure(Settings())

    await documentary.RenderDocumentaryVideoWorkflow().run(
        {
            "script": _script(10, 2, 5),
            "tts": {"provider": "qwen", "model": "qwen3-tts-flash", "voiceId": "Cherry"},
        }
    )

    request = next(item[1] for item in calls if item[0] == CoreTemporalTypes.GENERATE_SPEECH_QWEN)
    assert "instructions" not in request


async def test_minimax_payload_has_emotion_and_never_instruction(monkeypatch):
    calls = []
    monkeypatch.setattr(
        documentary.workflow,
        "execute_activity",
        _render_activity_fake(calls, CoreTemporalTypes.GENERATE_SPEECH_MINIMAX),
    )
    monkeypatch.setattr(documentary, "report_progress", _quiet_progress)
    documentary.configure(Settings())

    await documentary.RenderDocumentaryVideoWorkflow().run(
        {
            "script": _script(10, 2, 5, emotion="happy"),
            "tts": {
                "provider": "minimax",
                "model": "minimax-speech-2.8-hd",
                "voiceId": "Chinese (Mandarin)_Lyrical_Voice",
            },
        }
    )
    request = next(
        item[1] for item in calls if item[0] == CoreTemporalTypes.GENERATE_SPEECH_MINIMAX
    )
    assert request["emotion"] == "happy"
    assert "instructions" not in request
    assert "languageType" not in request


async def test_minimax_incompatible_whisper_is_omitted_with_warning(monkeypatch):
    calls = []
    progress = []

    async def record_progress(*args):
        progress.append(args)

    monkeypatch.setattr(
        documentary.workflow,
        "execute_activity",
        _render_activity_fake(calls, CoreTemporalTypes.GENERATE_SPEECH_MINIMAX),
    )
    monkeypatch.setattr(documentary, "report_progress", record_progress)
    documentary.configure(Settings())
    output = await documentary.RenderDocumentaryVideoWorkflow().run(
        {
            "script": _script(10, 2, 5, emotion="whisper"),
            "tts": {
                "provider": "minimax",
                "model": "minimax-speech-2.8-hd",
                "voiceId": "Chinese (Mandarin)_Lyrical_Voice",
            },
        }
    )
    request = next(
        item[1] for item in calls if item[0] == CoreTemporalTypes.GENERATE_SPEECH_MINIMAX
    )
    assert "emotion" not in request
    assert "不兼容" in output["warnings"][0]
    assert any(event[0] == "resolve_timeline" for event in progress)


async def test_create_workflow_passes_v2_requests_to_children(monkeypatch):
    calls = []

    monkeypatch.setattr(
        documentary.workflow,
        "info",
        lambda: SimpleNamespace(workflow_id="parent"),
    )

    async def execute_child_workflow(name, payload, **options):
        calls.append((name, payload, options))
        return _script() if name == documentary.GENERATE_WORKFLOW_TYPE else {"ok": True}

    monkeypatch.setattr(documentary.workflow, "execute_child_workflow", execute_child_workflow)
    monkeypatch.setattr(documentary, "report_progress", _quiet_progress)
    documentary.configure(Settings())
    tts = {"provider": "qwen", "model": "qwen3-tts-flash", "voiceId": "Cherry"}
    output = await documentary.CreateDocumentaryVideoWorkflow().run(
        {"videoUri": "https://cdn.test/source.mp4", "tts": tts}
    )

    assert output == {"ok": True}
    assert "theme" not in calls[0][1]
    assert calls[1][1]["tts"] == tts
    assert "models" not in calls[1][1]
    assert calls[0][2]["id"] == "parent/script"
    assert calls[1][2]["id"] == "parent/render"
