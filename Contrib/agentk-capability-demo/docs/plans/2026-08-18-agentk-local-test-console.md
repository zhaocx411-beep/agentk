# AgentK Local Test Console Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build and launch a local web console that dynamically tests every AgentK capability and exercises the comic-drama agent-loop cases through the Python SDK.

**Architecture:** A FastAPI process owns the AgentK Python SDK client, API key, profile catalog, upload storage, and normalized browser API. A framework-free browser client renders JSON Schema forms for capabilities and folds AgentK event streams into chat, tool, question, progress, and artifact views.

**Tech Stack:** Python 3.11+, FastAPI, Uvicorn, AgentK Python SDK, vanilla HTML/CSS/JavaScript, pytest.

---

### Task 1: Project shell and configuration

**Files:**
- Create: `pyproject.toml`
- Create: `src/agentk_capability_demo/config.py`
- Create: `src/agentk_capability_demo/__main__.py`
- Create: `src/agentk_capability_demo/app.py`
- Test: `tests/test_config.py`

1. Add failing tests for CLI/environment precedence, URL validation, and secret redaction.
2. Run `uv run pytest tests/test_config.py -q` and confirm failure.
3. Implement immutable settings and CLI flags `--agentk-address`, `--api-key`, `--host`, `--port`, and `--reload`.
4. Add a FastAPI app factory, health route, and static asset serving.
5. Run the focused test and confirm it passes.

### Task 2: Python SDK adapter and capability API

**Files:**
- Create: `src/agentk_capability_demo/agentk_client.py`
- Create: `src/agentk_capability_demo/capabilities.py`
- Create: `src/agentk_capability_demo/serialization.py`
- Test: `tests/test_capabilities.py`
- Test: `tests/fakes.py`

1. Add fake-SDK tests for paginated capability discovery, session/pipeline lookup, execution start, event forwarding, result normalization, cancellation, and error mapping.
2. Implement a public `AsyncClient` adapter with a fixed-address ConnectRPC provider.
3. Implement `GET /api/capabilities`, `POST /api/capabilities/{id}/runs`, event streaming, status, and cancellation routes.
4. Convert protobuf messages to stable lower-camel JSON without exposing SDK implementation details.
5. Run the focused test suite.

### Task 3: Agent-loop catalog and interaction API

**Files:**
- Create: `src/agentk_capability_demo/agents.py`
- Create: `src/agentk_capability_demo/event_fold.py`
- Create: `src/agentk_capability_demo/catalog/*.json`
- Test: `tests/test_agents.py`
- Test: `tests/test_event_fold.py`

1. Add tests for the eight comic-drama chat profiles plus the comic-drama pipeline case.
2. Add tests that fold text/reasoning deltas, tool calls, invocation progress, artifacts, ask-user questions, failures, and terminal events.
3. Implement session creation from vendored profiles, message and image attachment execution, answer submission, pipeline execution, status, history, and cancellation endpoints.
4. Keep session state in memory for the local demo and make all resources disposable.
5. Run the focused test suite.

### Task 4: Schema-driven web console

**Files:**
- Create: `src/agentk_capability_demo/static/index.html`
- Create: `src/agentk_capability_demo/static/app.css`
- Create: `src/agentk_capability_demo/static/app.js`
- Test: `tests/test_app.py`

1. Add app smoke tests for the page and browser API contract.
2. Build an industrial test-bench interface with Capability Lab and Agent Loop workspaces.
3. Render JSON Schema recursively, including enums, arrays, nested objects, raw JSON fallback, URI fields, and local uploads converted to data URLs where supported.
4. Render live event timelines, structured output, media artifacts, tool status, reasoning disclosure, questions, retries, and cancellation.
5. Add responsive layout, keyboard-friendly controls, empty/loading/error states, and reduced-motion support.
6. Run all Python tests and static syntax checks.

### Task 5: Documentation and live verification

**Files:**
- Modify: `README.md`
- Create: `.gitignore`

1. Document installation, address/key startup flags, supported cases, limitations, and test commands.
2. Start the server on `127.0.0.1:8080`.
3. Verify `/healthz`, `/`, and the disconnected-backend error state.
4. Open the page in the local browser, inspect desktop/mobile states, and fix visual defects.
5. Report the running URL, test results, and any backend prerequisites.
