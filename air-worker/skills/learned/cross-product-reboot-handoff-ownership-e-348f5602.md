# Cross-Product Reboot Handoff and Ownership Procedure

## When to apply

Use before a pending owner-operated reboot or shared-host maintenance handoff when multiple products, services, WSL, Docker, or an active Git checkout may be affected.

## Procedure

1. Read `PLAN.md`; record exact target nodes, live Git HEAD/dirty state, and core/SDK service health.
2. Push and independently verify the remote SHA for the required main history.
3. Preserve unrelated dirty or untracked work in a separate immutable Git commit-tree temporary index, and push it to a checkpoint branch without modifying the active checkout.
4. Validate TEI readiness independently; do not infer it from RAGFlow `healthz` or Docker cgroup ceilings. Monitor Windows host RAM and stop only the proven-owned TEI service under pressure.
5. Before shared WSL reboot, preserve the `.wslconfig` SHA and rollback details and obtain exact approval for that reboot.
6. If a foreign MySQL Compose startup lacks a password variable, use verified same-container Docker start followed by health readback; never invent a secret.
7. Append learning only through AirWorker core. Treat a recorded lesson as distinct from reviewer approval, auto-application, and seven-day effectiveness validation.

## Pitfalls

- Modifying the active checkout while creating a handoff checkpoint.
- Treating one product’s health signal or a container limit as proof of another product’s readiness.
- Rebooting shared WSL without exact approval and rollback information.
- Creating or guessing credentials for foreign services.
- Confusing evidence capture with approval or effectiveness validation.
