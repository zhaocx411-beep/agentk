# Qwen TTS Instruct Fallback Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Use Qwen instruction control only when AgentK advertises `qwen3-tts-instruct-flash`, while keeping ordinary Qwen Flash rendering functional without `instructions`.

**Architecture:** The browser config endpoint already reads the live AgentK capability schema. It will filter advertised models through the deployment allowlist and prefer the instruct model when present. The Temporal render workflow will independently enforce provider compatibility by adding `instructions` only for an instruct-capable model, so stale or manually submitted Flash requests remain safe.

**Tech Stack:** Python 3.11+, FastAPI, Temporal workflows, pytest, AgentK Python SDK.

---

### Task 1: Lock the compatibility behavior with tests

**Files:**
- Modify: `tests/test_app.py`
- Modify: `tests/test_narrato_workflows.py`
- Modify: `tests/test_narrato_settings.py`

1. Add a config-route test where AgentK advertises both Qwen models and assert that `qwen3-tts-instruct-flash` is selected.
2. Add a config-route fallback test where only `qwen3-tts-flash` is advertised.
3. Change the Qwen render test to assert that the instruct model receives combined global and cue instructions.
4. Add a Flash render test asserting that `instructions` is absent.
5. Run the focused tests and confirm they fail before implementation.

### Task 2: Implement model discovery and safe request construction

**Files:**
- Modify: `src/agentk_capability_demo/agent100/narrato/settings.py`
- Modify: `src/agentk_capability_demo/agent100/narrato/router.py`
- Modify: `src/agentk_capability_demo/agent100/narrato/workflows/documentary.py`
- Modify: `config.narrato.example.yaml`
- Modify: `README.md`

1. Add `qwen3-tts-instruct-flash` to the Qwen deployment candidates while keeping ordinary Flash as fallback.
2. Intersect live schema models with configured candidates and prefer the instruct model only when advertised.
3. Add `instructions` to Qwen activity payloads only for the instruct model.
4. Document the automatic downgrade behavior.
5. Run focused tests and confirm they pass.

### Task 3: Verify locally and against AgentK

**Files:**
- Modify if successful evidence is available: the README E2E record

1. Run Ruff and the full pytest suite.
2. Query the live AgentK capability catalog and record whether the instruct model is advertised.
3. Execute Qwen speech E2E with the advertised model and validate the audio artifact.
4. If the local Narrato worker and media fixture are available, run the render pipeline E2E and confirm terminal success; otherwise report the exact environmental blocker without weakening local verification.
