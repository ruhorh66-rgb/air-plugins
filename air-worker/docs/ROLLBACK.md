# air-worker 0.3.0 rollback plan

The source change is reversible by returning to the last installed plugin version; do
not edit the shared runtime database, queue database, or marketplace/cache by hand.

1. Confirm the prior Claude/Codex plugin versions and source commit.
2. Reinstall the prior plugin version through the configured marketplace.
3. In a fresh host task, run the previous offline selftest and one legacy/direct scenario
   appropriate to that version.
4. Verify worker request rows and queue rows were not modified by rollback. A task already
   `running`/`done` remains an auditable receipt; never replay it as rollback cleanup.

The current feature branch adds no schema migration. The atomic claim prevents a retry
from becoming a second execution. The required test is `test_execution_claim_is_idempotent_after_done`.
