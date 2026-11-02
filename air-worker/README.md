# air-worker

## Native independent source review of a plan/N node (0.11.11-rc.3 candidate)

This is the **one official route**, not a second plan or executor. The installed
0.11.11-rc.2 does **not** support the native `-node` form. First verify the
candidate version/hash and the product's pinned Git HEAD; use this only after
the owner authorizes source-only review. It must not execute blocked ChatLink
tests or use their live service.

```powershell
$node = "N-015_n011-scheduled-identity-trace-and-source-only-bi"
$root = "D:\DevBox\AIRChatLink\worktrees\asw"
$source = "1f1e6a2d402533f0f4d85145b6a766ca36d7840f"
$artifact = "docs/receipts/AIRWORKER_NATIVE_SEMANTIC_N015_GAP_20261102.json"
$sha = "9f545766e83bfa26fc22d0f70b0e26ce52a7d91b6f9369cb2cdd63965d05c65d"
& $candidateAirWorker semantic -product $root -node $node -scope source-only -source-commit $source -artifact $artifact -artifact-sha256 $sha -executor codex -reviewer claude-sonnet-5-5 -effort low
$semanticRC = $LASTEXITCODE
```

Here `$candidateAirWorker` is **only** an explicitly authorized exact SHA-pinned
0.11.11-rc.3 QA/development binary, never the installed rc.2 path. The example
names frozen N015 data solely to make the contract concrete; it has **not** been
executed against the ASW product and does **not** authorize doing so.

`-node` requires the **full** canonical ID in `plan/N-*.md` with exactly one
`[[N-ID]]` link in tracked `PLAN.md`, matching the exact Git HEAD. The single
tracked UTF-8 artifact is bounded to 48 KiB and must match both the literal
SHA256 and the pinned Git blob. A missing/stale/foreign/closed node, mismatched
SHA, unapproved provider/effort or accidental legacy `-step N-ID` fails RC2
**before any model**. The request pins the independent opposite-vendor route:
OpenAI executor identity `-executor codex` → `claude-sonnet-5-5 / low`;
a release override or another model is refused rather than silently selected.

`-scope source-only` **never calls product `runJudge`, shell/QA, executor,
or model fallback**. Claude's existing semantic reviewer permits only
`Read,Glob,Grep`, without Bash/Write/Edit. The actual native result and receipt
are in `.woody/semantic-verdict.json`, `.woody/semantic-history.jsonl` and
`.woody/jobs/*semantic-reviewer*.{receipt.json,out}`. The final JSON line
reports phase and model invocation. RC0 means **source-only semantic PASS with
concrete file/line evidence, not task PASS**; RC1 means source DRIFT; RC2 means
pre-model refusal or source NOT_PROVEN. The receipt declares
`factual_checks_executed=false`. RC0 additionally requires a source
citation with the **exact tracked artifact path, line number and literal content**
matched against the SHA-pinned bytes; an invented `file.go:3` is NOT_PROVEN.
The receipt must be read back with a fresh unique `review_id`, matching the
node, SHA, provider, outcome and the present invocation. A failed reviewer
launch is `MODEL_ATTEMPTED_NOT_PROVEN`, not proof of model invocation.
This confirms content-grounding, **not** separate Claude filesystem tool-use
events; if actual tool-read provenance is required, that remains NOT_PROVEN.

The legacy form `semantic -step <numeric PLAN step>` remains available but
**runs the configured factual judge and may execute QA**; it must never be
substituted for a native `-node` review. The source-only result does not close
native plan nodes, install a binary, authorize production, or replace separate
functional tests when those are permitted.

## 0.11.11-rc.3 — truthful native Codex outcomes (source-only candidate)

This isolated development candidate builds on the **independently accepted** safeoutcome
source `a8f6f66a134c5b6dbebb943b4087f8efd7a09c92`. It does not import or merge
the separately frozen AW-LRN-03 reviewer-catalogue branch `ebd85a6c`; that branch
retains its four Windows file-symlink-negative SKIP and release-security HOLD.

Native `air-worker executor dispatch -json` now emits schema
`air-worker.executor-dispatch/v2`: `model_turn_completed` means the model completed
one turn, **not** that the assigned task was fulfilled. A completed unverified
model turn has `ok=false`, `task_outcome=MODEL_TURN_COMPLETED_TASK_NOT_VERIFIED`,
and CLI **RC2**. A failed execution, access denial or unreadable task has RC1.
An independent factual judge establishes task completion by checking the actual
result after model dispatch; a receipt with `status=DONE` proves only process lifecycle.
The reviewed patch also recognizes structured failed Codex tool events, suppresses
fallback on access denial, and retains Windows `workspace-write` restrictions:
two explicit same-volume writable directories were proved on isolated D: using a
single native model run and 13/13 factual checks; cross-volume, reparse/link,
duplicate, ancestor and workspace-equal writable roots are rejected before launch.
No sandbox downgrade, UAC, privileged token or foreign service control is involved.

This is **not a published or installed version**. The installed `0.11.11-rc.2`
CLI/tray/bridge remain SHA-pinned and the only production rollback baseline.
This candidate must pass bounded external-consumer RC1/RC2 contract checks,
candidate-specific QA, clean source/build provenance and independent release
review before an exact LPR decision on publication and installation.
A test fixture is not proof that external consumer applications have adopted
the new response contract. The autonomous learned-procedure effect and full
AW-LRN-03 integration also remain NOT_PROVEN.


## 0.11.11-rc.2 â€” Codex junction alias correction (release candidate)

This bounded prerelease adds exactly the accepted source correction from commit
05a688cf: the selfcheck reads physically identical Codex config directories
once, recognizing verified Windows junction aliases via os.Stat + os.SameFile.
Distinct profiles and inaccessible/unsafe roots remain visible to the existing
fail-closed skill validation. The installed 0.11.11-rc.1 remains the rollback
baseline until a separate exact LPR release/install approval. Three unrelated
missing embedded vcs.revision diagnostics remain NOT_PROVEN if build provenance
cannot be embedded and verified; they are not silently suppressed.

HLP06 delivery/source safety and the previously accepted N124 CAS are unchanged.
N124 installed issuer/apply callbacks remain nil/fail-closed; no new trust, host
protocol, AirVera CAS or broader platform is included. HLP06 natural use/effect
will be verified after an authorized installation; source acceptance is not that
effect.
## 0.11.11-rc.1 — scoped HLP06 / N124 prerelease candidate

This GitHub-sourced prerelease is **not installed or approved for installation**. HLP06 has signed native delivery, replay protection and its read-only preflight in source. N124 preserves the prior accepted CAS capability, but grant issuance and apply remain fail-closed until a separately trusted installed host attests both the exact LPR decision and the exact actor/generation. Source/test evidence is not production efficacy. Installation, host trust assignment and live AirVera CAS each require separate explicit LPR authorization and verified rollback to installed 0.11.10.

## Explicit GPT procedure delivery (source candidate)

The discovery and explicit-load workflow follows [Hermes progressive disclosure](https://github.com/NousResearch/hermes-agent/blob/0e427d788d253630d38a3a51cd76849e73cd6925/tools/skills_tool.py). Action and outcome proof reuse AirWorker's existing signed native receipts; Hermes view/use counters are not treated as measured production benefit.

In GPT or AIR Commander, inspect `air-worker learn self status -json`, then discover the pinned owner's current procedures with `air-worker learn index -product <owner product_root> -json`. Load one exact entry with `air-worker learn self load -principal <p> -session-key <k> -run-id <r> -target <catalog target> -expected-sha256 <catalog sha256> -json`. Read the returned `context` before a suitable native action. The response includes `context_receipt_sha256` for the authenticated current delivery. For the supported native missing-configuration preflight, pass `-learning-principal <p> -learning-session-key <k> -learning-run-id <r> -learning-context-sha256 <receipt sha>` to `air-worker validate` with the original validate arguments. A linked deferral remains exit 2, returns `outcome_receipt_path`, and does not run full validation. A refresh or used/outcome mutation invalidates the prior current delivery proof; immutable signed native intent/result receipts retain accepted historical evidence without re-signing the old delivery. Delivery, explicit mechanical action binding, and measured production benefit are separate claims. This source capability is a candidate until installed; ChatGPT does not run these hooks automatically. It does not prove cognitive GPT causation or complete HLP-05.

## HLP-03 reviewer reuse candidate (source only)

The production reviewer now reads the current verified APPLIED catalogue before review, with up to 32 complete procedures and 128 KiB of bodies; an incomplete or oversized catalogue fails before the provider call. The update-first and fresh-read workflow follows [Hermes background review](https://github.com/NousResearch/hermes-agent/blob/0e427d788d253630d38a3a51cd76849e73cd6925/agent/background_review.py#L460-L497). The reviewer may request an exact target and SHA update for a useful refinement of the same informational procedure; protected and machine-enabled lessons are ineligible. The existing shared learning module owns apply, backup and rollback. Review context reads do not record skill_loaded or procedure_used. Semantic equivalence remains reviewer judgment, so these routing tests do not prove how a live model will classify every lesson.

## 0.11.9 candidate — machine-verifiable shared learning (source accepted; release gated)

The AirWorker executable and dual-host Claude/Codex plugin consume the published `air-modules/learning v0.1.0`, without duplicating its writer. The native CLI records a signed failure (`run_id` and source), applies only an independently reviewed safe Markdown procedure, then credits it **only after** exact-SHA `skill_loaded`, a real later `procedure_used` and independently verified outcome/effect. In the accepted example, missing `run-config.json` causes **deferred validation** and prevents one redundant retry; this **is not a validation PASS**. A proposal, loaded file or signed intent without a final result is not successful use.

Operational commands are `air-worker version`, `selfcheck -json`, `plan spine -product <root> -json`, `plan node list -product <root> -open -json`, `plan-lint -product <root> -json`, `validate -product <root> -json` and (only for a configured self-learning owner) `learn self status -json`, `learn status`, `learn load` and `loop -product <root>`. `loop` R02–R04 is gated by the exact 0.11.9 candidate, configured self-learning owner and signed native runtime receipts. Do not treat an isolated E2E as a change of the main product's release-slice status.

Current status as of 20.10.2026: frozen Go source `12eae0104b07b496951f40353cd44f190ba29f6b` passed full QA and isolated real R02–R04; the **primary product has now also closed R02, R03 and R04 using the native 0.11.9 core** and exact signed capture/review/use receipts. R03 performed exactly one genuine model review and one safe APPLIED procedure; R04 confirmed one later module-owned `procedure_used` with signed intent/result and `effect=avoided_repeat_validation_with_missing_config`. This is a deferred validation, **not a full validation PASS**. `N-070`/`N-093` CLOSED, `N-071` OPEN; primary release-slices R05 OPEN and R06–R07 PLANNED. The current native loop implements R02–R04, not R05–R07. Release 0.11.9 is **NOT tagged, published or installed**; installed 0.11.8 is unchanged. N124 and DEV136 remain deferred. Portable release assets must match the final Git source SHA in both Windows CLI/tray and package SHA-256; Authenticode was waived for this version only. Publication and installation require separate exact LPR permissions; rollback target is installed 0.11.8.

> **Start here (0.10.x).** The product is the `air-worker` binary: judge, goal-drift engine,
> loop, planner, report, feedback, install/tray. Hooks only deliver host events to it; skills
> are optional help. A new session starts from the product instruction
> (`E:\-5-\011_Plugins\AirWorker_Wiki\PRODUCT_INSTRUCTION.md` on the AIR OS coordinator) and
> `PLAN.md`, section «Состояние и порядок старта». The released version is the newest
> `air-worker--v*` tag on `origin/main`; the installed one is what `air-worker install -status`
> reports. Branches `release/*`, `fix/*`, `wip/*` that are not merged into `main` are not releases.
>
> The sections below describe the heavy-task executor `skills/run-worker-task` (signed requests
> through `llm-queue`, default since 0.3.0). It is one executor inside the product, not the 0.10.x
> control plane.

`air-worker` is the canonical dual-host plugin for Claude and Codex. It executes
heavy non-swarm tasks through the existing `llm-queue`.
Since 0.3.0 its default is direct execution: a signed request is created and executed
locally immediately. Telegram approval remains available only as `--approval telegram`.

Install and release this directory as the one Claude+Codex package. The sibling
`../air-worker-codex` directory is a checkout-only compatibility adapter for older
Codex layouts; it runs this shared core and is not a release source. It contains no
duplicate executor, runtime, or secret.

## Direct execution

```text
python skills/run-worker-task/scripts/worker.py create openrouter-llm <file> \
  --param model=nvidia/nemotron-3.5-lightning:free \
  --param instruction="..." --privacy external
```

Set `AIR_WORKER_RUNTIME` to a host-local runtime directory when you need an explicit
location. Otherwise the resolver uses `AIR_RUNTIME_ROOT` when present, then the
platform state directory (`%LOCALAPPDATA%/air-worker` or `$XDG_STATE_HOME/air-worker`).
An existing legacy `E:\-4-\air-worker` state is read-compatible migration fallback only.
On hosts without the existing Windows keyring setup, supply `AIR_WORKER_HMAC_KEY`
through the host secret store. `AIR_WORKER_FREE_ONLY=1` is the default and only
admits `:free` OpenRouter models. `--privacy local` rejects external OpenRouter
execution.

The contract is HMAC-signed and includes the task type, parameters, input path and
input digest. Before execution the core rechecks the signature, registry, privacy,
input size and executor bounds. It uses a single atomic claim:

```text
queued → running → done | failed | invalid
```

An id cannot execute twice. A failed/interrupted task is recovered with a new signed
request; llm-queue owns bounded retries and concurrency, so this plugin does not
start a second competing process.

Direct queue dispatch now requires the queue's targeted `run-job` and
`show-job-json` capability contract. It atomically starts exactly one known queue
id, then polls that same id's redacted JSON receipt. The historical global
`run --limit` implementation is unsafe with concurrent jobs and is rejected
fail-closed; see the «Current release gate» section of
[skills/run-worker-task/SKILL.md](skills/run-worker-task/SKILL.md).

## Router dependency

The OpenRouter executor requires an OpenAI-compatible router at
`AIR_WORKER_ROUTER_URL` (default
`http://127.0.0.1:8090/v1/chat/completions`). Health checks use
`AIR_WORKER_ROUTER_MODELS_URL` (default `http://127.0.0.1:8090/v1/models`).
The router is an external host service: this cross-platform plugin does not read its
OpenRouter key, copy secrets, or silently start a privileged process. The current
Windows deployment supervises it with a user-level scheduled task; POSIX and other
hosts may use their native user service manager while keeping the same two URLs.

## Product vault

On the current AIR OS coordinator, the canonical product-owned vault is
`E:\-5-\011_Plugins\AirWorker_Wiki`. It contains roadmap, product tasks,
operations/runbooks, release history and accepted product knowledge. It is not a
runtime directory: requests, results, metrics, protocol and secrets remain under
the host-local `AIR_WORKER_RUNTIME`. `010_Task_Control_Platform` keeps only the
aggregated status and a link. Other hosts resolve the equivalent coordinate through
their AIR Storage registry instead of assuming the Windows drive path. The session entry
point there is `PRODUCT_INSTRUCTION.md`; the canonical and only development plan is
`PLAN.md` in this directory.

## Legacy Telegram

Use `--approval telegram` only when a human button is wanted. Then run
`listener.py --once 30`. The listener preserves shared-bot locking, callback-only
input and chat-id validation, but delegates execution to the same core.

## Observability and safety

`protocol.py` is append-only JSONL with separate external legs; `run_metrics.csv`
records model/counters/outcome. Prompt payload, secrets, and local input/output paths
are excluded from protocol and metrics. Results remain in the configured local
runtime, identified by task id and status.

Run `python skills/run-worker-task/scripts/selftest.py` from this plugin root for the
offline suite. It includes 36 checks; live route verification is separate. See
[skills/run-worker-task/SKILL.md](skills/run-worker-task/SKILL.md) for migration and acceptance.
