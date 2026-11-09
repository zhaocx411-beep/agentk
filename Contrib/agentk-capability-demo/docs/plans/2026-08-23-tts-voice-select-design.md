# TTS Voice Select Design

## Problem

The capability lab currently renders `voiceId` as a free-text field because the
AgentK capability schemas declare it as a required string without an `enum`.
The generated example `your-voice-id` is therefore submitted as if it were a
real voice and causes avoidable failures.

## Design

The demo server enriches the discovered TTS capability schemas with an
`x-options` array. Qwen and MiniMax options mirror AgentK's built-in provider
voice catalogs. The generic schema form renders `x-options` with the same
select control used for enums, including human-readable descriptions.

CosyVoice is filtered from the public capability catalog. It has no built-in
system voices and this product does not expose account-scoped enrolled or
designed voice IDs.

This is an additive JSON Schema extension. Raw capability execution payloads
remain unchanged, and ordinary schema enums keep their existing behavior.

## Verification

- Python tests verify Qwen, MiniMax, hidden CosyVoice, and unrelated capability schema
  enrichment.
- JavaScript tests verify option normalization and generated defaults.
- Existing API and frontend test suites must remain green.
- Browser verification checks that Qwen and MiniMax show selectable voices and
  that CosyVoice is absent from the public catalog.
