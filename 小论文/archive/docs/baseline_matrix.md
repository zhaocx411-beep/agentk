# AutoSEAC-Fin Baseline Matrix

This matrix defines the baseline set needed for a credible financial RAG
chunking paper. It separates internal engineering baselines, official framework
baselines, finance-specific table baselines, parent-child baselines, and
long-context baselines.

## Dependency Setup

Use the Python 3.12 runtime for official framework baselines. The default
system `/usr/bin/python3` is Python 3.9 on this machine and is not compatible
with the installed LlamaIndex version.

Runtime used for validated baseline runs:

```bash
/Users/zhaochenxu06/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3
```

LangChain official splitters:

```bash
/Users/zhaochenxu06/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3 -m pip install langchain-text-splitters
```

LlamaIndex official node parsers:

```bash
/Users/zhaochenxu06/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3 -m pip install llama-index-core
```

Verify:

```bash
/Users/zhaochenxu06/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3 - <<'PY'
from langchain_text_splitters import RecursiveCharacterTextSplitter, CharacterTextSplitter
from llama_index.core.node_parser import SentenceSplitter, TokenTextSplitter
print("official splitter dependencies OK")
PY
```

If a dependency is missing, `scripts/run_finance_chunking_experiment.py` records
the method as `unavailable` instead of silently substituting a local
implementation.

## Baseline Families

| Method | Family | Implementation | Purpose |
|---|---|---|---|
| `fixed` | Simple | local fixed token window | Minimal fixed-size reference. |
| `recursive` | Simple | local recursive splitter | Minimal recursive reference. |
| `semantic` | Simple semantic | local adjacent semantic-distance splitter | Internal semantic reference. |
| `structure` | Structure-only | local heading/table protection | Tests rule-only structure boundaries. |
| `langchain_recursive` | Official framework | LangChain `RecursiveCharacterTextSplitter` | Widely used production RAG baseline. |
| `langchain_character` | Official framework | LangChain `CharacterTextSplitter` | Simple official character splitter. |
| `llamaindex_sentence` | Official framework | LlamaIndex `SentenceSplitter` | Widely used LlamaIndex node parser. |
| `llamaindex_token` | Official framework | LlamaIndex `TokenTextSplitter` | Official token-level node parser. |
| `table_whole` | Finance table-aware | one whole-table chunk | Tests preserving complete table context. |
| `table_row_context` | Finance table-aware | row chunk with table/unit/header context | Strong table-row retrieval baseline. |
| `table_cell_context` | Finance table-aware | cell chunk with row/column/table context | Fine-grained numeric evidence baseline. |
| `fixed_parent_child` | Parent-child | fixed parents + row/sentence children | Tests parent-child retrieval without AutoSEAC-Fin boundary scoring. |
| `full_document` | Long-context | one chunk per document/context | Long-context/no-chunking retrieval baseline. |
| `proposed_v6` | Prior method | existing SEAC parent-child method | Non-financial predecessor. |
| `autoseac_fin` | Proposed | financial metadata-aware self-evolving chunker | Main method. |

## Experimental Roles

Use the official framework baselines to answer:

- Does AutoSEAC-Fin beat commonly used LangChain and LlamaIndex splitters?
- Are gains due to finance-aware modeling rather than only implementation
  details?

Use table-aware baselines to answer:

- Is AutoSEAC-Fin better than a strong finance-specific table heuristic?
- Are row/cell evidence gains still present when baselines preserve table
  structure?

Use parent-child and long-context baselines to answer:

- Are gains due only to parent-child retrieval?
- Is chunking still useful when the system can retrieve or provide larger
  contexts?

## Current TAT-QA Caution

TAT-QA contexts are short pseudo-documents containing a few paragraphs and one
table. Large fixed or full-document baselines can be unusually strong here.
Final claims should be validated on longer and noisier settings such as
DocFinQA, FinanceBench, and iFinD pilot data.

## Current Smoke/Validation Status

Official framework baselines were run on TAT-QA validation with `row_all`
evidence mode and `max_tokens=768`:

```bash
/Users/zhaochenxu06/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3 scripts/run_finance_chunking_experiment.py \
  --corpus workbench/tatqa/corpus.json \
  --qa workbench/tatqa/qa_validation.json \
  --unit-mode standard \
  --methods fixed recursive langchain_recursive langchain_character llamaindex_sentence llamaindex_token table_whole table_row_context table_cell_context full_document fixed_parent_child proposed_v6 autoseac_fin \
  --evidence-mode row_all \
  --max-tokens 768 \
  --semantic-percentile 0.95 \
  --proposed-soft-target 768 \
  --proposed-dense-target 768 \
  --hierarchy-parent-weight 0.0 \
  --no-fin-cell-context \
  --fin-row-context \
  --finance-query-boost \
  --finance-query-boost-scope autoseac_fin \
  --finance-year-boost 0.06 \
  --finance-metric-boost 0.06 \
  --finance-value-boost 0.02 \
  --finance-max-boost 0.12 \
  --output-dir experiments/tatqa_validation_official_baselines_row_all
```

Summary:

| Method | MRR | Hit@1 | Hit@3 | Hit@5 |
|---|---:|---:|---:|---:|
| fixed_parent_child | 0.3989 | 0.3133 | 0.4246 | 0.4945 |
| AutoSEAC-Fin v0004 | 0.3599 | 0.2749 | 0.3881 | 0.4483 |
| full_document | 0.3572 | 0.2689 | 0.3936 | 0.4477 |
| fixed | 0.3320 | 0.2470 | 0.3692 | 0.4191 |
| LangChain Character | 0.3278 | 0.2421 | 0.3644 | 0.4136 |
| LangChain Recursive | 0.3278 | 0.2421 | 0.3644 | 0.4136 |
| recursive | 0.3274 | 0.2421 | 0.3656 | 0.4124 |
| LlamaIndex Token | 0.3255 | 0.2409 | 0.3650 | 0.4069 |
| LlamaIndex Sentence | 0.3033 | 0.2275 | 0.3364 | 0.3771 |
| proposed_v6 | 0.2769 | 0.2062 | 0.2974 | 0.3522 |
| table_row_context | 0.2238 | 0.1563 | 0.2457 | 0.2889 |
| table_cell_context | 0.1868 | 0.1387 | 0.2038 | 0.2299 |
| table_whole | 0.1804 | 0.1411 | 0.1934 | 0.2184 |

Interpretation:

- AutoSEAC-Fin v0004 beats fixed, recursive, LangChain, full-document by a small
  margin on Hit@5, and beats `proposed_v6` substantially.
- `fixed_parent_child` is currently the strongest TAT-QA row-all validation
  baseline. This is a stronger and more credible comparison target than
  fixed-size alone.
- Because TAT-QA is short-context and table-centric, this result should motivate
  cross-dataset validation rather than more TAT-QA-only parameter tuning.
