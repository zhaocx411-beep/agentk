from __future__ import annotations

import argparse
import os
from dataclasses import dataclass
from urllib.parse import urlparse


def _url(value: str, name: str) -> str:
    value = value.strip().rstrip("/")
    parsed = urlparse(value)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        raise ValueError(f"{name} must be an absolute http(s) URL")
    return value


@dataclass(frozen=True, slots=True)
class Settings:
    agentk_address: str | None
    api_key: str | None = None
    host: str = "127.0.0.1"
    port: int = 8080
    public_base_url: str | None = None
    reload: bool = False
    blob_endpoint: str | None = None
    blob_bucket: str | None = None
    blob_prefix: str = "agentk-capability-demo"
    blob_cdn_base: str | None = None

    def __post_init__(self) -> None:
        address = self.agentk_address.strip() if self.agentk_address else ""
        if address:
            object.__setattr__(self, "agentk_address", _url(address, "agentk address"))
        else:
            object.__setattr__(self, "agentk_address", None)
        key = self.api_key.strip() if self.api_key else None
        object.__setattr__(self, "api_key", key or None)
        if not 1 <= self.port <= 65535:
            raise ValueError("port must be between 1 and 65535")
        if self.public_base_url:
            object.__setattr__(
                self, "public_base_url", _url(self.public_base_url, "public base URL")
            )
        endpoint = self.blob_endpoint.strip().rstrip("/") if self.blob_endpoint else None
        bucket = self.blob_bucket.strip().strip("/") if self.blob_bucket else None
        if bool(endpoint) != bool(bucket):
            raise ValueError("blob endpoint and bucket must be configured together")
        object.__setattr__(
            self, "blob_endpoint", _url(endpoint, "blob endpoint") if endpoint else None
        )
        object.__setattr__(self, "blob_bucket", bucket)
        prefix = self.blob_prefix.strip().strip("/")
        if not prefix or prefix == "." or ".." in prefix.split("/"):
            raise ValueError("blob prefix must be a non-empty relative path")
        object.__setattr__(self, "blob_prefix", prefix)
        if self.blob_cdn_base:
            object.__setattr__(self, "blob_cdn_base", _url(self.blob_cdn_base, "blob CDN base URL"))

    def public(self) -> dict[str, object]:
        return {
            "agentkAddress": self.agentk_address or "kess://klingai-agentk-api",
            "host": self.host,
            "port": self.port,
            "uploadsPublic": bool(self.public_base_url),
            "uploadsManaged": bool(self.blob_endpoint),
        }


def parse_settings(argv: list[str] | None = None) -> Settings:
    parser = argparse.ArgumentParser(description="AgentK Capability Demo")
    parser.add_argument(
        "--agentk-address",
        default=os.getenv("AGENTK_ADDRESS") or None,
        help="Optional AgentK URL; omit to use SDK KESS discovery (env: AGENTK_ADDRESS)",
    )
    parser.add_argument(
        "--api-key",
        default=os.getenv("AGENTK_API_KEY") or None,
        help="Deprecated compatibility option; SDK 0.3.0 uses KWS service identity",
    )
    parser.add_argument(
        "--host", default=os.getenv("AGENTK_CAPABILITY_DEMO_HOST", "127.0.0.1")
    )
    parser.add_argument(
        "--port",
        type=int,
        default=int(os.getenv("AGENTK_CAPABILITY_DEMO_PORT", "8080")),
    )
    parser.add_argument(
        "--public-base-url",
        default=os.getenv("AGENTK_CAPABILITY_DEMO_PUBLIC_BASE_URL") or None,
        help="Externally reachable base URL used for uploaded files",
    )
    parser.add_argument("--reload", action="store_true")
    parser.add_argument(
        "--blob-endpoint",
        default=os.getenv("AGENTK_DEMO_BLOB_ENDPOINT") or None,
        help="BlobStore HTTP endpoint (env: AGENTK_DEMO_BLOB_ENDPOINT)",
    )
    parser.add_argument(
        "--blob-bucket",
        default=os.getenv("AGENTK_DEMO_BLOB_BUCKET") or None,
        help="BlobStore bucket shared with AgentK (env: AGENTK_DEMO_BLOB_BUCKET)",
    )
    args = parser.parse_args(argv)
    return Settings(
        agentk_address=args.agentk_address,
        api_key=args.api_key,
        host=args.host,
        port=args.port,
        public_base_url=args.public_base_url,
        reload=args.reload,
        blob_endpoint=args.blob_endpoint,
        blob_bucket=args.blob_bucket,
    )
