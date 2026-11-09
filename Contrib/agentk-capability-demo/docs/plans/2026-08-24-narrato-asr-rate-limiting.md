# Narrato ASR Rate Limiting Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Keep Narrato's Azure Whisper traffic within a deployment-wide six-calls-per-minute quota while making transient HTTP 429 failures recover automatically.

**Architecture:** Each script workflow performs ASR serially and must acquire a lease from one namespace-wide Temporal coordinator before scheduling `agentk.capability.transcribe_audio`. Every worker replica idempotently starts the coordinator with a fixed workflow ID and `USE_EXISTING`, so concurrent Narrato executions share one FIFO queue; it enforces one in-flight lease and an 11-second minimum start interval. The ASR Activity uses its own longer exponential retry policy, while all other Core Activities retain their existing policy.

**Tech Stack:** Python 3.11+, Temporal Python SDK 1.31, AgentK worker SDK, pytest/pytest-asyncio.

---

### Task 1: Add validated ASR traffic settings

**Files:**
- Modify: `src/agentk_capability_demo/agent100/narrato/settings.py`
- Modify: `config.narrato.example.yaml`
- Test: `tests/test_narrato_settings.py`

**Steps:**
1. Add failing tests for defaults, YAML/environment loading, and invalid values.
2. Run `uv run pytest tests/test_narrato_settings.py -q` and confirm failure.
3. Add `asr_concurrency=1`, `asr_min_interval_seconds=11`, `asr_global_concurrency=1`, `asr_lease_timeout_seconds=900`, and a stable coordinator workflow ID.
4. Map the settings to `NARRATO_ASR_*` environment variables and validate positive, bounded values.
5. Document the staging-safe defaults in the example YAML.
6. Re-run the settings tests and confirm they pass.

### Task 2: Add a durable global Temporal coordinator

**Files:**
- Create: `src/agentk_capability_demo/agent100/narrato/workflows/asr_rate_limiter.py`
- Modify: `src/agentk_capability_demo/agent100/narrato/workflows/__init__.py`
- Modify: `src/agentk_capability_demo/agent100/narrato/worker.py`
- Test: `tests/test_narrato_asr_rate_limiter.py`
- Test: `tests/test_narrato_worker.py`

**Steps:**
1. Write failing unit tests for FIFO grants, duplicate requests, release, lease expiry, and minimum spacing.
2. Run the new tests and confirm failure.
3. Implement a non-published `narrato.asr.rate_limiter.v1` workflow with request/release signals.
4. Store queued requests and active lease expirations in workflow state; use Temporal timers only.
5. Signal grants back to the exact requesting workflow/run and continue-as-new periodically to bound history.
6. Register the coordinator in every Narrato worker without adding it to the public AgentK capability manifest.
7. Run limiter and worker tests and confirm they pass.

### Task 3: Gate ASR calls and add ASR-specific retries

**Files:**
- Modify: `src/agentk_capability_demo/agent100/narrato/workflows/documentary.py`
- Test: `tests/test_narrato_workflows.py`
- Test: `tests/test_narrato_workflow_io_contracts.py`

**Steps:**
1. Add failing tests asserting ASR uses a dedicated retry policy and expanded schedule-to-close timeout.
2. Add failing tests asserting a permit is acquired before every ASR Activity and released in `finally` on success or failure.
3. Add a grant signal handler to `GenerateDocumentaryScriptWorkflow`.
4. Signal the coordinator workflow started idempotently by each worker replica and wait for a grant addressed to the exact workflow/run.
5. Change ASR batching to `asr_concurrency` (default one) without reducing video-understanding concurrency.
6. Add an ASR retry policy: each attempt has a 45-second hard timeout, with a 15-second initial interval, coefficient 2, one-minute maximum interval, four attempts, and a five-minute schedule-to-close budget. This yields increasing waits of 15, 30, and 60 seconds before the final attempt; after that, only the affected window falls back to no dialogue.
7. If all retry attempts still fail with a transient provider error, degrade only that analysis window to an empty transcript and continue with visual evidence. Deterministic input, configuration, and invalid-output failures remain terminal.
8. Run the workflow tests and confirm they pass.

### Task 4: Verify behavior and compatibility

**Files:**
- Modify as required by test findings only.

**Steps:**
1. Run `uv run ruff check src tests`.
2. Run the focused suite: `uv run pytest tests/test_narrato_settings.py tests/test_narrato_asr_rate_limiter.py tests/test_narrato_worker.py tests/test_narrato_workflows.py tests/test_narrato_workflow_io_contracts.py -q`.
3. Run `uv run pytest -q`.
4. Inspect the diff for accidental changes and verify the public capability manifest still contains only the three Narrato pipelines.
5. Record any staging deployment configuration needed to share the same coordinator workflow ID and Temporal namespace.
