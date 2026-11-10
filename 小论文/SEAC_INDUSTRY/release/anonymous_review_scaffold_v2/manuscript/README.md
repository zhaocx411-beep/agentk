# SEAC industry manuscript status

The authoritative paper source is `seac_industry_manuscript.md`. The original
DOCX at the workspace root is preserved and is not the submission version.

Current status: **experiment-ready draft, not submission-ready**.

The three verified review workbooks are available under
`../outputs/annotation_workbooks_v1/`; they are internal confidential artifacts,
not publication supplements. Completing them is the current critical path.

Blocking items before a clean DOCX may be generated:

1. P01 human privacy release for 27 QA records.
2. P02–P10 double-blind boundary annotation and adjudication.
3. P02–P10 QA approval, exact evidence verification, and privacy release.
4. GPU execution of the frozen 15-method matrix.
5. Statistical aggregation and insertion of formal results.
6. Author names, affiliations, corresponding-author details, and target venue.
7. Written data-owner decision on whether anonymized examples or benchmark
   records may be released.

The tokens `{{FORMAL_*}}` in the manuscript are deliberate machine-readable
result slots. They must all be replaced before generating the final DOCX.
The P01 CPU BGE-M3 numbers under `experiments/P01_strict_unit_bge_m3_v1` are
validation-only and must not be copied into the formal result tables.
