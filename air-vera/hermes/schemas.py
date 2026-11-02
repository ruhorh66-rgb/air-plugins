"""One bounded Hermes v0.22 Agent Plugin v1 schema for native AirVera."""

AIR_VERA = {
    "name": "air_vera",
    "description": (
        "Use the installed and independently accepted AirVera native control "
        "plane for read-only status, mechanism list, package-health, or "
        "Task-010 historical evidence. Task evidence does not mean sent, "
        "delivered, read or task DONE. No authority, filesystem paths, "
        "mail provider writes or arbitrary CLI commands may be supplied."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "action": {
                "type": "string",
                "enum": ["status", "list", "package-health", "task-evidence"],
            },
            "task_id": {
                "type": "string",
                "pattern": "^TASK-OBS-[0-9]{4}$",
                "description": "Required only for task-evidence, exact Task-010 ID.",
            },
        },
        "required": ["action"],
        "additionalProperties": False,
    },
}
