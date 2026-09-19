"""Native Hermes registration for AirWorker."""
from pathlib import Path

from . import adapter, hooks, schemas

execute = adapter.execute


def register(ctx) -> None:
    hooks.configure(getattr(ctx, "profile_name", ""))
    ctx.register_tool(
        name="air_worker",
        # Hermes 0.21.3 validates CLI toolset names before plugin-defined names
        # are registered. Reuse the built-in execution toolset so bounded
        # stream-json output is not prefixed by an "Unknown toolsets" warning.
        toolset="terminal",
        schema=schemas.AIR_WORKER,
        handler=execute,
    )
    for name, callback in (
        ("pre_llm_call", hooks.pre_llm_call),
        ("pre_tool_call", hooks.pre_tool_call),
        ("post_tool_call", hooks.post_tool_call),
        ("pre_verify", hooks.pre_verify),
        ("transform_llm_output", hooks.transform_llm_output),
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
