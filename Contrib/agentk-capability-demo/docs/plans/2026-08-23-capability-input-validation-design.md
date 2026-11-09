# Capability Input Validation Design

The Capability Lab rejects invalid arguments before creating an AgentK
execution. Validation remains catalog-driven: a browser module evaluates the
input schema for required fields, types, enums, numeric and length bounds,
array cardinality, URI format, object properties, and `anyOf`/`oneOf` groups.
Both generated forms and raw JSON use the same validator. Failures stay visible
above the form and highlight the affected fields.

Some staging manifests describe constraints only in prose or omit them. The
Demo therefore has a deliberately small catalog-enrichment layer for confirmed
contract gaps. The first overlay exposes the generic `agentk.generate_video`
duration as the string enum `4` through `15`, matching its implementation.
Cross-field rules cover video segment ranges, paired first/last frames,
feature-video audio, and Seedance reference-audio requirements.

These overlays do not hide provider contract defects. Every gap found during
staging tests is also recorded in the capability test report so it remains
visible to AgentK maintainers. Unit tests cover generic schema validation,
cross-field rules, and known schema enrichment.
