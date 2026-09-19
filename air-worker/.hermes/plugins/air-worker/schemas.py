"""The single model-visible AirWorker schema."""

AIR_WORKER = {
    "name": "air_worker",
    "description": "Operate a managed AirWorker product through binary-owned status, loop, and judge operations. In the enforce profile, status atomically resumes orphaned pending work and judges completed work before returning; it never returns an unverified completion or orphaned pending state as success. Detailed output is stored at the returned log path.",
    "parameters": {
        "type": "object",
        "properties": {
            "action": {"type": "string", "enum": ["status", "run", "verify"], "description": "status is read-only in shadow; in enforce it resumes pending work when no live worker exists and judges completion before returning."},
            "product": {"type": "string", "description": "AirWorker product root path."},
            "config": {"type": "string", "description": "Optional config file path resolved within the product root."},
        },
        "required": ["action", "product"],
        "additionalProperties": False,
    },
}
