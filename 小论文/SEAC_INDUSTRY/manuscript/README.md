# SEAC industry manuscript status

The authoritative paper source is `seac_industry_manuscript.md`. The original
DOCX at the workspace root is preserved and is not the submission version.

Current status: **formal experiment and result-complete long paper; BigComp 2027
conference adaptation in progress**.

The three verified review workbooks are available under
`../outputs/annotation_workbooks_v1/`; they are internal confidential artifacts,
not publication supplements. Completing them is the current critical path.

The former experiment blockers below are complete:

1. P01 human privacy release for 27 QA records.
2. P02–P10 double-blind boundary annotation and adjudication.
3. P02–P10 QA approval, exact evidence verification, and privacy release.
4. GPU execution of the frozen 15-method matrix.
5. Statistical aggregation and insertion of formal results.
6. Author names, affiliations, and corresponding-author details remain
   deliberately unresolved; the target venue is IEEE BigComp 2027.
7. Written data-owner decision on whether anonymized examples or benchmark
   records may be released.

The authoritative placeholder source remains `seac_industry_manuscript.md`.
The completed long-form evidence version is
`seac_industry_completed_20260824.md`; it contains all 144 injected values and
passes the final manuscript lint. The P01 CPU BGE-M3 numbers under
`experiments/P01_strict_unit_bge_m3_v1` remain validation-only and are not used
in the formal result tables.
