# ADR-0001: Conditional bounded web research for Narrato

## Status

Accepted

## Context

Narrato currently derives documentary scripts only from ASR and visual evidence. Public film,
television, game, historical, or news footage may need external context to disambiguate characters
and plot. Original or private footage usually does not, so unconditional search adds latency, cost,
and unrelated claims. `agentk.generate_text` is a structured generation Activity rather than an
autonomous tool-calling loop, while staging publishes `agentk.web_search` as a stable Activity.

Search results are untrusted external text and may contain prompt injection or unsupported claims.
Search availability must not become a new single point of failure in the long-video workflow.

## Decision

Add a bounded research phase after multimodal evidence collection and before story analysis:

1. Ask the story model for a strict research plan with `needed`, `reason`, and zero to five unique
   queries of at most 100 characters.
2. Skip search when `needed=false`. Invalid plans or transient planning failures default to no
   research.
3. When needed, invoke `agentk.web_search` once per query, using the configured search model.
   Each query has the normal Core retry policy; individual search failures are recorded as degraded
   progress and do not fail script generation.
4. Normalize results to title, URL, and summary, cap their size, label them as untrusted reference
   material, and inject them only into story analysis. Video/ASR evidence remains authoritative;
   external research may resolve public context but may not override contradictory source evidence.
5. Expose the search model through the existing model selection contract and validate it against
   the live AgentK capability schema at worker startup.

This is deterministic workflow orchestration, not an unbounded agent loop. The maximum number of
provider calls, Temporal history growth, latency, and cost are therefore bounded per execution.

## Consequences

### Positive

- Searches only when the evidence indicates public context is useful.
- Supports multiple complementary queries while keeping cost and history bounded.
- Preserves Temporal replay determinism and the existing structured-output validation pipeline.
- Search outages degrade cleanly to video-only generation.
- URLs remain attached to normalized research items for source-aware reasoning.

### Negative

- Query planning is one additional model call even when search is skipped.
- A fixed plan cannot adapt query-by-query as freely as an autonomous agent loop.
- Search summaries are not full-page evidence and can still be inaccurate.

### Neutral

- The output script schema remains unchanged; research is internal workflow evidence.
- `volc-search` is the default because the generated narration and likely queries are Chinese.

## Alternatives Considered

**Always search**

- Rejected because it wastes calls on private/original footage and increases unsupported context.

**Replace the workflow with an autonomous tool-calling agent**

- Rejected for now because it weakens deterministic bounds, complicates structured contracts and
  Temporal replay, and is a much larger migration.

**Single fixed query derived from the theme**

- Rejected because one query cannot reliably cover title identification, characters, and plot, and
  user-provided themes are often generic.

## Non-Functional Requirements

- At most five search calls per script execution.
- Search failures never abort script generation.
- Deterministic configuration errors remain terminal during worker startup.
- Untrusted search text is explicitly separated from instructions in prompts.
- Progress events expose whether research was skipped, completed, or degraded.

## References

- AgentK Core `agentk.web_search` Activity schema.
- [Narrato ASR rate limiting plan](../plans/2026-08-24-narrato-asr-rate-limiting.md)
