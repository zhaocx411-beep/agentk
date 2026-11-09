from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any


@dataclass(frozen=True, slots=True)
class Settings:
    agentk_base_url: str = "https://agentk.internal"
    agentk_api_key: str = ""
    console_token: str = ""
    api_host: str = "127.0.0.1"
    media_api_url: str = "http://127.0.0.1:8000"
    upload_bucket: str = ""
    bs3_endpoint: str = ""
    cdn_base: str = ""
    media_key_prefix: str = "narrato-agentk"
    vision_model: str = "gemini-3.1-flash-lite"
    text_model: str = "deepseek-v4-pro"
    tts_model: str = "qwen3-tts-flash"
    qwen_voice_id: str = ""
    analysis_window_seconds: float = 30.0
    analysis_concurrency: int = 3
    tts_concurrency: int = 4
    worker_task_queue: str = "contrib-narrato-documentary-v1"
    core_task_queue: str = "agentk"

    @classmethod
    def load(cls) -> Settings:
        values: dict[str, Any] = {}
        config_path = os.getenv("NARRATO_CONFIG", "").strip()
        if config_path:
            values.update(_load_yaml(Path(config_path)))
        env = {
            "agentk_base_url": os.getenv("AGENTK_BASE_URL"),
            "agentk_api_key": os.getenv("AGENTK_API_KEY"),
            "console_token": os.getenv("NARRATO_CONSOLE_TOKEN"),
            "api_host": os.getenv("NARRATO_API_HOST"),
            "media_api_url": os.getenv("NARRATO_MEDIA_API_URL"),
            "upload_bucket": os.getenv("NARRATO_UPLOAD_BUCKET"),
            "bs3_endpoint": os.getenv("NARRATO_BS3_ENDPOINT"),
            "cdn_base": os.getenv("NARRATO_CDN_BASE"),
            "media_key_prefix": os.getenv("NARRATO_MEDIA_KEY_PREFIX"),
            "vision_model": os.getenv("NARRATO_VISION_MODEL"),
            "text_model": os.getenv("NARRATO_TEXT_MODEL"),
            "tts_model": os.getenv("NARRATO_TTS_MODEL"),
            "qwen_voice_id": os.getenv("NARRATO_QWEN_VOICE_ID"),
            "core_task_queue": os.getenv("NARRATO_CORE_TASK_QUEUE"),
        }
        values.update({key: value for key, value in env.items() if value not in (None, "")})
        return cls(**values)

    def validate_worker(self) -> None:
        required = {
            "AGENTK_API_KEY": self.agentk_api_key,
            "NARRATO_VISION_MODEL": self.vision_model,
            "NARRATO_TEXT_MODEL": self.text_model,
            "NARRATO_TTS_MODEL": self.tts_model,
        }
        missing = [name for name, value in required.items() if not str(value).strip()]
        if missing:
            raise ValueError(f"missing worker configuration: {', '.join(missing)}")

    def validate_media(self) -> None:
        required = {
            "NARRATO_UPLOAD_BUCKET": self.upload_bucket,
            "NARRATO_BS3_ENDPOINT": self.bs3_endpoint,
            "NARRATO_CDN_BASE": self.cdn_base,
        }
        missing = [name for name, value in required.items() if not str(value).strip()]
        if missing:
            raise ValueError(f"missing media configuration: {', '.join(missing)}")

    def validate_api(self) -> None:
        loopback_hosts = {"127.0.0.1", "::1", "localhost"}
        if self.api_host.strip().lower() not in loopback_hosts and not self.console_token:
            raise ValueError(
                "NARRATO_CONSOLE_TOKEN is required when NARRATO_API_HOST is not loopback"
            )


def _load_yaml(path: Path) -> dict[str, Any]:
    import yaml

    with path.open("r", encoding="utf-8") as handle:
        raw = yaml.safe_load(handle) or {}
    if not isinstance(raw, dict):
        raise TypeError("NARRATO_CONFIG must contain a YAML object")
    flat = dict(raw)
    models = flat.pop("models", {}) or {}
    media = flat.pop("media", {}) or {}
    agentk = flat.pop("agentk", {}) or {}
    if "key_prefix" in media:
        media["media_key_prefix"] = media.pop("key_prefix")
    if "api_url" in media:
        flat["media_api_url"] = media.pop("api_url")
    return {
        **flat,
        **{f"{key}_model": value for key, value in models.items()},
        **media,
        **{f"agentk_{key}": value for key, value in agentk.items()},
    }
