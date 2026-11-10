# SEAC for Enterprise Software-Engineering Documents

This workspace contains the first reproducible experiment pipeline for comparing
text chunking methods on software-engineering documents.

Implemented methods:

- `fixed`: fixed token windows
- `recursive`: recursive character/separator splitting
- `structure`: heading/table-aware structural splitting
- `semantic`: adjacent-unit semantic-distance splitting with a static percentile threshold
- `proposed`: initial structure/domain-density/continuity/adaptive-threshold implementation
- `proposed_v6`: full hierarchical SEAC with parent-child retrieval
- `langchain_recursive` and `llamaindex_sentence`: official external baselines
- `seac_no_*`: seven named SEAC ablations used by the paper protocol

The paper scope is frozen to enterprise software-engineering documents. Finance
RAG and Personal-FinKB are not part of this workspace's publication target.

The tested paper environment is pinned in `requirements-paper.txt`.

The human-review stage is operationalized in internal workbooks that contain
enterprise evidence and must not be publicly released. The normative rules are in
`docs/annotation_and_privacy_protocol.md`, and
`docs/human_review_handoff.md` lists the three outstanding human tasks, who can
perform each one, and the commands that turn a filled workbook into a formal
artifact. The legacy A/B boundary workbooks under `outputs/annotation_workbooks_v1/`
apply only to P01; P02–P10 boundaries use the pre-label verification pack below.

During annotation, run `python3 scripts/audit_review_progress.py` for a read-only
completion and error report; it does not write reviewed labels or QA records.

P02–P10 boundaries additionally use the disclosed LLM pre-label path of
`docs/annotation_and_privacy_protocol.md` section 4bis. Pre-labels never become
the gold standard on their own: promotion requires the blind human verification
workbook and a per-project human-model kappa of at least 0.70.

```bash
python3 scripts/prepare_llm_prelabel_batches.py
python3 scripts/merge_llm_prelabels.py --check-only
python3 scripts/merge_llm_prelabels.py
python3 scripts/audit_prelabel_consistency.py
python3 scripts/build_prelabel_verification_sample.py
# a human fills the verification workbook, either in Excel or through the
# localhost annotation view, which writes the same three columns back to it
python3 scripts/serve_boundary_verification.py
python3 scripts/promote_llm_prelabels.py
python3 scripts/promote_llm_prelabels.py --write
```

The advisory QA content pre-review is separate and grants no approval; privacy
status stays `PENDING` for every record and must be set by an authorized human
reviewer:

```bash
python3 scripts/prepare_qa_prereview_batches.py --projects P02 P03 P04 P05 P06 P07 P08 P09 P10
python3 scripts/merge_qa_prereview.py
```

Because a project's documents reuse whole passages verbatim, the benchmark scores
a hit on any member of a question's gold evidence set. Resolve the equivalence
sets, re-adjudicate the drafts under that model, verify supplementary drafts, and
build the human approval workbook:

```bash
python3 scripts/resolve_qa_evidence_equivalents.py --write
python3 scripts/prepare_qa_readjudication_batches.py
python3 scripts/merge_qa_readjudication.py
python3 scripts/verify_supplementary_qa_drafts.py
python3 scripts/build_qa_approval_workbook.py
```

Rewritten and supplementary drafts are accepted only if the corpus still supports
them, and every candidate stays `PENDING` on approval and privacy until
`outputs/qa_approval_v1/SEAC_QA_approval.xlsx` is filled by a human reviewer. The
localhost review view resolves every corpus location of a candidate's evidence and
highlights the span inside each one, which is what makes a multi-evidence candidate
judgeable; it enforces the same rules as the importer and writes the same four
columns back to the workbook:

```bash
python3 scripts/serve_qa_approval.py
```

Once the workbook is filled, this is the only path into the formal QA files; it
refuses to write while any row is undecided, re-derives evidence from the corpus
rather than trusting the workbook, and applies the mechanical §6.1bis rule that
excludes a question beginning with a section number even where the reviewer
approved it. The exclusion is reported rather than written back, so the workbook
keeps the reviewer's decision:

```bash
python3 scripts/import_qa_approval_workbook.py
python3 scripts/import_qa_approval_workbook.py --write
python3 scripts/audit_formal_readiness.py
```

P01's 27 questions passed content review long ago and wait only on privacy release.
The pre-screen flagged all of them by scanning the question, answer, span, file name
and section path together, so the flag never says which field carries the customer
name — 25 rows carry it only in the source file name, two carry it in the answer and
the span. The localhost view derives the identifier from the corpus file names, marks
every occurrence in place, and separates those two cases; it also carries a privacy
rejection through to `approval_status`, which the importer requires, and warns that
anonymizing a span in place would fail the span-containment check downstream:

```bash
python3 scripts/serve_privacy_review.py
```

Both boundary and QA rounds are complete: 2,400 boundaries and 195 questions are promoted. A
project may be withheld from one axis with a stated reason in the run manifest —
P08 from boundary metrics, P10 from retrieval metrics — and the suite passes `none`
for a withheld axis so the axis is reported as absent instead of as a zero score.
The separate P01 exercise and controlled answer key are under
`outputs/calibration_workbooks_v1/`; both contain private enterprise-derived
text and are excluded from the anonymous artifact.

Repair legacy/malformed Word sources into traceable workspace copies, then
build or refresh project corpora from the versioned project manifest:

```bash
python3 scripts/convert_legacy_sources.py --projects P04 P05 P09
python3 scripts/build_industrial_projects.py --projects P04 P05 P06 P07 P08 P09 P10
python3 scripts/prepare_annotation_pack.py --projects P02 P03 P04 P05 P06 P07 P08 P09 P10
python3 scripts/build_corpus_inventory.py
```

Audit the paper gates before any formal run:

```bash
python3 scripts/audit_formal_readiness.py --require-ready
python3 scripts/run_formal_suite.py --dry-run
```

After every project passes, run the frozen 15-method BGE-M3 matrix on a CUDA
machine:

```bash
python3 scripts/run_formal_suite.py \
  --device cuda \
  --model-batch-size 16 \
  --output-root experiments/formal_v1
```

Formal runs reject unreviewed QA and provisional breakpoint labels. The P01 CPU
BGE-M3 output under `experiments/P01_strict_unit_bge_m3_v1` is validation-only
and must not be copied into the paper.

The current proposed method adds two domain-specific behaviors:

- Metadata-aware indexing: retained document type and heading path are prepended
  to the retrieval representation, while the original chunk text remains
  unchanged.
- Balanced adaptive boundaries: headings are mandatory structural boundaries;
  semantic scores, information density, continuity penalties, and adaptive
  target sizes control the remaining boundaries under a hard 512-token limit.
- Heading anchoring and minimum viable chunks: a heading remains a boundary
  marker but is attached to its first content unit, preventing heading-only
  retrieval fragments.
- Structure-aware long-unit splitting: oversized meeting notes, table rows, and
  constraint blocks are split first on sentence and list boundaries before a
  token fallback is used.

The frozen paper protocol, data split, annotation gates, metrics, and remaining
work are recorded in `docs/industrial_seac_protocol.md`.
