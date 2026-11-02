"""Hermes registration: exactly one model-visible tool."""
from .adapter import execute
from .schemas import AIR_VERA


def register(ctx) -> None:
    ctx.register_tool(name="air_vera", toolset="terminal", schema=AIR_VERA, handler=execute)
