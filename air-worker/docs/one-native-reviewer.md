# Native code review

Use a newly built source binary; this change does not update installed AirWorker 0.11.8.
The product must be in a clean Git repository. The current branch must have either a
matching configured GitHub upstream or no tracking configuration and exactly one
configured GitHub remote; the published branch SHA must equal the requested source SHA.
Missing, partial, conflicting, or ambiguous remote authority is rejected. Declare and
enable the session for that product using the existing session workflow first.

One canonical launch (append `-preflight` to validate without starting Codex):

```text
air-worker review code -product <product-root> -source-sha <40-lowercase-hex> -task-file <inside-product-path> -principal <principal> -session-key <session> -json
```

`-state-dir <directory>` selects existing session state. Relative task paths resolve
against the product. Tasks must be regular files of at most 16 KiB, with no symlink or
Windows reparse/junction ancestors. The configured `gpt6-sol:low` tier must resolve to
Codex / `gpt-6-sol` / `low`. The command resolves Codex itself and fixes the sandbox to
`read-only`; no model, tier, executable, effort, proxy, sandbox, or extra CLI options
are accepted. Do not manually launch Codex to perform this review.

Preflight emits `PREFLIGHT_OK` only after all checks pass; it is not a review verdict.
Exit codes: 0 = validated preflight or review PASS; 1 = CHANGES_REQUIRED;
2 = invalid input, failed invariant, transport failure, or NOT_PROVEN.
PASS requires zero P0, P1 and P2 findings, a successful job, a terminal successful
Codex event, and unchanged local/remote source, task identity/digest and PLAN digest.

The output identifies `job_receipt`. Under `.woody/jobs/`, that receipt records
RUNNING/DONE/FAILED and the output path; its `.review.json` companion records the
validated verdict, exact reviewer result, source/task/PLAN digests, fixed runner,
output digest and Codex session ID. PLAN SHA is SHA-256 of the exact PLAN.md bytes.
The step identifier derives from the full source SHA and task digest.

An existing receipt or permanent `.claim` refuses replay in the declared session,
even after completion or interruption. Inspect those artifacts and the receipt's
`output_path`; never blindly restart or delete the claim. RUNNING or a missing review
receipt is not PASS. Claims also remain when preparation fails after reservation.
The command never closes plan nodes or writes PLAN/LEARN. Acceptance of N-124/A2
still requires the independent reviewer and the native/human gates.

Scoped host verification, from the product's `cmd` directory:

```powershell
go test . -run '^TestCodeReview|^TestNativeReviewer' -count=1
git diff --check
```

Tests use temporary Git repositories and mock the live remote and Codex runner;
there are no real model calls or external GitHub writes.
