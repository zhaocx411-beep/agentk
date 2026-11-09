# AgentK Capability Naming Alignment

## Naming model

AgentK uses separate names for separate layers. A public capability ID is the
stable cross-repository identity and must be either `agentk.<name>` or
`contrib.<owner>.<name>`. Every segment is lowercase snake_case, starts with a
letter, and the complete ID is at most 128 characters. A Temporal type is an
internal routing detail and must not be presented as a capability ID. Profile
tool and pipeline names are session-local aliases and are also not capability
identities.

The demo already uses compliant public IDs, including the three Narrato IDs.
Those IDs stay unchanged. Existing AgentK Core Temporal ABI strings also stay
unchanged because changing them would break running workers. The code will make
the distinction explicit by naming the Core mapping `CoreTemporalTypes` instead
of the ambiguous `CoreTemporalTypes`.

## Project-wide migration

The product and package identity becomes `AgentK Capability Demo` everywhere:

- Python distribution: `agentk-capability-demo`
- Python package: `agentk_capability_demo`
- CLI: `agentk-capability-demo` and `agentk-capability-demo-worker`
- environment prefix: `AGENTK_CAPABILITY_DEMO_`
- user-facing brand: `AgentK`, without the legacy `AgentK` spelling

Narrato remains a named integrated capability provider below `agent100`; its
business types, configuration prefix, and published capability IDs are outside
the project-brand rename. Its existing Temporal Task Queue is also retained as
a runtime ABI. Historical design documents and test evidence retain their
original facts, but live commands and file references are updated where they
would otherwise send developers to obsolete entry points.

## Verification

Verification covers import/package discovery, CLI and container entry points,
all Python tests, all Node tests, Ruff, and JavaScript syntax checks. Searches
must show no live legacy package, CLI, environment-prefix, or brand references
outside intentionally historical evidence.
