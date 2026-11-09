import pytest

from agentk_capability_demo.config import Settings, parse_settings


def test_cli_settings_and_public_view(monkeypatch):
    monkeypatch.setenv("AGENTK_ADDRESS", "http://ignored:9")
    settings = parse_settings(
        ["--agentk-address", "http://agentk.test/", "--api-key", "secret", "--port", "9090"]
    )
    assert settings.agentk_address == "http://agentk.test"
    assert settings.port == 9090
    assert "secret" not in repr(settings.public())


def test_missing_address_selects_kess_discovery(monkeypatch):
    monkeypatch.delenv("AGENTK_ADDRESS", raising=False)

    settings = parse_settings(["--api-key", "local-dev"])

    assert settings.agentk_address is None
    assert settings.public()["agentkAddress"] == "kess://klingai-agentk-api"


def test_blob_settings_use_shared_demo_environment(monkeypatch):
    monkeypatch.setenv("AGENTK_DEMO_BLOB_ENDPOINT", "https://blob.test")
    monkeypatch.setenv("AGENTK_DEMO_BLOB_BUCKET", "shared-bucket")

    settings = parse_settings([])

    assert settings.blob_endpoint == "https://blob.test"
    assert settings.blob_bucket == "shared-bucket"


@pytest.mark.parametrize("address", ["localhost:8080", "ftp://example.test"])
def test_address_must_be_absolute_http_url(address):
    with pytest.raises(ValueError, match="absolute http"):
        Settings(address, "key")


def test_blank_address_selects_kess_discovery():
    assert Settings("  ", "key").agentk_address is None


def test_api_key_is_optional_when_agentk_auth_is_off():
    settings = Settings("http://agentk.test", " ")

    assert settings.api_key is None
