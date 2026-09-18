"""The single model-visible AirWorker schema."""

AIR_WORKER = {
    "name": "air_worker",
    "description": "Operate a managed AirWorker product through binary-owned status, loop, and judge operations. Detailed output is stored at the returned log path.",
    "parameters": {
        "type": "object",
        "properties": {
            "action": {"type": "string", "enum": ["status", "run", "verify"]},
            "product": {"type": "string", "description": "AirWorker product root path."},
            "config": {"type": "string", "description": "Optional config file path resolved within the product root."},
        },
        "required": ["action", "product"],
        "additionalProperties": False,
    },
}
