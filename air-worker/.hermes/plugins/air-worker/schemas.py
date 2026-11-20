"""The single model-visible AirWorker schema."""

AIR_WORKER = {
    "name": "air_worker",
    "description": "Operate a managed AirWorker product through binary-owned status, loop, and judge operations. In the enforce profile, status atomically resumes orphaned pending work and judges completed work before returning; it never returns an unverified completion or orphaned pending state as success. Detailed output is stored at the returned log path.",
    "parameters": {
        "type": "object",
        "properties": {
            "action": {"type": "string", "enum": ["status", "run", "verify", "plan_new"], "description": "status is read-only in shadow; enforce may resume work. plan_new creates a native plan node; use a stable request_id and reconcile UNKNOWN before retry."},
            "title": {"type": "string", "description": "plan_new: node title."},
            "parent": {"type": "string", "description": "plan_new: existing parent or stage."},
            "owner": {"type": "string", "description": "plan_new: node owner."},
            "done_when": {"type": "string", "description": "plan_new: completion criterion."},
            "request_id": {"type": "string", "description": "plan_new: stable non-secret request identifier."},
            "product": {"type": "string", "description": "AirWorker product root path."},
            "config": {"type": "string", "description": "Optional config file path resolved within the product root."},
        },
        "required": ["action", "product"],
        "additionalProperties": False,
    },
}
