# Capability Form Examples Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use executing-plans to implement this plan task-by-task.

**Goal:** Prefill each capability form with safe, editable examples so a local tester can understand required parameters and modify them before running.

**Architecture:** Keep AgentK's live JSON Schema as the source of truth. Add a small browser-side default generator that prefers schema defaults and examples, then enum values, then semantic examples for required fields. Optional fields remain empty unless their schema declares a default, preventing examples from silently enabling extra behavior.

**Tech Stack:** Vanilla JavaScript, HTML/CSS, Node's built-in test runner, pytest, FastAPI static assets.

---

### Task 1: Build and test the example generator

**Files:**
- Create: `src/agentk_capability_demo/static/form-defaults.js`
- Create: `tests/frontend_defaults.test.js`

1. Add failing Node tests for schema defaults, enums, URLs, prompts, numeric ranges, arrays, objects, and `oneOf` operations.
2. Run `node --test tests/frontend_defaults.test.js` and confirm failure.
3. Implement `defaultsFor(schema, capabilityId)` and `exampleFor(...)` as a browser/CommonJS module.
4. Run the Node tests and confirm they pass.

### Task 2: Integrate examples into the capability form

**Files:**
- Modify: `src/agentk_capability_demo/static/index.html`
- Modify: `src/agentk_capability_demo/static/app.js`
- Modify: `src/agentk_capability_demo/static/app.css`

1. Load the generator before `app.js` and replace the existing minimal default builder.
2. Add a visible “已填入示例值” note and a “恢复示例” action.
3. Preserve raw JSON mode, uploads, user editing, schema validation, and run submission behavior.
4. Reload the browser and verify representative simple, media, and complex-array capabilities.

### Task 3: Review, verify, and ship

**Files:**
- Modify: `README.md`

1. Document the precedence and that URL/deployment aliases must be replaced when appropriate.
2. Run `uv run ruff check src tests`, `uv run pytest -q`, Node syntax/tests, and `git diff --check`.
3. Review the full diff for accidental optional-parameter activation and misleading model defaults.
4. Commit and push the tested change to `origin/master`.
