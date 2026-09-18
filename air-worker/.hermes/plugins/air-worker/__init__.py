"""Native Hermes registration for AirWorker."""
from pathlib import Path

from . import adapter, hooks, schemas

execute = adapter.execute


def register(ctx) -> None:
    hooks.configure(getattr(ctx, "profile_name", ""))
    ctx.register_tool(
        name="air_worker",
        toolset="air_worker",
        schema=schemas.AIR_WORKER,
        handler=execute,
    )
    for name, callback in (
        ("pre_llm_call", hooks.pre_llm_call),
        ("pre_tool_call", hooks.pre_tool_call),
        ("post_tool_call", hooks.post_tool_call),
        ("pre_verify", hooks.pre_verify),
        ("subagent_start", hooks.subagent_start),
        ("subagent_stop", hooks.subagent_stop),
        ("on_session_start", hooks.bind_session),
        ("on_session_end", hooks.unbind_session),
    ):
        ctx.register_hook(name, callback)
    ctx.register_skill(
        "operate-air-worker",
        Path(__file__).parent / "skills" / "operate-air-worker" / "SKILL.md",
    )
