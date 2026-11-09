from __future__ import annotations

from fastapi import APIRouter

from .narrato.router import router as narrato_router
from .registry import public_agents

router = APIRouter(prefix="/api/agent100")


@router.get("")
async def catalog() -> dict[str, object]:
    agents = public_agents()
    return {"agents": agents, "count": len(agents)}


router.include_router(narrato_router, prefix="/narrato")
