from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

DEFAULT_VISION_MODEL_CANDIDATES = (
    "gemini-3.1-flash-lite",
    "gemini-3-flash-preview",
    "gemini-3.1-pro-preview",
)
DEFAULT_TEXT_MODEL_CANDIDATES = (
    "deepseek-v4-pro",
    "deepseek-v4-flash",
    "gpt-5.4",
    "claude-sonnet-4-6",
    "claude-haiku-4-5",
    "glm-5.1",
    "kimi-k2.6",
    "qwen3.7-max",
    "qwen3.5-plus",
)
QWEN_TTS_INSTRUCT_MODEL = "qwen3-tts-instruct-flash"
DEFAULT_QWEN_TTS_MODEL_CANDIDATES = (
    QWEN_TTS_INSTRUCT_MODEL,
    "qwen3-tts-flash",
)
DEFAULT_MINIMAX_TTS_MODEL_CANDIDATES = (
    "minimax-speech-2.8-hd",
    "minimax-speech-2.8-turbo",
    "minimax-speech-2.6-hd",
    "minimax-speech-2.6-turbo",
    "minimax-speech-02-hd",
    "minimax-speech-02-turbo",
    "minimax-speech-01-hd",
    "minimax-speech-01-turbo",
)


@dataclass(frozen=True, slots=True)
class Settings:
    agentk_base_url: str | None = None
    agentk_api_key: str | None = None
    console_token: str = ""
    api_host: str = "127.0.0.1"
    media_api_url: str = "http://127.0.0.1:8000"
    upload_backend: str = "http"
    upload_database: str = "upload"
    upload_bucket: str = ""
    bs3_endpoint: str = ""
    cdn_base: str = ""
    media_key_prefix: str = "narrato-agentk"
    vision_model: str = "gemini-3.1-flash-lite"
    text_model: str = "deepseek-v4-pro"
    asr_model: str = "azure-whisper"
    search_model: str = "volc-search"
    story_model: str = "deepseek-v4-pro"
    script_model: str = "deepseek-v4-pro"
    # tts_model remains the Qwen deployment setting for backward-compatible
    # configuration files; TTS is no longer part of the content-model map.
    tts_model: str = "qwen3-tts-flash"
    minimax_tts_model: str = "minimax-speech-2.8-hd"
    vision_model_candidates: tuple[str, ...] = DEFAULT_VISION_MODEL_CANDIDATES
    text_model_candidates: tuple[str, ...] = DEFAULT_TEXT_MODEL_CANDIDATES
    asr_model_candidates: tuple[str, ...] = ("azure-whisper",)
    search_model_candidates: tuple[str, ...] = ("volc-search", "brightdata-serp")
    tts_model_candidates: tuple[str, ...] = DEFAULT_QWEN_TTS_MODEL_CANDIDATES
    minimax_tts_model_candidates: tuple[str, ...] = DEFAULT_MINIMAX_TTS_MODEL_CANDIDATES
    qwen_voice_id: str = ""
    analysis_window_seconds: float = 30.0
    analysis_concurrency: int = 3
    asr_concurrency: int = 1
    asr_min_interval_seconds: float = 11.0
    asr_global_concurrency: int = 1
    asr_lease_timeout_seconds: float = 900.0
    asr_rate_limiter_workflow_id: str = "narrato-asr-global-rate-limiter-v1"
    tts_concurrency: int = 4
    worker_task_queue: str = "demo-worker"
    core_task_queue: str = "agentk"
    temporal_address: str = ""
    temporal_namespace: str = "default"

    @classmethod
    def load(cls) -> Settings:
        values: dict[str, Any] = {}
        config_path = os.getenv("NARRATO_CONFIG", "").strip()
        if config_path:
            values.update(_load_yaml(Path(config_path)))
        story_configured = bool(values.get("story_model"))
        script_configured = bool(values.get("script_model"))
        env = {
            "agentk_base_url": os.getenv("AGENTK_ADDRESS") or os.getenv("AGENTK_BASE_URL"),
            "agentk_api_key": os.getenv("AGENTK_API_KEY"),
            "console_token": os.getenv("NARRATO_CONSOLE_TOKEN"),
            "api_host": os.getenv("NARRATO_API_HOST"),
            "media_api_url": os.getenv("NARRATO_MEDIA_API_URL"),
            "media_key_prefix": os.getenv("NARRATO_MEDIA_KEY_PREFIX"),
            "vision_model": os.getenv("NARRATO_VISION_MODEL"),
            "text_model": os.getenv("NARRATO_TEXT_MODEL"),
            "asr_model": os.getenv("NARRATO_ASR_MODEL"),
            "search_model": os.getenv("NARRATO_SEARCH_MODEL"),
            "story_model": os.getenv("NARRATO_STORY_MODEL"),
            "script_model": os.getenv("NARRATO_SCRIPT_MODEL"),
            "tts_model": os.getenv("NARRATO_TTS_MODEL"),
            "minimax_tts_model": os.getenv("NARRATO_MINIMAX_TTS_MODEL"),
            "vision_model_candidates": _csv_env("NARRATO_VISION_MODEL_CANDIDATES"),
            "text_model_candidates": _csv_env("NARRATO_TEXT_MODEL_CANDIDATES"),
            "asr_model_candidates": _csv_env("NARRATO_ASR_MODEL_CANDIDATES"),
            "search_model_candidates": _csv_env("NARRATO_SEARCH_MODEL_CANDIDATES"),
            "tts_model_candidates": _csv_env("NARRATO_TTS_MODEL_CANDIDATES"),
            "minimax_tts_model_candidates": _csv_env(
                "NARRATO_MINIMAX_TTS_MODEL_CANDIDATES"
            ),
            "qwen_voice_id": os.getenv("NARRATO_QWEN_VOICE_ID"),
            "asr_concurrency": _int_env("NARRATO_ASR_CONCURRENCY"),
            "asr_min_interval_seconds": _float_env("NARRATO_ASR_MIN_INTERVAL_SECONDS"),
            "asr_global_concurrency": _int_env("NARRATO_ASR_GLOBAL_CONCURRENCY"),
            "asr_lease_timeout_seconds": _float_env("NARRATO_ASR_LEASE_TIMEOUT_SECONDS"),
            "asr_rate_limiter_workflow_id": os.getenv(
                "NARRATO_ASR_RATE_LIMITER_WORKFLOW_ID"
            ),
            "worker_task_queue": os.getenv("NARRATO_WORKER_TASK_QUEUE"),
            "core_task_queue": os.getenv("NARRATO_CORE_TASK_QUEUE"),
            "temporal_address": os.getenv("NARRATO_TEMPORAL_ADDRESS"),
            "temporal_namespace": os.getenv("NARRATO_TEMPORAL_NAMESPACE"),
        }
        blob_endpoint = (os.getenv("AGENTK_DEMO_BLOB_ENDPOINT") or "").strip().rstrip("/")
        blob_bucket = (os.getenv("AGENTK_DEMO_BLOB_BUCKET") or "").strip().strip("/")
        if blob_endpoint:
            env["bs3_endpoint"] = blob_endpoint
        if blob_bucket:
            env["upload_bucket"] = blob_bucket
        if blob_endpoint and blob_bucket:
            env["cdn_base"] = f"{blob_endpoint}/{blob_bucket}"
        values.update({key: value for key, value in env.items() if value not in (None, "")})
        if env["text_model"]:
            if not env["story_model"] and not story_configured:
                values["story_model"] = env["text_model"]
            if not env["script_model"] and not script_configured:
                values["script_model"] = env["text_model"]
        return cls(**values)

    def validate_worker(self) -> None:
        required = {
            "NARRATO_VISION_MODEL": self.vision_model,
            "NARRATO_TEXT_MODEL": self.text_model,
            "NARRATO_ASR_MODEL": self.asr_model,
            "NARRATO_SEARCH_MODEL": self.search_model,
            "NARRATO_STORY_MODEL": self.story_model,
            "NARRATO_SCRIPT_MODEL": self.script_model,
            "NARRATO_TTS_MODEL": self.tts_model,
            "NARRATO_MINIMAX_TTS_MODEL": self.minimax_tts_model,
        }
        missing = [name for name, value in required.items() if not str(value).strip()]
        if missing:
            raise ValueError(f"missing worker configuration: {', '.join(missing)}")
        if not 0 < self.analysis_window_seconds <= 300:
            raise ValueError("analysis_window_seconds must be between 0 and 300")
        if not 1 <= self.analysis_concurrency <= 16:
            raise ValueError("analysis_concurrency must be between 1 and 16")
        if not 1 <= self.asr_concurrency <= 16:
            raise ValueError("asr_concurrency must be between 1 and 16")
        if not 0 < self.asr_min_interval_seconds <= 300:
            raise ValueError("asr_min_interval_seconds must be between 0 and 300")
        if not 1 <= self.asr_global_concurrency <= 16:
            raise ValueError("asr_global_concurrency must be between 1 and 16")
        if not 60 <= self.asr_lease_timeout_seconds <= 86400:
            raise ValueError("asr_lease_timeout_seconds must be between 60 and 86400")
        if not self.asr_rate_limiter_workflow_id.strip():
            raise ValueError("asr_rate_limiter_workflow_id must not be empty")
        if not 1 <= self.tts_concurrency <= 16:
            raise ValueError("tts_concurrency must be between 1 and 16")
        if not self.worker_task_queue.strip() or not self.core_task_queue.strip():
            raise ValueError("worker_task_queue and core_task_queue must not be empty")
        if self.temporal_address.strip() and not self.temporal_namespace.strip():
            raise ValueError("temporal_namespace must not be empty for local Temporal")
        self.resolve_models({})

    def model_defaults(self) -> dict[str, str]:
        return {
            "vision": self.vision_model,
            "asr": self.asr_model,
            "search": self.search_model,
            "story": self.story_model,
            "script": self.script_model,
        }

    def model_options(self) -> dict[str, tuple[str, ...]]:
        def with_default(default: str, candidates: tuple[str, ...]) -> tuple[str, ...]:
            # A deployment-specific default is always a valid choice even when
            # callers construct Settings directly without also replacing the
            # UI allow-list (common in tests and private deployments).
            return candidates if default in candidates else (default, *candidates)

        return {
            "vision": with_default(self.vision_model, self.vision_model_candidates),
            "asr": with_default(self.asr_model, self.asr_model_candidates),
            "search": with_default(self.search_model, self.search_model_candidates),
            "story": with_default(self.story_model, self.text_model_candidates),
            "script": with_default(self.script_model, self.text_model_candidates),
        }

    def resolve_models(self, overrides: dict[str, str]) -> dict[str, str]:
        defaults = self.model_defaults()
        unknown = sorted(set(overrides) - set(defaults))
        if unknown:
            raise ValueError(f"models.{unknown[0]} is not supported")
        selected = {**defaults, **overrides}
        for slot, model in selected.items():
            if not str(model).strip():
                raise ValueError(f"models.{slot} is required")
            candidates = self.model_options()[slot]
            if model not in candidates:
                raise ValueError(
                    f"models.{slot}={model!r} is not allowed by this Narrato deployment"
                )
        return selected

    def tts_defaults(self) -> dict[str, str]:
        return {"qwen": self.tts_model, "minimax": self.minimax_tts_model}

    def tts_model_options(self) -> dict[str, tuple[str, ...]]:
        def with_default(default: str, candidates: tuple[str, ...]) -> tuple[str, ...]:
            return candidates if default in candidates else (default, *candidates)

        return {
            "qwen": with_default(self.tts_model, self.tts_model_candidates),
            "minimax": with_default(
                self.minimax_tts_model,
                self.minimax_tts_model_candidates,
            ),
        }

    def resolve_tts(self, value: dict[str, str]) -> dict[str, str]:
        provider = str(value.get("provider") or "").strip().lower()
        if provider not in self.tts_defaults():
            raise ValueError(f"tts.provider={provider!r} is not supported")
        model = str(value.get("model") or "").strip()
        if model not in self.tts_model_options()[provider]:
            raise ValueError(
                f"tts.model={model!r} is not allowed for provider {provider!r}"
            )
        voice_id = str(value.get("voiceId") or "").strip()
        if not voice_id:
            raise ValueError("tts.voiceId is required")
        return {"provider": provider, "model": model, "voiceId": voice_id}

    def validate_media(self) -> None:
        backend = self.upload_backend.strip().lower()
        if backend != "http":
            raise ValueError("Narrato media upload uses AGENTK_DEMO_BLOB_ENDPOINT over HTTP")
        required = {
            "AGENTK_DEMO_BLOB_BUCKET": self.upload_bucket,
            "AGENTK_DEMO_BLOB_ENDPOINT": self.bs3_endpoint,
        }
        missing = [name for name, value in required.items() if not str(value).strip()]
        if missing:
            raise ValueError(f"missing media configuration: {', '.join(missing)}")
        parsed = urlparse(self.bs3_endpoint)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise ValueError("AGENTK_DEMO_BLOB_ENDPOINT must be an absolute HTTP(S) URL")

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
    temporal = flat.pop("temporal", {}) or {}
    model_candidates = flat.pop("model_candidates", {}) or {}
    if "key_prefix" in media:
        media["media_key_prefix"] = media.pop("key_prefix")
    if "api_url" in media:
        flat["media_api_url"] = media.pop("api_url")
    if "backend" in media:
        media["upload_backend"] = media.pop("backend")
    model_values = {f"{key}_model": value for key, value in models.items()}
    if "text" in models:
        model_values.setdefault("story_model", models["text"])
        model_values.setdefault("script_model", models["text"])
    return {
        **flat,
        **model_values,
        **{f"{key}_model_candidates": tuple(value) for key, value in model_candidates.items()},
        **media,
        **{f"agentk_{key}": value for key, value in agentk.items()},
        **{f"temporal_{key}": value for key, value in temporal.items()},
    }


def _csv_env(name: str) -> tuple[str, ...] | None:
    value = os.getenv(name)
    if value is None:
        return None
    return tuple(item.strip() for item in value.split(",") if item.strip())


def _int_env(name: str) -> int | None:
    value = os.getenv(name)
    return None if value in (None, "") else int(value)


def _float_env(name: str) -> float | None:
    value = os.getenv(name)
    return None if value in (None, "") else float(value)
