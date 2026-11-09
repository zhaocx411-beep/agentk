from types import SimpleNamespace

from narrato_agentk.agentk_core import ActivityNames
from narrato_agentk.settings import Settings
from narrato_agentk.workflows import documentary


async def _quiet_progress(*args, **kwargs):
    return None


async def test_generate_workflow_uses_core_queue_and_retries_invalid_script(monkeypatch):
    calls = []
    generated = 0

    async def execute_activity(name, payload, **options):
        nonlocal generated
        calls.append((name, payload, options))
        if name == ActivityNames.PROBE_VIDEO:
            return {
                "fileSizeBytes": 100,
                "durationSeconds": 65,
                "videoStreams": [{}],
            }
        if name == ActivityNames.UNDERSTAND_VIDEO_SEGMENT:
            return {"text": f"window {payload['startSeconds']}"}
        if name == ActivityNames.GENERATE_TEXT:
            generated += 1
            if generated == 1:
                return {
                    "result": {
                        "version": "1",
                        "sourceVideoUri": "https://cdn.test/source.mp4",
                        "sourceDurationSeconds": 65,
                        "theme": "自然",
                        "segments": [
                            {
                                "id": "segment-001",
                                "sourceStartSeconds": 66,
                                "sourceEndSeconds": 67,
                                "narration": "越界旁白",
                                "ost": 2,
                            }
                        ],
                    }
                }
            return {
                "result": {
                    "version": "1",
                    "sourceVideoUri": "https://cdn.test/source.mp4",
                    "sourceDurationSeconds": 65,
                    "theme": "自然",
                    "segments": [
                        {
                            "id": "ignored",
                            "sourceStartSeconds": 0,
                            "sourceEndSeconds": 10,
                            "narration": "旁白",
                            "ost": 0,
                        }
                    ],
                }
            }
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    monkeypatch.setattr(documentary, "report_progress", _quiet_progress)
    documentary.configure(Settings(analysis_window_seconds=30, analysis_concurrency=3))

    output = await documentary.GenerateDocumentaryScriptWorkflow().run(
        {"videoUri": "https://cdn.test/source.mp4", "theme": "自然"}
    )

    assert output["segments"][0]["id"] == "segment-001"
    assert output["segments"][0]["ost"] == 2
    assert generated == 2
    assert len([item for item in calls if item[0] == ActivityNames.UNDERSTAND_VIDEO_SEGMENT]) == 3
    assert all(item[2]["task_queue"] == "agentk" for item in calls)


async def test_render_workflow_batches_nine_segments_and_concats(monkeypatch):
    calls = []
    counters = {"tts": 0, "process": 0, "compose": 0}

    async def execute_activity(name, payload, **options):
        calls.append((name, payload, options))
        if name == ActivityNames.PROBE_VIDEO:
            return {
                "fileSizeBytes": 100,
                "durationSeconds": 20,
                "videoStreams": [{}],
            }
        if name == ActivityNames.GENERATE_SPEECH_QWEN:
            counters["tts"] += 1
            index = counters["tts"]
            return {
                "artifacts": [{"type": "audio", "uri": f"https://cdn.test/a{index}.mp3"}],
                "metadata": {"audioLengthMs": 1000},
            }
        if name == ActivityNames.PROCESS_VIDEO:
            counters["process"] += 1
            index = counters["process"]
            return {"artifacts": [{"type": "video", "uri": f"https://cdn.test/c{index}.mp4"}]}
        if name == ActivityNames.COMPOSE_VIDEO:
            counters["compose"] += 1
            index = counters["compose"]
            return {"artifacts": [{"type": "video", "uri": f"https://cdn.test/b{index}.mp4"}]}
        if name == ActivityNames.CONCAT_VIDEOS:
            return {"artifacts": [{"type": "video", "uri": "https://cdn.test/final.mp4"}]}
        raise AssertionError(name)

    monkeypatch.setattr(documentary.workflow, "execute_activity", execute_activity)
    monkeypatch.setattr(documentary, "report_progress", _quiet_progress)
    documentary.configure(Settings(tts_concurrency=4))
    segments = [
        {
            "id": f"segment-{index + 1:03d}",
            "sourceStartSeconds": index * 2,
            "sourceEndSeconds": index * 2 + 1,
            "narration": f"旁白 {index + 1}",
            "ost": 0 if index == 0 else 2,
        }
        for index in range(9)
    ]
    output = await documentary.RenderDocumentaryVideoWorkflow().run(
        {
            "script": {
                "version": "1",
                "sourceVideoUri": "https://cdn.test/source.mp4",
                "sourceDurationSeconds": 20,
                "theme": "自然",
                "segments": segments,
            },
            "voiceId": "Cherry",
            "aspect": "9:16",
        }
    )

    assert output["artifacts"][0]["uri"] == "https://cdn.test/final.mp4"
    assert output["durationSeconds"] == 9
    assert counters == {"tts": 9, "process": 9, "compose": 2}
    process_calls = [item for item in calls if item[0] == ActivityNames.PROCESS_VIDEO]
    assert process_calls[0][1]["operations"][1] == {"type": "mute"}
    assert process_calls[0][1]["operations"][2]["type"] == "reframe"
    compose_calls = [item for item in calls if item[0] == ActivityNames.COMPOSE_VIDEO]
    assert [len(item[1]["videoUrls"]) for item in compose_calls] == [8, 1]
    concat_calls = [item for item in calls if item[0] == ActivityNames.CONCAT_VIDEOS]
    assert len(concat_calls) == 1
    assert concat_calls[0][1] == {
        "videoUrls": ["https://cdn.test/b1.mp4", "https://cdn.test/b2.mp4"],
        "size": "auto",
    }
    assert concat_calls[0][2]["task_queue"] == "agentk"


async def test_create_workflow_passes_output_to_render_and_propagates_cancellation(monkeypatch):
    calls = []

    def info():
        return SimpleNamespace(workflow_id="parent")

    async def execute_child_workflow(name, payload, **options):
        calls.append((name, payload, options))
        if name == documentary.GENERATE_WORKFLOW_TYPE:
            return {"sourceVideoUri": payload["videoUri"], "segments": []}
        return {"ok": True}

    monkeypatch.setattr(documentary.workflow, "info", info)
    monkeypatch.setattr(documentary.workflow, "execute_child_workflow", execute_child_workflow)
    documentary.configure(Settings())
    output = await documentary.CreateDocumentaryVideoWorkflow().run(
        {"videoUri": "https://cdn.test/source.mp4", "voiceId": "Cherry"}
    )

    assert output == {"ok": True}
    assert calls[1][1]["script"] == {
        "sourceVideoUri": "https://cdn.test/source.mp4",
        "segments": [],
    }
    assert calls[0][1] == {
        "videoUri": "https://cdn.test/source.mp4",
        "theme": "",
        "instructions": "",
    }
    assert calls[0][2]["id"] == "parent/script"
    assert calls[0][2]["task_queue"] == "contrib-narrato-documentary-v1"
    assert calls[0][2]["execution_timeout"].total_seconds() == 3 * 60 * 60
    assert calls[1][1] == {
        "script": {"sourceVideoUri": "https://cdn.test/source.mp4", "segments": []},
        "voiceId": "Cherry",
        "aspect": "source",
        "narrationVolume": 1.0,
        "subtitles": True,
    }
    assert calls[1][2]["id"] == "parent/render"
    assert calls[1][2]["task_queue"] == "contrib-narrato-documentary-v1"
    assert calls[1][2]["execution_timeout"].total_seconds() == 21 * 60 * 60
    assert all(
        item[2]["cancellation_type"]
        == documentary.workflow.ChildWorkflowCancellationType.WAIT_CANCELLATION_COMPLETED
        for item in calls
    )
