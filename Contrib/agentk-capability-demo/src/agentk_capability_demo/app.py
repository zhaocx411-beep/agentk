from __future__ import annotations

import os
from contextlib import asynccontextmanager
from pathlib import Path

from agentk.request_lane.ks_ingress import ASGIMiddleware
from fastapi import FastAPI
from fastapi.responses import FileResponse
from fastapi.staticfiles import StaticFiles

from .agent100.narrato.router import configure_narrato
from .agent100.router import router as agent100_router
from .agentk_client import create_client
from .api import router
from .blob_upload import BlobUploadStorage
from .config import Settings
from .store import MemoryStore

PACKAGE_DIR = Path(__file__).resolve().parent
STATIC_DIR = PACKAGE_DIR / "static"


def settings_from_env() -> Settings:
    return Settings(
        agentk_address=os.getenv("AGENTK_ADDRESS") or None,
        api_key=os.getenv("AGENTK_API_KEY") or None,
        host=os.getenv("AGENTK_CAPABILITY_DEMO_HOST", "127.0.0.1"),
        port=int(os.getenv("AGENTK_CAPABILITY_DEMO_PORT", "8080")),
        public_base_url=os.getenv("AGENTK_CAPABILITY_DEMO_PUBLIC_BASE_URL") or None,
        reload=os.getenv("AGENTK_CAPABILITY_DEMO_RELOAD", "").lower() in {"1", "true", "yes"},
        blob_endpoint=os.getenv("AGENTK_DEMO_BLOB_ENDPOINT") or None,
        blob_bucket=os.getenv("AGENTK_DEMO_BLOB_BUCKET") or None,
        blob_prefix="agentk-capability-demo",
        blob_cdn_base=None,
    )


def create_app(settings: Settings | None = None, client=None, upload_storage=None) -> FastAPI:
    settings = settings or settings_from_env()
    upload_dir = Path.cwd() / "uploads"
    upload_dir.mkdir(parents=True, exist_ok=True)
    agentk = client or create_client(settings.agentk_address, settings.api_key)
    uploads = upload_storage
    if uploads is None and settings.blob_endpoint and settings.blob_bucket:
        uploads = BlobUploadStorage(
            settings.blob_endpoint,
            settings.blob_bucket,
            settings.blob_prefix,
            settings.blob_cdn_base,
        )

    @asynccontextmanager
    async def lifespan(_app: FastAPI):
        yield
        close = getattr(agentk, "aclose", None)
        if close:
            await close()
        close_uploads = getattr(uploads, "aclose", None)
        if close_uploads:
            await close_uploads()

    app = FastAPI(title="AgentK Capability Demo", version="0.1.0", lifespan=lifespan)
    app.add_middleware(ASGIMiddleware)
    app.state.settings = settings
    app.state.agentk = agentk
    app.state.store = MemoryStore()
    app.state.upload_dir = upload_dir
    app.state.upload_storage = uploads
    app.include_router(router)
    configure_narrato(app, settings)
    app.include_router(agent100_router)
    app.mount("/assets", StaticFiles(directory=STATIC_DIR), name="assets")
    app.mount("/uploads", StaticFiles(directory=upload_dir), name="uploads")

    @app.middleware("http")
    async def prevent_api_caching(request, call_next):
        response = await call_next(request)
        if (
            request.url.path == "/"
            or request.url.path.startswith("/api/")
            or request.url.path.startswith("/assets/")
        ):
            response.headers["Cache-Control"] = "no-store"
        return response

    @app.get("/healthz")
    async def healthz() -> dict[str, str]:
        return {"status": "ok"}

    @app.get("/", include_in_schema=False)
    async def index() -> FileResponse:
        return FileResponse(STATIC_DIR / "index.html")

    return app
