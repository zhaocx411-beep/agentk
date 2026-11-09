from agentk.client import Options

from agentk_capability_demo import agentk_client


def test_create_client_uses_sdk_kess_discovery_without_address(monkeypatch):
    expected = object()
    received: list[Options] = []

    def fake_create(options: Options):
        received.append(options)
        return expected

    monkeypatch.setattr(agentk_client, "_create_kess_client", fake_create)

    result = agentk_client.create_client(None, "local-dev")

    assert result is expected
    assert received == [Options()]


def test_create_client_does_not_require_legacy_api_key_when_auth_is_off(monkeypatch):
    received: list[Options] = []

    def fake_create(options: Options):
        received.append(options)
        return object()

    monkeypatch.setattr(agentk_client, "_create_kess_client", fake_create)

    agentk_client.create_client(None)

    assert received == [Options()]


def test_custom_http_client_requires_explicit_address():
    try:
        agentk_client.create_client(None, "local-dev", http_client=object())
    except ValueError as error:
        assert "explicit AgentK address" in str(error)
    else:
        raise AssertionError("expected missing address to be rejected")
