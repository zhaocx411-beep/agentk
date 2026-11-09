from narrato_agentk.settings import Settings


def test_settings_worker_validation_accepts_model_defaults():
    Settings(agentk_api_key="secret").validate_worker()


def test_settings_media_validation_names_missing_values():
    try:
        Settings().validate_media()
    except ValueError as error:
        assert "NARRATO_UPLOAD_BUCKET" in str(error)
        assert "NARRATO_BS3_ENDPOINT" in str(error)
    else:
        raise AssertionError("expected media validation failure")


def test_settings_loads_nested_yaml_and_env_takes_precedence(tmp_path, monkeypatch):
    config = tmp_path / "config.yaml"
    config.write_text(
        """
agentk:
  base_url: https://yaml.test
  api_key: yaml-key
media:
  api_url: http://media.test
  upload_bucket: upload-bucket
  bs3_endpoint: http://bs3.test
  cdn_base: https://cdn.test
  key_prefix: custom-prefix
models:
  vision: vision-yaml
  text: text-yaml
  tts: tts-yaml
core_task_queue: agentk-yaml
""".strip(),
        encoding="utf-8",
    )
    monkeypatch.setenv("NARRATO_CONFIG", str(config))
    monkeypatch.setenv("NARRATO_TEXT_MODEL", "text-env")
    monkeypatch.setenv("NARRATO_CORE_TASK_QUEUE", "agentk-local-e2e")

    settings = Settings.load()

    assert settings.agentk_base_url == "https://yaml.test"
    assert settings.media_api_url == "http://media.test"
    assert settings.media_key_prefix == "custom-prefix"
    assert settings.vision_model == "vision-yaml"
    assert settings.text_model == "text-env"
    assert settings.core_task_queue == "agentk-local-e2e"


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
