# Industrial SEAC Frozen Experimental Protocol

Protocol version: `industrial-seac-v1.1`

Version 1.1 was frozen before any locked-test retrieval result was released. It
changes only statistical inference and latency aggregation: paired observations
are randomized as whole projects; the sole confirmatory test is SEAC versus the
official LlamaIndex SentenceSplitter on unit MRR; and latency percentiles are
computed from pooled per-query measurements. Corpus membership, splits, methods,
hyperparameters, annotations, and retrieval metrics are unchanged.

## Scope

The paper studies chunking and evidence retrieval for heterogeneous enterprise
software-engineering documents: requirements specifications, detailed designs,
API specifications, database designs, technical agreements, proposals,
research reports, and meeting minutes. Unrelated domain-specific RAG directions
and non-software-engineering applications are out of scope.

## Dataset split

- P01-P02: development only.
- P03: validation and model selection.
- P04-P10: locked cross-project test set.

P04-P10 must not influence SEAC parameters, hierarchy weights, query wording,
or baseline selection after the protocol is frozen. Their raw documents may be
parsed and annotated, but retrieval results must not be inspected until the
annotation audit is complete and the final configuration is locked.

## Frozen SEAC configuration

The paper name is **SEAC**. The implementation identifier remains
`proposed_v6` for artifact compatibility.

- maximum parent length: 512 tokens;
- parent soft target: 320 tokens;
- dense-region target: 224 tokens;
- minimum parent length: 48 tokens;
- child target: 112 tokens;
- child maximum: 176 tokens;
- boundary weights (semantic/structure/key-density change): 0.45/0.30/0.25;
- same-heading continuity penalty: 0.35; same protected-group penalty: 1.0;
- document-level threshold adjustments: +0.08 × table-row ratio and
  −0.05 × heading ratio, applied to the 0.8 boundary-score quantile;
- parent/child retrieval weights: 0.35/0.65;
- formal encoder: BAAI/bge-m3;
- shared fair-context metadata for every method.

Any later parameter change creates a new protocol version and must be justified
using development/validation evidence only.

## Formal annotation gates

P02-P10 use a deterministic, blinded, document- and structure-stratified
sample of 300 adjacent-unit boundaries per project (`seed=20260804`). P01
retains its full human double-annotated and adjudicated boundary set.

For P02-P10 the boundary reference is built by the LLM pre-label plus human
sample verification path of `docs/annotation_and_privacy_protocol.md` section
4bis. A language model labels all 2,700 blinded candidates without access to any
method output; a human then independently labels a stratum-proportional random
sample of 100 candidates per project plus every model-flagged candidate, blind to
the pre-label. Promotion requires complete verification rows and per-project
human-model `kappa >= 0.70`, and human labels override pre-labels. Every
promoted boundary keeps `label_source` and `human_verified`, and the paper must
report the human-verified and model-labelled shares. This path must be disclosed
as a mixed reference standard; it may not be described as human double
annotation.

4ter. Current boundary status. Verification is complete: all 1,050 rows are
judged, four flagged ambiguous with reasons. Round one exposed three families of
evidence-free adjacent units that the written noise rule did not enumerate, and
the rule was amended to settle them deterministically (protocol
`annotation_and_privacy_protocol.md` §4.1bis). The amendment is a post-hoc
specification and the paper reports agreement both before and after it. Eight
projects passed and carry 2,400 promoted boundaries: 783 `HUMAN_VERIFIED`
(32.6%), 387 `RULE_4_1` (16.1%), 1,230 `LLM_PRELABEL` (51.2%). P08 measured
kappa 0.650 and is withheld: its residual disagreement concerns cover-page
furniture appearing once or twice per document, which has no label-independent
justification, and no further noise category was added to clear the threshold.
Boundary metrics therefore cover P04–P07, P09 and P10 in the locked test set;
P08 retrieval and QA metrics are unaffected. The manifest records the omission
through `boundary_withheld` and `boundary_withheld_reason`, and the readiness
audit refuses a withheld project that still has a promoted boundary file.

4quater. Current QA status. Human final approval is complete: all 225 candidates
across P02–P10 are judged, 205 approved and 20 rejected. Sixteen of the twenty
rejections are one generator defect — the question names a document section
instead of asking the fact, so its scope is set by the section rather than by the
question, which §6.1 forbids. Round one applied §6.1 unevenly on a related
family: ten approved questions still carried a leading section number in front of
an otherwise self-contained subject. That part of §6.1 needs no judgement and was
fixed as a mechanical rule (protocol §6.1bis), which excludes those ten at import
without editing the reviewer's decision. Like §4.1bis this is a post-hoc
specification, and the paper reports question counts before and after it. Eight
projects passed with 184 approved questions. P10 retained 11 of 18 candidates,
below the 15-question minimum, and is withheld from retrieval reporting; its 11
questions are individually valid and stay on disk, and its boundary metrics are
unaffected. The manifest records the omission through `qa_withheld` and
`qa_withheld_reason`, and the readiness audit refuses the flag on a project whose
question count actually meets the minimum.

A withheld axis is passed to the runner as `none` rather than as a missing path,
so the axis is reported as absent: `results.json` carries `null` for it, no detail
file is written, and the CSV columns are left empty instead of zero.

The normative label decisions, calibration, adjudication, QA approval, and
privacy release rules are defined in `docs/annotation_and_privacy_protocol.md`.

Boundary evaluation:

- requires an adjudicated `human_label` for every candidate;
- accepts `BREAK`, `NO_BREAK`, and `NOISE`;
- excludes `NOISE` from binary boundary precision/recall/F1;
- never falls back to `suggested_label` in a formal run.

Retrieval evaluation:

- uses only QA records marked KEEP/APPROVED/VERIFIED or their Chinese equivalents;
- records block-level evidence for compatibility;
- requires row/unit-level evidence for table questions in the final benchmark;
- credits a hit on any member of a question's gold evidence set, since verbatim
  passage reuse makes several locations equally defensible as the evidence;
- reports both evidence-id and exact evidence-span MRR/Hit@K.

Exploratory runs may explicitly pass `--allow-unreviewed-qa` and
`--allow-provisional-breakpoints`. Results produced with either flag are not
paper-reportable.

## Baselines and ablations

Required baselines:

- fixed-size;
- recursive character;
- structure-only;
- semantic;
- LangChain RecursiveCharacterTextSplitter;
- LlamaIndex SentenceSplitter;
- SEAC without hierarchy (`proposed`);
- full SEAC (`proposed_v6`).

Required ablations remove semantic distance, key-information density,
continuity penalties, adaptive thresholds, heading-path context,
table-row children, and parent-child fusion one at a time.

The no-density variant removes both the density term in the boundary score and
the density-driven 224-token target. The no-continuity variant zeros both
numeric terms in the continuity penalty while retaining the hard rule that a
protected table group cannot be split by an optional semantic cut. This keeps
each ablation aligned with the component definition without creating malformed
table parents.

The experiment runner exposes these as `seac_no_semantic`,
`seac_no_density`, `seac_no_continuity`,
`seac_no_adaptive_threshold`, `seac_no_heading_context`,
`seac_no_table_children`, and `seac_no_fusion`. The LangChain and LlamaIndex
baselines call the official `langchain-text-splitters` and `llama-index-core`
implementations rather than local approximations.

## Reported metrics

- boundary precision, recall, and F1;
- MRR and Hit@1/3/5 at block and unit evidence levels;
- evidence-span MRR and Hit@1/3/5;
- per-project and per-question-type results;
- chunk count, parent/child count, token statistics, indexing time, and query latency;
- hierarchical bootstrap confidence intervals;
- paired project-cluster randomization tests, using all 128 project-level sign
  assignments for the seven locked projects;
- one two-sided, unadjusted confirmatory test of SEAC versus the official
  LlamaIndex SentenceSplitter on unit MRR, frozen before locked-test access;
- Holm correction across the remaining 55 secondary retrieval tests and,
  separately, across the 14 boundary-F1 comparisons;
- pooled per-query latency mean, median, and 95th percentile.

## Current evidence status

- P01 boundary gold: complete, double annotated and adjudicated.
- A frozen 30-case P01 calibration exercise and separately controlled answer
  key are ready. They cover all nine observed boundary-type/label combinations
  and six formerly disputed cases; they never enter formal test metrics.
- P01 QA: 27 audited KEEP items; all 9 table questions are uniquely aligned to
  exact row units. Privacy pre-screening is complete and human privacy release
  is pending, so current BGE-M3 runs remain validation-only. These 27 items keep
  their existing human content decisions and are excluded from the LLM content
  pre-review.
- P02-P10 boundaries: all 2,700 blinded candidates carry LLM pre-labels
  (455 `BREAK`, 1,820 `NO_BREAK`, 425 `NOISE`; 234 flagged for human review).
  The mechanical rule-conformance audit reports two structural conflicts, both
  caused by corrupted heading paths and both already flagged for review, and no
  unflagged conflict. The blind human verification workbook of 1,050 rows
  (900 agreement-sample rows and 150 flagged rows) is fully judged; eight projects
  are promoted and P08 is withheld, as recorded in section 4ter.
- P02-P10 QA drafts: mechanical checks resolve every declared evidence unit and
  confirm span containment. The advisory content pre-review approves 71 of 262
  drafts, marks 124 as requiring a rewrite, and rejects 67. No project reaches
  the 15-question minimum on approved drafts alone, because a project's documents
  reuse whole passages verbatim and a single gold evidence unit is therefore
  indefensible.
- P02-P10 QA candidate set: the benchmark moved to the multi-evidence model of
  `docs/annotation_and_privacy_protocol.md` section 6ter. Equivalence resolution
  over the 262 drafts reports 180 unique spans, 72 equivalence sets (41 spanning
  more than one document), and 10 spans above the occurrence ceiling that were
  not expanded. Re-adjudication yields 69 single-round approvals, 12 multi-evidence
  releases, and 114 verified rewrites; 67 drafts stay excluded, two of them because
  the occurrence ceiling overrides a content approval that had judged a boilerplate
  interface row unique inside its own table. Thirty supplementary drafts close the
  remaining per-project shortfalls, giving 225 candidates with a per-project minimum
  of 18, of which 45 carry an equivalence set and 3 are flagged `needs_role_check`
  for unreliable table headers. All 225 have since been judged and imported by
  `scripts/import_qa_approval_workbook.py`, the only path from a reviewer decision
  to `qa_approved.json`; section 4quater records the outcome.
- P02-P10 formal QA files: 195 questions written across nine projects (P02 31,
  P03 28, P04 22, P05 17, P06 22, P07 23, P08 22, P09 19, P10 11). Eight projects
  meet the 15-question minimum; P10 is withheld from retrieval reporting, leaving
  184 questions in the reported set.
- P04-P10: locked corpora and blind annotation packs are ready. No retrieval
  metrics have been inspected under the frozen protocol.
- P04/P05 legacy Word sources and the malformed P09 database design were
  converted into SHA-256-tracked, non-destructive DOCX copies. The resulting
  corpus contains 89 documents, 31,841 blocks, 66,981 retrieval units, and
  approximately 1.68 million tokens.
- The read-only workbook audit on 2026-08-04 found 0 complete, 5,662 pending,
  27 partially complete, and 0 invalid review rows. Sections 4ter, 4quater and
  4quinquies supersede that snapshot: every human review round is now complete and
  `audit_formal_readiness.py --require-ready` reports all ten projects ready.
- The whitelist-only anonymous-review scaffold is at `v2`, which carries annotation
  protocol `v1.4`. It ships 98 files and passes 286 package-local unit tests with
  12 skipped, a deterministic synthetic CPU smoke run, archive integrity checks,
  manifest hashing, and a zero-finding private-identifier scan. The skipped tests
  are those requiring real annotation workbooks, which the package excludes by
  design. It explicitly declares that human review and locked-test results are
  absent. Scaffold `v1` carried protocol `v1.0` and a test suite whose targets were
  not packaged; it was removed rather than kept alongside `v2`.

### 4quinquies. Current P01 privacy status

P01's 27 questions carry an individual privacy verdict: 23 `NO_RISK`, 1 `CLEARED`,
and 3 `REJECTED` (QA020, QA029, QA035), leaving 24 questions against a minimum of
15. The three rejections state a customer or affiliate identity in both the answer
and the evidence span; each also rejects the content, which the import gate
requires. None was anonymized in place, because editing an evidence span would
break the span-containment check in section 8.

One rejection turned on an affiliate name that the review view's derived
identifier did not cover, so it was found by reading rather than by highlighting.
The highlighting is an aid and not a completeness guarantee; annotation protocol
section 7bis records this.

Existing P01-P03 BGE-M3 tables are exploratory because they predate these
strict annotation gates. They may motivate the method but must be rerun after
the benchmark is finalized.
