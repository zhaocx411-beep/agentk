# Public artifact schemas

These JSON Schema 2020-12 documents describe the release-safe interfaces used
by the SEAC pipeline. They specify structure, not permission to release any
enterprise-derived record. Real corpus, boundary, and QA files remain subject
to the separate privacy and data-owner gates.

- `corpus.schema.json`: parsed documents, blocks, and table rows;
- `boundary.schema.json`: adjudicated adjacent-unit boundary records;
- `qa.schema.json`: approved provenance- and span-aware retrieval questions.

The invented files under `examples/synthetic/` illustrate all three schemas.
