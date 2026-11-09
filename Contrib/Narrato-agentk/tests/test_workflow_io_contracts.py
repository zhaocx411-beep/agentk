from __future__ import annotations

import asyncio
from typing import Any

import pytest
from temporalio.exceptions import ApplicationError

from narrato_agentk.agentk_core import ActivityNames
from narrato_agentk.contracts import DOCUMENTARY_SCRIPT_SCHEMA, MAX_VIDEO_BYTES
from narrato_agentk.settings import Settings
from narrato_agentk.workflows import documentary

SOURCE_URI = "https://cdn.test/source.mp4"


def _script(segments: list[dict[str, Any]], *, duration: float = 20) -> dict[str, Any]:
    return {
        "version": "1",
        "sourceVideoUri": SOURCE_URI,
        "sourceDurationSeconds": duration,
        "theme": "自然",
        "segments": segments,
    }


def _probe(*, duration: float = 20, size: int = 100) -> dict[str, Any]:
    return {
        "fileSizeBytes": size,
        "durationSeconds": duration,
        "videoStreams": [{"codec": "h264"}],
    }


def _tts(uri: str, length_ms: int | None = 1000) -> dict[str, Any]:
    metadata = {} if length_ms is None else {"audioLengthMs": length_ms}
    return {
        "artifacts": [{"type": "audio", "uri": uri}],
        "metadata": metadata,
    }


def _artifact(uri: str) -> dict[str, Any]:
    return {"artifacts": [{"type": "video", "uri": uri, "params": {"codec": "h264"}}]}


def _install_progress_recorder(monkeypatch: pytest.MonkeyPatch) -> list[tuple[Any, ...]]:
    progress: list[tuple[Any, ...]] = []

    async def report(*args: Any) -> None:
        progress.append(args)

    monkeypatch.setattr(documentary, "report_progress", report)
    return progress


async def test_generate_workflow_checks_every_core_input_and_final_output(monkeypatch):
    calls: list[tuple[str, dict[str, Any], dict[str, Any]]] = []
    progress = _install_progress_recorder(monkeypatch)

    async def execute_activity(name: str, payload: dict[str, Any], **options: Any) -> Any:
        calls.append((name, payload, options))
        if name == ActivityNames.PROBE_VIDEO:
            return _probe(duration=35)
        if name == ActivityNames.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": f"分析窗口，模型声称时间为 999，实际起点 {payload['startSeconds']}"}
        if name == ActivityNames.GENERATE_TEXT:
            return {
                "result": {
                    "version": "1",
                    "sourceVideoUri": "https://model.invalid/wrong.mp4",
                    "sourceDurationSeconds": 999,
                    "theme": "模型擅自改写",
                    "segments": [
                        {
                            "id": "segment-999",
                            "sourceStartSeconds": 2,
                            "sourceEndSeconds": 8,
                            "narration": "溪流穿过山谷。",
                            "ost": 0,
                        }
                    ],
                }
            }
        raise AssertionError(f"unexpected activity: {name}")

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    documentary.configure(
        Settings(
            vision_model="vision-fixed",
            text_model="text-fixed",
            analysis_window_seconds=30,
            analysis_concurrency=3,
        )
    )

    result = await documentary.GenerateDocumentaryScriptWorkflow().run(
        {
            "videoUri": SOURCE_URI,
            "theme": "河谷",
            "instructions": "语气克制",
        }
    )

    assert calls[0][0:2] == (ActivityNames.PROBE_VIDEO, {"videoUrl": SOURCE_URI})
    analysis_calls = [call for call in calls if call[0] == ActivityNames.UNDERSTAND_VIDEO_SEGMENT]
    assert [(call[1]["startSeconds"], call[1]["endSeconds"]) for call in analysis_calls] == [
        (0.0, 30.0),
        (30.0, 35.0),
    ]
    assert all(call[1]["model"] == "vision-fixed" for call in analysis_calls)
    assert all(call[1]["videoUrl"] == SOURCE_URI for call in analysis_calls)
    assert "视频主题：河谷" in analysis_calls[0][1]["prompt"]
    assert "补充要求：语气克制" in analysis_calls[0][1]["prompt"]

    generate_call = next(call for call in calls if call[0] == ActivityNames.GENERATE_TEXT)
    assert generate_call[1]["model"] == "text-fixed"
    assert generate_call[1]["outputSchema"] == DOCUMENTARY_SCRIPT_SCHEMA
    assert '"startSeconds":0.0' in generate_call[1]["prompt"]
    assert '"endSeconds":35.0' in generate_call[1]["prompt"]
    assert all(call[2]["task_queue"] == "agentk" for call in calls)

    assert result == {
        "version": "1",
        "sourceVideoUri": SOURCE_URI,
        "sourceDurationSeconds": 35.0,
        "theme": "河谷",
        "segments": [
            {
                "id": "segment-001",
                "sourceStartSeconds": 2.0,
                "sourceEndSeconds": 8.0,
                "narration": "溪流穿过山谷。",
                "ost": 2,
            }
        ],
    }
    assert [event[0] for event in progress] == [
        "validate_input",
        "analyze_video",
        "analyze_video",
        "generate_script",
        "complete",
    ]
    assert progress[2][2] == {"percent": 35, "completed": 2, "total": 2}


async def test_generate_workflow_limits_video_analysis_concurrency_to_three(monkeypatch):
    active = 0
    maximum_active = 0
    analysis_starts: list[float] = []
    _install_progress_recorder(monkeypatch)

    async def execute_activity(name, payload, **options):
        nonlocal active, maximum_active
        if name == ActivityNames.PROBE_VIDEO:
            return _probe(duration=210)
        if name == ActivityNames.UNDERSTAND_VIDEO_SEGMENT:
            analysis_starts.append(payload["startSeconds"])
            active += 1
            maximum_active = max(maximum_active, active)
            await asyncio.sleep(0)
            active -= 1
            return {"text": f"窗口 {payload['startSeconds']}"}
        if name == ActivityNames.GENERATE_TEXT:
            return {
                "result": {
                    "version": "1",
                    "sourceVideoUri": SOURCE_URI,
                    "sourceDurationSeconds": 210,
                    "theme": "",
                    "segments": [
                        {
                            "id": "segment-001",
                            "sourceStartSeconds": 0,
                            "sourceEndSeconds": 10,
                            "narration": "旁白",
                            "ost": 2,
                        }
                    ],
                }
            }
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    documentary.configure(Settings(analysis_window_seconds=30, analysis_concurrency=3))

    await documentary.GenerateDocumentaryScriptWorkflow().run({"videoUri": SOURCE_URI})

    assert maximum_active == 3
    assert analysis_starts == [0.0, 30.0, 60.0, 90.0, 120.0, 150.0, 180.0]


@pytest.mark.parametrize(
    ("probe_output", "message"),
    [
        ("not-an-object", "non-object"),
        (_probe(size=MAX_VIDEO_BYTES + 1), "video size"),
        (_probe(duration=0), "video duration"),
        ({"fileSizeBytes": 100, "durationSeconds": 5, "videoStreams": []}, "no video"),
    ],
)
async def test_generate_workflow_rejects_invalid_probe_output(monkeypatch, probe_output, message):
    _install_progress_recorder(monkeypatch)

    async def execute_activity(name, payload, **options):
        assert name == ActivityNames.PROBE_VIDEO
        return probe_output

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)

    with pytest.raises(ApplicationError, match=message) as captured:
        await documentary.GenerateDocumentaryScriptWorkflow().run({"videoUri": SOURCE_URI})

    assert captured.value.type == "InvalidArgument"
    assert captured.value.non_retryable is True


async def test_generate_workflow_rejects_empty_analysis_output(monkeypatch):
    _install_progress_recorder(monkeypatch)

    async def execute_activity(name, payload, **options):
        if name == ActivityNames.PROBE_VIDEO:
            return _probe(duration=5)
        if name == ActivityNames.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": "  "}
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)

    with pytest.raises(ApplicationError, match="returned no text") as captured:
        await documentary.GenerateDocumentaryScriptWorkflow().run({"videoUri": SOURCE_URI})

    assert captured.value.type == "InvalidModelOutput"
    assert captured.value.non_retryable is True


async def test_generate_workflow_stops_after_one_local_schema_retry(monkeypatch):
    generated = 0
    prompts: list[str] = []
    progress = _install_progress_recorder(monkeypatch)

    async def execute_activity(name, payload, **options):
        nonlocal generated
        if name == ActivityNames.PROBE_VIDEO:
            return _probe(duration=5)
        if name == ActivityNames.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": "一片森林"}
        if name == ActivityNames.GENERATE_TEXT:
            generated += 1
            prompts.append(payload["prompt"])
            return {
                "result": {
                    "version": "1",
                    "sourceVideoUri": SOURCE_URI,
                    "sourceDurationSeconds": 5,
                    "theme": "",
                    "segments": [
                        {
                            "id": "segment-001",
                            "sourceStartSeconds": 4,
                            "sourceEndSeconds": 6,
                            "narration": "越界",
                            "ost": 2,
                        }
                    ],
                }
            }
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)

    with pytest.raises(ApplicationError, match="exceeds source duration") as captured:
        await documentary.GenerateDocumentaryScriptWorkflow().run({"videoUri": SOURCE_URI})

    assert generated == 2
    assert "上一次输出未通过本地校验" not in prompts[0]
    assert "sourceEndSeconds exceeds source duration" in prompts[1]
    assert captured.value.type == "InvalidModelOutput"
    retry_event = next(event for event in progress if len(event) > 2 and event[2].get("attempt"))
    assert retry_event[2] == {"percent": 40, "attempt": 2}


async def test_render_workflow_checks_ost_payloads_timeline_and_single_batch_output(monkeypatch):
    calls: list[tuple[str, dict[str, Any], dict[str, Any]]] = []
    progress = _install_progress_recorder(monkeypatch)

    async def execute_activity(name: str, payload: dict[str, Any], **options: Any) -> Any:
        calls.append((name, payload, options))
        if name == ActivityNames.PROBE_VIDEO:
            return _probe(duration=20)
        if name == ActivityNames.GENERATE_SPEECH_QWEN:
            if payload["text"] == "静音原声并配音":
                return _tts("https://cdn.test/s0.mp3", 2000)
            return _tts("https://cdn.test/s2.mp3", 4000)
        if name == ActivityNames.PROCESS_VIDEO:
            start = payload["operations"][0]["startSeconds"]
            return _artifact(f"https://cdn.test/clip-{start}.mp4")
        if name == ActivityNames.COMPOSE_VIDEO:
            return _artifact("https://cdn.test/composed.mp4")
        if name == ActivityNames.CONCAT_VIDEOS:
            raise AssertionError("one compose batch must not call concat_videos")
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    documentary.configure(Settings(tts_model="tts-fixed", tts_concurrency=4))
    script = _script(
        [
            {
                "id": "segment-001",
                "sourceStartSeconds": 0,
                "sourceEndSeconds": 5,
                "narration": "静音原声并配音",
                "ost": 0,
            },
            {
                "id": "segment-002",
                "sourceStartSeconds": 5,
                "sourceEndSeconds": 8,
                "narration": "只保留原声",
                "ost": 1,
            },
            {
                "id": "segment-003",
                "sourceStartSeconds": 8,
                "sourceEndSeconds": 14,
                "narration": "原声和配音共存",
                "ost": 2,
            },
        ]
    )

    result = await documentary.RenderDocumentaryVideoWorkflow().run(
        {
            "script": script,
            "voiceId": "Cherry",
            "aspect": "1:1",
            "narrationVolume": 0.7,
            "subtitles": True,
        }
    )

    tts_calls = [call for call in calls if call[0] == ActivityNames.GENERATE_SPEECH_QWEN]
    assert [call[1] for call in tts_calls] == [
        {
            "model": "tts-fixed",
            "text": "静音原声并配音",
            "voiceId": "Cherry",
            "languageType": "Chinese",
        },
        {
            "model": "tts-fixed",
            "text": "原声和配音共存",
            "voiceId": "Cherry",
            "languageType": "Chinese",
        },
    ]

    process_calls = [call[1] for call in calls if call[0] == ActivityNames.PROCESS_VIDEO]
    assert process_calls == [
        {
            "videoUrl": SOURCE_URI,
            "operations": [
                {"type": "trim", "startSeconds": 0.0, "durationSeconds": 2.0},
                {"type": "mute"},
                {"type": "reframe", "width": 1080, "height": 1080, "fit": "cover"},
            ],
            "quality": "balanced",
        },
        {
            "videoUrl": SOURCE_URI,
            "operations": [
                {"type": "trim", "startSeconds": 5.0, "durationSeconds": 3.0},
                {"type": "reframe", "width": 1080, "height": 1080, "fit": "cover"},
            ],
            "quality": "balanced",
        },
        {
            "videoUrl": SOURCE_URI,
            "operations": [
                {"type": "trim", "startSeconds": 8.0, "durationSeconds": 4.0},
                {"type": "reframe", "width": 1080, "height": 1080, "fit": "cover"},
            ],
            "quality": "balanced",
        },
    ]

    compose = next(call[1] for call in calls if call[0] == ActivityNames.COMPOSE_VIDEO)
    assert compose == {
        "videoUrls": [
            "https://cdn.test/clip-0.0.mp4",
            "https://cdn.test/clip-5.0.mp4",
            "https://cdn.test/clip-8.0.mp4",
        ],
        "size": "auto",
        "audioTracks": [
            {
                "url": "https://cdn.test/s0.mp3",
                "startSeconds": 0.0,
                "durationSeconds": 2.0,
                "volume": 0.7,
            },
            {
                "url": "https://cdn.test/s2.mp3",
                "startSeconds": 5.0,
                "durationSeconds": 4.0,
                "volume": 0.7,
            },
        ],
        "preserveOriginalAudio": True,
        "subtitleSegments": [
            {"startSeconds": 0.0, "endSeconds": 2.0, "text": "静音原声并配音"},
            {"startSeconds": 5.0, "endSeconds": 9.0, "text": "原声和配音共存"},
        ],
    }
    assert result["durationSeconds"] == 9.0
    assert result["segmentCount"] == 3
    assert result["artifacts"][0] == {
        "type": "video",
        "uri": "https://cdn.test/composed.mp4",
        "params": {"codec": "h264"},
        "description": "纪录片解说成片",
    }
    assert [event[0] for event in progress] == [
        "validate_input",
        "synthesize_speech",
        "synthesize_speech",
        "cut_segments",
        "cut_segments",
        "compose_batches",
        "compose_batches",
        "finalize",
        "complete",
    ]


async def test_render_workflow_probes_tts_artifact_when_qwen_omits_duration(monkeypatch):
    calls: list[tuple[str, dict[str, Any]]] = []
    _install_progress_recorder(monkeypatch)
    audio_uri = "https://cdn.test/qwen.mp3"

    async def execute_activity(name, payload, **options):
        calls.append((name, payload))
        if name == ActivityNames.PROBE_VIDEO:
            if payload["videoUrl"] == SOURCE_URI:
                return _probe(duration=20)
            assert payload["videoUrl"] == audio_uri
            return {"durationSeconds": 1.234}
        if name == ActivityNames.GENERATE_SPEECH_QWEN:
            return _tts(audio_uri, None)
        if name == ActivityNames.PROCESS_VIDEO:
            return _artifact("https://cdn.test/clip.mp4")
        if name == ActivityNames.COMPOSE_VIDEO:
            return _artifact("https://cdn.test/final.mp4")
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    result = await documentary.RenderDocumentaryVideoWorkflow().run(
        {
            "script": _script(
                [
                    {
                        "id": "segment-001",
                        "sourceStartSeconds": 0,
                        "sourceEndSeconds": 5,
                        "narration": "真实时长来自音频探测",
                        "ost": 2,
                    }
                ]
            ),
            "voiceId": "Cherry",
            "subtitles": True,
        }
    )

    probes = [payload for name, payload in calls if name == ActivityNames.PROBE_VIDEO]
    assert probes == [{"videoUrl": SOURCE_URI}, {"videoUrl": audio_uri}]
    process = next(payload for name, payload in calls if name == ActivityNames.PROCESS_VIDEO)
    assert process["operations"][0]["durationSeconds"] == 1.234
    assert result["durationSeconds"] == 1.234


async def test_render_workflow_ost_one_skips_tts_and_subtitles(monkeypatch):
    calls: list[tuple[str, dict[str, Any]]] = []
    _install_progress_recorder(monkeypatch)

    async def execute_activity(name, payload, **options):
        calls.append((name, payload))
        if name == ActivityNames.PROBE_VIDEO:
            return _probe(duration=10)
        if name == ActivityNames.PROCESS_VIDEO:
            return _artifact("https://cdn.test/original.mp4")
        if name == ActivityNames.COMPOSE_VIDEO:
            return _artifact("https://cdn.test/final.mp4")
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    result = await documentary.RenderDocumentaryVideoWorkflow().run(
        {
            "script": _script(
                [
                    {
                        "id": "segment-001",
                        "sourceStartSeconds": 2,
                        "sourceEndSeconds": 6,
                        "narration": "原声段落",
                        "ost": 1,
                    }
                ],
                duration=10,
            ),
            "voiceId": "unused-but-required",
            "subtitles": True,
        }
    )

    assert ActivityNames.GENERATE_SPEECH_QWEN not in [name for name, _ in calls]
    process = next(payload for name, payload in calls if name == ActivityNames.PROCESS_VIDEO)
    assert process["operations"] == [{"type": "trim", "startSeconds": 2.0, "durationSeconds": 4.0}]
    compose = next(payload for name, payload in calls if name == ActivityNames.COMPOSE_VIDEO)
    assert compose["audioTracks"] == []
    assert compose["subtitleSegments"] == []
    assert compose["preserveOriginalAudio"] is True
    assert result["durationSeconds"] == 4.0


async def test_render_workflow_limits_tts_and_cut_concurrency_to_four(monkeypatch):
    active = {"tts": 0, "cut": 0}
    maximum = {"tts": 0, "cut": 0}
    output_index = {"tts": 0, "cut": 0, "compose": 0}
    _install_progress_recorder(monkeypatch)

    async def execute_activity(name, payload, **options):
        if name == ActivityNames.PROBE_VIDEO:
            return _probe(duration=30)
        if name == ActivityNames.GENERATE_SPEECH_QWEN:
            active["tts"] += 1
            maximum["tts"] = max(maximum["tts"], active["tts"])
            await asyncio.sleep(0)
            active["tts"] -= 1
            output_index["tts"] += 1
            return _tts(f"https://cdn.test/a{output_index['tts']}.mp3", 1000)
        if name == ActivityNames.PROCESS_VIDEO:
            active["cut"] += 1
            maximum["cut"] = max(maximum["cut"], active["cut"])
            await asyncio.sleep(0)
            active["cut"] -= 1
            output_index["cut"] += 1
            return _artifact(f"https://cdn.test/c{output_index['cut']}.mp4")
        if name == ActivityNames.COMPOSE_VIDEO:
            output_index["compose"] += 1
            return _artifact(f"https://cdn.test/b{output_index['compose']}.mp4")
        if name == ActivityNames.CONCAT_VIDEOS:
            return _artifact("https://cdn.test/final.mp4")
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    documentary.configure(Settings(tts_concurrency=4))
    segments = [
        {
            "id": f"segment-{index + 1:03d}",
            "sourceStartSeconds": index * 3,
            "sourceEndSeconds": index * 3 + 2,
            "narration": f"旁白 {index + 1}",
            "ost": 2,
        }
        for index in range(9)
    ]

    result = await documentary.RenderDocumentaryVideoWorkflow().run(
        {"script": _script(segments, duration=30), "voiceId": "Cherry"}
    )

    assert maximum == {"tts": 4, "cut": 4}
    assert result["segmentCount"] == 9


@pytest.mark.parametrize(
    ("failure_stage", "expected_type", "message"),
    [
        ("duration", "InvalidArgument", "does not match"),
        ("tts", "InvalidArgument", "measurable duration"),
        ("process", "InvalidCoreOutput", "no artifact"),
        ("compose", "InvalidCoreOutput", "no artifact"),
    ],
)
async def test_render_workflow_classifies_invalid_stage_outputs(
    monkeypatch, failure_stage, expected_type, message
):
    _install_progress_recorder(monkeypatch)

    async def execute_activity(name, payload, **options):
        if name == ActivityNames.PROBE_VIDEO:
            if failure_stage == "tts" and payload["videoUrl"] != SOURCE_URI:
                return {"durationSeconds": 0}
            return _probe(duration=11 if failure_stage == "duration" else 10)
        if name == ActivityNames.GENERATE_SPEECH_QWEN:
            return _tts("https://cdn.test/a.mp3", None if failure_stage == "tts" else 1000)
        if name == ActivityNames.PROCESS_VIDEO:
            return (
                {"artifacts": []}
                if failure_stage == "process"
                else _artifact("https://cdn.test/clip.mp4")
            )
        if name == ActivityNames.COMPOSE_VIDEO:
            return (
                {"artifacts": []}
                if failure_stage == "compose"
                else _artifact("https://cdn.test/final.mp4")
            )
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    raw_input = {
        "script": _script(
            [
                {
                    "id": "segment-001",
                    "sourceStartSeconds": 0,
                    "sourceEndSeconds": 5,
                    "narration": "旁白",
                    "ost": 2,
                }
            ],
            duration=10,
        ),
        "voiceId": "Cherry",
    }

    with pytest.raises(ApplicationError, match=message) as captured:
        await documentary.RenderDocumentaryVideoWorkflow().run(raw_input)

    assert captured.value.type == expected_type
    assert captured.value.non_retryable is True
