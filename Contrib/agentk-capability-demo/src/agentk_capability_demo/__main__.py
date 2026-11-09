from __future__ import annotations

import os

import uvicorn

from .config import parse_settings


def main() -> None:
    settings = parse_settings()
    if settings.agentk_address:
        os.environ["AGENTK_ADDRESS"] = settings.agentk_address
    else:
        os.environ.pop("AGENTK_ADDRESS", None)
    if settings.api_key:
        os.environ["AGENTK_API_KEY"] = settings.api_key
    else:
        os.environ.pop("AGENTK_API_KEY", None)
    os.environ["AGENTK_CAPABILITY_DEMO_HOST"] = settings.host
    os.environ["AGENTK_CAPABILITY_DEMO_PORT"] = str(settings.port)
    if settings.public_base_url:
        os.environ["AGENTK_CAPABILITY_DEMO_PUBLIC_BASE_URL"] = settings.public_base_url
    if settings.blob_endpoint and settings.blob_bucket:
        os.environ["AGENTK_DEMO_BLOB_ENDPOINT"] = settings.blob_endpoint
        os.environ["AGENTK_DEMO_BLOB_BUCKET"] = settings.blob_bucket
    uvicorn.run(
        "agentk_capability_demo.app:create_app",
        host=settings.host,
        port=settings.port,
        reload=settings.reload,
        factory=True,
    )


if __name__ == "__main__":
    main()
