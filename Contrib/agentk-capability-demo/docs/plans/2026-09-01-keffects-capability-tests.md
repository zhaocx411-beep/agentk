# Keffects Capability Test Support Plan

**Goal:** Make the dynamic Capability Lab directly usable for the new Keffects capabilities and the new `kling_omni_video.subjectLists` input, without duplicating AgentK capability manifests.

**Architecture:** AgentK remains the source of truth for capability discovery, input schemas, defaults, descriptions, and execution. The demo adds only generic form behavior, small capability-aware examples, browser-side cross-field validation, and regression fixtures that prove catalog and execution payloads are passed through unchanged.

## Task 1: Capture the new contracts in tests

- Add frontend fixtures for `create_voice_embedding`, `create_kling_subject`, `identify_faces`, `lip_sync_video`, and `kling_omni_video.subjectLists`.
- Verify usable required-field examples, media upload controls, exclusive subject input modes, and lip-sync time validation.
- Add an API regression test proving all new and extended arguments survive the SDK pipeline request.

## Task 2: Improve schema-driven form behavior

- Materialize the first `oneOf`/`anyOf` required-input branch when creating form examples.
- Support direct `audio` and `video` URL fields as upload targets while leaving enum-valued generation modes alone.
- Add readable examples for subject names, face identifiers, and millisecond lip-sync timing.
- Implement JSON Schema `not` and the lip-sync end-time cross-field rule.

## Task 3: Document and verify

- Add the four Keffects capabilities and their business ordering to the integrated capability inventory.
- Document the new fields and PRT-only availability of `create_voice_embedding`.
- Run all Python tests, all Node frontend tests, and Ruff checks.

