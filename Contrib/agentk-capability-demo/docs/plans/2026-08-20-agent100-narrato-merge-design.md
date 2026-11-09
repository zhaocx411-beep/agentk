# Agent 100 / Narrato Merge Design

## Goal

Merge the existing `narrato-agentk` test application and Worker into `agentk-capability-demo`,
which remains the canonical repository. The browser exposes three top-level workspaces:
Capability Test, Agent Loop Test, and Agent 100 Test. Narrato is the first Agent 100 module.

## Architecture

Agent 100 is a registry of isolated agent modules. Shared code owns navigation, the AgentK
consumer client, execution bookkeeping, and application lifecycle. Each registered agent owns
its metadata, browser UI, API router, configuration, contracts, workflows, prompts, and Worker
factory. Narrato lives below `agentk_capability_demo.agent100.narrato`; future agents are added as siblings
and do not import Narrato implementation details.

The web process and Worker remain separate runtime roles. `agentk-capability-demo` serves all three browser
tabs and proxies Narrato pipeline executions through the existing AgentK client. A new worker
command receives an Agent 100 id and starts only that agent's Worker/task queue. This prevents a
web restart or horizontal scale-out from duplicating long-running workers.

## Browser and API

The main page gains a third native workspace. Its left rail is populated from an Agent 100
catalog endpoint and selects Narrato initially. The Narrato control deck is migrated into the
same DOM, with all IDs, CSS selectors, and JavaScript state names scoped to the Narrato module.
It retains three pipelines, model and analysis controls, upload, request preview, progress,
cancel, JSON download, artifact links, and script-to-render handoff.

Narrato routes live under `/api/agent100/narrato/*`: config/readiness, media upload, execution
start/status, and cancel. Generic capability and Agent Loop routes remain unchanged. AgentK
credentials stay server-side. Narrato media keeps its 512 MiB video validation and managed
HTTP(S) URI requirement instead of changing the generic upload contract.

## Migration and Compatibility

Narrato contracts, prompts, workflow logic, Core ABI checks, media storage, and Worker code are
ported to AgentK SDK/Worker `0.2.2` and Python `>=3.11,<3.14`. The old standalone Narrato CLI and
standalone FastAPI application are not migrated. Narrato-specific model, Temporal, task queue,
and media settings remain namespaced; shared AgentK address and API key use the demo settings.
The original `narrato-agentk` repository is left untouched and becomes the historical source.

## Verification

Unit tests cover registry isolation, Narrato configuration and contracts, Worker publication,
API validation and execution lifecycle, media limits, and existing demo regressions. Frontend
tests cover top-level tab switching and Narrato request/state helpers. Browser verification must
show all three tabs, open Narrato from Agent 100, switch its three pipelines, preview a selected
video request, and exercise error/readiness states without console errors. A live pipeline run
is attempted when AgentK, Temporal, Core Worker, BlobStore, and model services are available;
otherwise the exact unavailable dependency is reported separately from offline tests.
