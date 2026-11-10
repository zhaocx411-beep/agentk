# JSS evidence gate

## Research evidence

- [ ] Both boundary annotators complete the separate 30-case P01 calibration exercise before formal labeling.
- [ ] P01 QA privacy review is complete.
- [ ] P02–P10 A/B boundary annotations are complete and independently authored.
- [ ] Every disagreement is adjudicated and Cohen's kappa is reported.
- [ ] Every retained QA passes content, exact-evidence, and privacy review.
- [ ] `scripts/audit_review_progress.py --require-complete` passes with no invalid or partial rows.
- [ ] `scripts/audit_formal_readiness.py --require-ready` passes 10/10 projects.
- [ ] The frozen 15-method BGE-M3 GPU matrix completes for P01–P10.
- [ ] Formal analysis uses only P04–P10 for the primary locked-test estimates.
- [ ] All formal run/config/input/implementation hashes validate.
- [ ] `scripts/fill_manuscript_results.py` replaces exactly 130 result slots.
- [ ] `scripts/lint_manuscript.py --require-final` passes.

## Confidentiality and release

- [ ] No customer/vendor/project identity occurs in the submission manuscript.
- [ ] Raw workbooks, corpora, and document-derived QA are excluded from public artifacts.
- [ ] Data owner has approved the exact examples and aggregate fields, if any, selected for release.
- [ ] Anonymous review artifact passes the sensitive-identifier and secret scan.
- [ ] Repository URL/DOI and data-availability language match the released artifact.

## JSS files and metadata

- [ ] Abstract is at most 250 words.
- [ ] There are 1–7 English keywords.
- [ ] Highlights contain 3–5 bullets, each at most 85 characters.
- [ ] Word manuscript is editable, single-column, and below 36 pages or justified.
- [ ] Equations and tables are editable, numbered, captioned, and cited.
- [ ] Citations are author–year and references are alphabetized.
- [ ] Title page contains verified author names, affiliations, ORCIDs, and corresponding-author details.
- [ ] Funding, competing interests, CRediT roles, acknowledgements, and biographies are confirmed.
- [ ] AI-use declaration is reviewed and approved by every author.
- [ ] Cover letter contains the exact locked-test conclusion, not a stronger paraphrase.
- [ ] Final DOCX and generated PDF are rendered and visually checked page by page.
