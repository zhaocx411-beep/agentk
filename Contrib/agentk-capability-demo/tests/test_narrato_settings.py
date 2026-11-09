from agentk_capability_demo.agent100.narrato.settings import Settings


def test_settings_worker_validation_accepts_model_defaults():
    settings = Settings()
    settings.validate_worker()
    assert settings.model_defaults() == {
        "vision": "gemini-3.1-flash-lite",
        "asr": "azure-whisper",
        "search": "volc-search",
        "story": "deepseek-v4-pro",
        "script": "deepseek-v4-pro",
    }
    assert settings.tts_defaults() == {
        "qwen": "qwen3-tts-flash",
        "minimax": "minimax-speech-2.8-hd",
    }
    assert settings.tts_model_options()["qwen"] == (
        "qwen3-tts-instruct-flash",
        "qwen3-tts-flash",
    )
    assert settings.asr_concurrency == 1
    assert settings.asr_min_interval_seconds == 11
    assert settings.asr_global_concurrency == 1
    assert settings.asr_lease_timeout_seconds == 900
    assert settings.asr_rate_limiter_workflow_id == "narrato-asr-global-rate-limiter-v1"


def test_settings_worker_rejects_invalid_concurrency_and_window():
    for settings, message in (
        (Settings(agentk_api_key="secret", analysis_window_seconds=0), "analysis_window"),
        (Settings(agentk_api_key="secret", analysis_concurrency=0), "analysis_concurrency"),
        (Settings(agentk_api_key="secret", asr_concurrency=0), "asr_concurrency"),
        (
            Settings(agentk_api_key="secret", asr_min_interval_seconds=0),
            "asr_min_interval_seconds",
        ),
        (
            Settings(agentk_api_key="secret", asr_global_concurrency=0),
            "asr_global_concurrency",
        ),
        (
            Settings(agentk_api_key="secret", asr_lease_timeout_seconds=10),
            "asr_lease_timeout_seconds",
        ),
        (
            Settings(agentk_api_key="secret", asr_rate_limiter_workflow_id=" "),
            "asr_rate_limiter_workflow_id",
        ),
        (Settings(agentk_api_key="secret", tts_concurrency=17), "tts_concurrency"),
    ):
        try:
            settings.validate_worker()
        except ValueError as error:
            assert message in str(error)
        else:
            raise AssertionError("expected invalid worker tuning to be rejected")


def test_settings_resolves_user_model_selection_against_deployment_allowlist():
    settings = Settings(
        text_model_candidates=("deepseek-v4-pro", "gpt-5.4"),
        vision_model_candidates=("gemini-3.1-flash-lite", "gemini-3.1-pro-preview"),
    )
    selected = settings.resolve_models({"vision": "gemini-3.1-pro-preview", "story": "gpt-5.4"})
    assert selected["vision"] == "gemini-3.1-pro-preview"
    assert selected["story"] == "gpt-5.4"
    assert selected["script"] == "deepseek-v4-pro"

    try:
        settings.resolve_models({"story": "not-deployed"})
    except ValueError as error:
        assert "models.story" in str(error)
    else:
        raise AssertionError("expected undeployed model to be rejected")


def test_settings_resolves_tts_model_per_provider():
    settings = Settings()
    assert settings.resolve_tts(
        {"provider": "qwen", "model": "qwen3-tts-flash", "voiceId": "Cherry"}
    )["provider"] == "qwen"
    assert settings.resolve_tts(
        {
            "provider": "minimax",
            "model": "minimax-speech-2.8-hd",
            "voiceId": "Chinese (Mandarin)_Lyrical_Voice",
        }
    )["provider"] == "minimax"
    try:
        settings.resolve_tts(
            {"provider": "minimax", "model": "qwen3-tts-flash", "voiceId": "x"}
        )
    except ValueError as error:
        assert "tts.model" in str(error)
    else:
        raise AssertionError("expected cross-provider TTS model to be rejected")


def test_settings_media_validation_names_missing_values():
    try:
        Settings().validate_media()
    except ValueError as error:
        assert "AGENTK_DEMO_BLOB_BUCKET" in str(error)
        assert "AGENTK_DEMO_BLOB_ENDPOINT" in str(error)
    else:
        raise AssertionError("expected media validation failure")


def test_settings_loads_shared_blob_configuration(monkeypatch):
    monkeypatch.setenv("AGENTK_DEMO_BLOB_BUCKET", "ylab-stunt")
    monkeypatch.setenv("AGENTK_DEMO_BLOB_ENDPOINT", "https://blob.test")

    settings = Settings.load()

    assert settings.upload_bucket == "ylab-stunt"
    assert settings.bs3_endpoint == "https://blob.test"
    assert settings.cdn_base == "https://blob.test/ylab-stunt"
    settings.validate_media()


def test_settings_loads_nested_yaml_and_env_takes_precedence(tmp_path, monkeypatch):
    config = tmp_path / "config.yaml"
    config.write_text(
        """
agentk:
  base_url: https://yaml.test
  api_key: yaml-key
models:
  vision: vision-yaml
  text: text-yaml
  asr: asr-yaml
  story: story-yaml
  script: script-yaml
  tts: tts-yaml
model_candidates:
  vision: [vision-yaml, vision-two]
  text: [text-yaml, text-two]
core_task_queue: agentk-yaml
temporal:
  address: 127.0.0.1:7233
  namespace: default
""".strip(),
        encoding="utf-8",
    )
    monkeypatch.setenv("NARRATO_CONFIG", str(config))
    monkeypatch.setenv("NARRATO_TEXT_MODEL", "text-env")
    monkeypatch.setenv("NARRATO_CORE_TASK_QUEUE", "agentk-local-e2e")
    monkeypatch.setenv("NARRATO_ASR_CONCURRENCY", "1")
    monkeypatch.setenv("NARRATO_ASR_MIN_INTERVAL_SECONDS", "12.5")
    monkeypatch.setenv("NARRATO_ASR_GLOBAL_CONCURRENCY", "2")
    monkeypatch.setenv("NARRATO_ASR_LEASE_TIMEOUT_SECONDS", "1200")
    monkeypatch.setenv("NARRATO_ASR_RATE_LIMITER_WORKFLOW_ID", "staging-asr-limiter")
    monkeypatch.setenv("NARRATO_WORKER_TASK_QUEUE", "narrato-v2-e2e")

    settings = Settings.load()

    assert settings.agentk_base_url == "https://yaml.test"
    assert settings.vision_model == "vision-yaml"
    assert settings.text_model == "text-env"
    assert settings.asr_model == "asr-yaml"
    assert settings.story_model == "story-yaml"
    assert settings.script_model == "script-yaml"
    assert settings.vision_model_candidates == ("vision-yaml", "vision-two")
    assert settings.text_model_candidates == ("text-yaml", "text-two")
    assert settings.core_task_queue == "agentk-local-e2e"
    assert settings.asr_concurrency == 1
    assert settings.asr_min_interval_seconds == 12.5
    assert settings.asr_global_concurrency == 2
    assert settings.asr_lease_timeout_seconds == 1200
    assert settings.asr_rate_limiter_workflow_id == "staging-asr-limiter"
    assert settings.worker_task_queue == "narrato-v2-e2e"
    assert settings.temporal_address == "127.0.0.1:7233"
    assert settings.temporal_namespace == "default"


def test_worker_rejects_empty_local_temporal_namespace():
    settings = Settings(
        agentk_api_key="secret",
        temporal_address="127.0.0.1:7233",
        temporal_namespace="",
    )

    try:
        settings.validate_worker()
    except ValueError as error:
        assert "temporal_namespace" in str(error)
    else:
        raise AssertionError("expected empty local Temporal namespace to be rejected")


def test_api_rejects_non_loopback_binding_without_access_token():
    try:
        Settings(api_host="0.0.0.0").validate_api()
    except ValueError as error:
        assert "NARRATO_CONSOLE_TOKEN" in str(error)
    else:
        raise AssertionError("expected insecure API binding to be rejected")


def test_api_accepts_loopback_or_authenticated_remote_binding():
    Settings(api_host="127.0.0.1").validate_api()
    Settings(api_host="0.0.0.0", console_token="secret").validate_api()
