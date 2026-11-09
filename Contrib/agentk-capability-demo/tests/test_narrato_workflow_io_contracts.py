import pytest
from temporalio.exceptions import ActivityError, ApplicationError

from agentk_capability_demo.agent100.narrato.agentk_core import CoreTemporalTypes
from agentk_capability_demo.agent100.narrato.contracts import (
    NARRATIVE_PLAN_SCHEMA,
    SCRIPT_GENERATION_SCHEMA,
)
from agentk_capability_demo.agent100.narrato.workflows import documentary


@pytest.fixture(autouse=True)
def _enable_opening_cadence_patch(monkeypatch):
    monkeypatch.setattr(
        documentary.workflow,
        "patched",
        lambda patch_id: patch_id == documentary.OPENING_CADENCE_PATCH_ID,
    )


async def _quiet(*args, **kwargs):
    return None


def _profile():
    return {
        "primaryGenre": "纪录片",
        "secondaryGenres": [],
        "thesis": "记录河谷水位变化。",
        "hook": "一夜之间，河岸为什么退后了十米？",
        "style": "事实优先，简洁有张力",
        "pacing": "舒缓但不拖沓",
        "globalTtsInstruction": "沉稳清晰，数据处稍加重。",
    }


def _plan(duration=10.0, *, research=False):
    return {
        **_profile(),
        "researchNeeded": research,
        "researchReason": "核实公开地理背景" if research else "仅需原片证据",
        "researchQueries": ["河谷 水位 公开资料", "河岸 退缩 原因"] if research else [],
        "storyBeats": [
            {
                "id": "beat-001",
                "startSeconds": 0,
                "endSeconds": duration,
                "summary": "河谷水位下降，露出新的河岸。",
                "narrationPurpose": "建立变化并解释关键结果",
                "importance": 5,
                "evidenceWindowIndexes": [0],
            }
        ],
    }


def _script(duration=10.0, *, emotion="calm"):
    return {
        "version": "2",
        "sourceVideoUri": "https://cdn.test/source.mp4",
        "sourceDurationSeconds": duration,
        "narrativeProfile": _profile(),
        "narrationCues": [
            {
                "id": "cue-001",
                "startSeconds": 2,
                "endSeconds": 5,
                "narration": "水位退去后，原本隐藏的河岸第一次完整露出。",
                "evidenceWindowIndexes": [0],
                "ttsInstruction": "先平稳陈述，第一次三个字加重。",
                "importance": 5,
                "emotion": emotion,
            }
        ],
    }


def _probe(duration=10.0, *, audio=False):
    return {
        "fileSizeBytes": 100,
        "durationSeconds": duration,
        "videoStreams": [{"width": 1920, "height": 1080}],
        "audioStreams": [{}] if audio else [],
    }


def _transient_error(name):
    error = ActivityError(
        f"{name} failed",
        scheduled_event_id=1,
        started_event_id=2,
        identity="core",
        activity_type=name,
        activity_id="test",
        retry_state=None,
    )
    error.__cause__ = ApplicationError("temporary", type="Transient")
    return error


async def test_plan_can_request_research_without_adding_an_llm_stage(monkeypatch):
    calls = []
    query_count = 0

    async def execute_activity(name, payload, **options):
        nonlocal query_count
        calls.append((name, payload, options))
        if name == CoreTemporalTypes.PROBE_VIDEO:
            return _probe()
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": "河水退去，岸边出现清晰水痕。"}
        if name == CoreTemporalTypes.GENERATE_TEXT:
            if payload["outputSchema"] == NARRATIVE_PLAN_SCHEMA:
                return {"result": _plan(research=True)}
            assert payload["outputSchema"] == SCRIPT_GENERATION_SCHEMA
            return {"result": _script()}
        if name == CoreTemporalTypes.WEB_SEARCH:
            query_count += 1
            if query_count == 2:
                raise _transient_error(name)
            return {
                "results": [
                    {
                        "title": "公开资料",
                        "url": "https://example.test/river",
                        "summary": "水位变化背景",
                    }
                ]
            }
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    monkeypatch.setattr(documentary, "report_progress", _quiet)
    output = await documentary.GenerateDocumentaryScriptWorkflow().run(
        {
            "videoUri": "https://cdn.test/source.mp4",
            "analysisOptions": {"asrEnabled": False},
        }
    )
    assert output["version"] == "2"
    assert len([call for call in calls if call[0] == CoreTemporalTypes.WEB_SEARCH]) == 2
    assert [
        call[1]["outputSchema"] for call in calls if call[0] == CoreTemporalTypes.GENERATE_TEXT
    ] == [NARRATIVE_PLAN_SCHEMA, SCRIPT_GENERATION_SCHEMA]


async def test_script_structure_is_repaired_at_most_three_times_then_falls_back(monkeypatch):
    script_calls = []

    async def execute_activity(name, payload, **options):
        if name == CoreTemporalTypes.PROBE_VIDEO:
            return _probe()
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": "河岸出现在画面中。"}
        if name == CoreTemporalTypes.GENERATE_TEXT:
            if payload["outputSchema"] == NARRATIVE_PLAN_SCHEMA:
                return {"result": _plan()}
            script_calls.append(payload)
            return {"result": {"version": "2", "narrationCues": "bad"}}
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    monkeypatch.setattr(documentary, "report_progress", _quiet)
    output = await documentary.GenerateDocumentaryScriptWorkflow().run(
        {
            "videoUri": "https://cdn.test/source.mp4",
            "analysisOptions": {"asrEnabled": False},
        }
    )
    assert len(script_calls) == 3
    assert "校验错误" in script_calls[1]["prompt"]
    assert output["version"] == "2"
    assert output["narrationCues"]


async def test_asr_payload_stays_on_source_timeline_and_informs_pacing(monkeypatch):
    calls = []

    async def execute_activity(name, payload, **options):
        calls.append((name, payload, options))
        if name == CoreTemporalTypes.PROBE_VIDEO:
            return _probe(audio=True)
        if name == CoreTemporalTypes.EXTRACT_AUDIO:
            return {
                "artifacts": [{"type": "audio", "uri": "https://cdn.test/source.mp3"}],
                "durationSeconds": 10,
            }
        if name == CoreTemporalTypes.TRANSCRIBE_AUDIO:
            return {
                "text": "水退了",
                "segments": [{"start": 6, "end": 9, "text": "水退了"}],
            }
        if name == CoreTemporalTypes.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": "河岸逐渐露出。"}
        if name == CoreTemporalTypes.GENERATE_TEXT:
            return {
                "result": _plan() if payload["outputSchema"] == NARRATIVE_PLAN_SCHEMA else _script()
            }
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    monkeypatch.setattr(documentary, "report_progress", _quiet)
    output = await documentary.GenerateDocumentaryScriptWorkflow().run(
        {"videoUri": "https://cdn.test/source.mp4"}
    )
    asr = next(call for call in calls if call[0] == CoreTemporalTypes.TRANSCRIBE_AUDIO)
    assert asr[1] == {
        "model": "azure-whisper",
        "audioUrl": "https://cdn.test/source.mp3",
        "task": "transcribe",
    }
    assert "pacingWarning" not in output


def _render_fake(calls, *, source_audio=False, tts_duration=True, bad_process=False):
    source_probed = False
    process_index = 0

    async def execute_activity(name, payload, **options):
        nonlocal source_probed, process_index
        calls.append((name, payload, options))
        if name == CoreTemporalTypes.PROBE_VIDEO:
            if not source_probed:
                source_probed = True
                return _probe(audio=source_audio)
            return {"durationSeconds": 2.5}
        if name in {
            CoreTemporalTypes.GENERATE_SPEECH_QWEN,
            CoreTemporalTypes.GENERATE_SPEECH_MINIMAX,
        }:
            return {
                "artifacts": [{"type": "audio", "uri": "https://cdn.test/tts.mp3"}],
                "metadata": {"audioLengthMs": 2500} if tts_duration else {},
            }
        if name == CoreTemporalTypes.EXTRACT_AUDIO:
            return {"artifacts": [{"type": "audio", "uri": "https://cdn.test/original.mp3"}]}
        if name == CoreTemporalTypes.PROCESS_VIDEO:
            if bad_process:
                return {"artifacts": []}
            process_index += 1
            return {
                "artifacts": [
                    {"type": "video", "uri": f"https://cdn.test/part-{process_index}.mp4"}
                ]
            }
        if name == CoreTemporalTypes.COMPOSE_VIDEO:
            return {"artifacts": [{"type": "video", "uri": "https://cdn.test/final.mp4"}]}
        raise AssertionError(name)

    return execute_activity


async def test_qwen_missing_duration_probes_exact_tts_artifact(monkeypatch):
    calls = []
    monkeypatch.setattr(
        documentary.workflow,
        "execute_activity",
        _render_fake(calls, tts_duration=False),
    )
    monkeypatch.setattr(documentary, "report_progress", _quiet)
    output = await documentary.RenderDocumentaryVideoWorkflow().run(
        {
            "script": _script(),
            "tts": {"provider": "qwen", "model": "qwen3-tts-flash", "voiceId": "Cherry"},
        }
    )
    probes = [call[1]["videoUrl"] for call in calls if call[0] == CoreTemporalTypes.PROBE_VIDEO]
    assert probes == ["https://cdn.test/source.mp4", "https://cdn.test/tts.mp3"]
    assert output["durationSeconds"] == 10


async def test_original_audio_is_only_extracted_and_ducked_during_narration(monkeypatch):
    calls = []
    monkeypatch.setattr(
        documentary.workflow,
        "execute_activity",
        _render_fake(calls, source_audio=True),
    )
    monkeypatch.setattr(documentary, "report_progress", _quiet)
    await documentary.RenderDocumentaryVideoWorkflow().run(
        {
            "script": _script(),
            "tts": {"provider": "qwen", "model": "qwen3-tts-flash", "voiceId": "Cherry"},
            "originalAudioVolume": 0.3,
        }
    )
    extracts = [call[1] for call in calls if call[0] == CoreTemporalTypes.EXTRACT_AUDIO]
    assert extracts == [
        {
            "videoUrl": "https://cdn.test/source.mp4",
            "startSeconds": 2.0,
            "durationSeconds": 2.5,
            "format": "mp3",
            "bitRate": "128k",
            "sampleRate": 48000,
            "channels": 2,
            "normalizeLoudness": False,
        }
    ]
    compose = next(call[1] for call in calls if call[0] == CoreTemporalTypes.COMPOSE_VIDEO)
    assert [track["volume"] for track in compose["audioTracks"]] == [0.3, 1.0]
    assert compose["preserveOriginalAudio"] is True


async def test_minimax_fluent_is_forwarded_for_26_models(monkeypatch):
    calls = []
    monkeypatch.setattr(
        documentary.workflow,
        "execute_activity",
        _render_fake(calls),
    )
    monkeypatch.setattr(documentary, "report_progress", _quiet)
    await documentary.RenderDocumentaryVideoWorkflow().run(
        {
            "script": _script(emotion="fluent"),
            "tts": {
                "provider": "minimax",
                "model": "minimax-speech-2.6-turbo",
                "voiceId": "Chinese (Mandarin)_Lyrical_Voice",
            },
        }
    )
    request = next(
        call[1] for call in calls if call[0] == CoreTemporalTypes.GENERATE_SPEECH_MINIMAX
    )
    assert request["emotion"] == "fluent"
    assert "instructions" not in request


async def test_source_duration_mismatch_is_invalid_argument(monkeypatch):
    async def execute_activity(name, payload, **options):
        assert name == CoreTemporalTypes.PROBE_VIDEO
        return _probe(12)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    monkeypatch.setattr(documentary, "report_progress", _quiet)
    with pytest.raises(ApplicationError, match="does not match") as raised:
        await documentary.RenderDocumentaryVideoWorkflow().run(
            {
                "script": _script(10),
                "tts": {
                    "provider": "qwen",
                    "model": "qwen3-tts-flash",
                    "voiceId": "Cherry",
                },
            }
        )
    assert raised.value.type == "InvalidArgument"


async def test_invalid_process_artifact_is_real_runtime_error(monkeypatch):
    calls = []
    monkeypatch.setattr(
        documentary.workflow,
        "execute_activity",
        _render_fake(calls, bad_process=True),
    )
    monkeypatch.setattr(documentary, "report_progress", _quiet)
    with pytest.raises(ApplicationError, match="no artifact") as raised:
        await documentary.RenderDocumentaryVideoWorkflow().run(
            {
                "script": _script(),
                "tts": {
                    "provider": "qwen",
                    "model": "qwen3-tts-flash",
                    "voiceId": "Cherry",
                },
            }
        )
    assert raised.value.type == "InvalidCoreOutput"
