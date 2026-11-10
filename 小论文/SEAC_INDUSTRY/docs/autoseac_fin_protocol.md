# AutoSEAC-Fin Experimental Protocol

Version: v0.1

This document defines the experimental protocol for AutoSEAC-Fin, a task-driven
self-evolving chunking framework for evidence-grounded financial RAG.

The purpose of this protocol is to prevent test leakage, keep the automated
search process auditable, and make the final paper reproducible. It should be
treated as the governing document for later implementations of:

- `skills/autoseac_fin/SKILL.md`
- `configs/autoseac_fin.yaml`
- `scripts/run_finance_chunking_experiment.py`
- `scripts/run_autoseac_fin.py`
- `experiments/autoseac_fin_versions/`

## 1. Research Scope

AutoSEAC-Fin studies financial document chunking as a downstream-task
optimization problem. The primary goal is not to maximize agreement with human
chunk boundaries alone. The primary goal is to improve evidence-grounded
financial RAG under retrieval, numerical, citation, and cost constraints.

The method should optimize chunking strategies for:

- evidence retrieval;
- answer-supporting context completeness;
- numerical evidence coverage;
- citation correctness;
- table/header/unit preservation;
- company, period, and metric disambiguation;
- retrieval cost control.

Human boundaries, section rules, and financial document structures are used as
regularization and diagnostics. They are not the sole optimization target.

## 2. Dataset Plan

The experiment uses a two-layer data design:

1. public financial benchmarks for reproducibility and international
   comparability;
2. iFinD-based private financial corpus for real-world validation.

### 2.1 Core Public Benchmarks

The main public benchmarks are:

| Dataset | Primary Role | Usage |
| --- | --- | --- |
| DocFinQA | Long-document financial QA and retrieval | Main train/validation/test benchmark |
| TAT-QA | Text-table QA, units, scale, numerical reasoning | Main structure and numerical benchmark |
| FinanceBench | Open-book financial QA with evidence strings | External held-out public test |

DocFinQA is the preferred first long-document benchmark because it stresses
chunking under full-report noise. TAT-QA is the preferred first table-aware
benchmark because it directly tests unit, scale, row, column, and surrounding
text preservation. FinanceBench is reserved as a high-credibility external
benchmark and should not be used for iterative parameter search unless a clear
official train/validation split is available and documented.

### 2.2 Diagnostic Public Benchmarks

The diagnostic benchmarks are:

| Dataset | Primary Role | Usage |
| --- | --- | --- |
| FinQA | Numerical reasoning programs and supporting evidence | Numerical reasoning diagnostics |
| MultiHiertt | Multi-table and hierarchical table reasoning | Table hierarchy robustness and ablation |

These datasets are used to diagnose specific design choices such as numerical
context preservation, formula input coverage, table-row child chunks, and
hierarchical table serialization. They should not distract from the main
DocFinQA/TAT-QA/FinanceBench narrative.

### 2.3 Private Real-World Benchmark

iFinD data is used for real-world financial validation. It should be reported
as a private corpus unless the data provider explicitly permits public release.

The iFinD corpus should prioritize:

- annual reports;
- semi-annual reports;
- quarterly reports;
- company announcements;
- industry research reports;
- stock research reports;
- financial indicator tables;
- company profiles;
- major event announcements.

Before any public release, confirm and document:

- data license;
- redistribution restrictions;
- whether raw documents can be shared;
- whether derived metadata can be shared;
- whether anonymized examples can be shared;
- whether statistics and annotation protocol can be shared.

## 3. Canonical Data Schema

All datasets must be converted into a unified corpus and QA schema before
experiments. Dataset-specific scripts may preserve extra fields, but the common
fields below should be available for the core pipeline.

### 3.1 Corpus Schema

Each corpus file should be stored as:

```text
workbench/<dataset>/corpus.json
```

Each document should follow this shape:

```json
{
  "dataset": "docfinqa",
  "document_id": "",
  "file_name": "",
  "document_type": "annual_report/10-k/10-q/earnings_release/announcement/research_report",
  "company": "",
  "ticker": "",
  "industry": "",
  "report_date": "",
  "fiscal_year": "",
  "fiscal_quarter": "",
  "source_uri": "",
  "license": "",
  "blocks": []
}
```

Each block should follow this shape where available:

```json
{
  "block_id": "",
  "block_index": 1,
  "block_type": "heading/paragraph/table/table_row/table_cell/footnote/caption/formula/metadata",
  "heading_path": "",
  "text": "",
  "rows": [],
  "table_id": "",
  "table_title": "",
  "row_header": "",
  "column_header": "",
  "metric": "",
  "unit": "",
  "currency": "",
  "period": "",
  "source_page": "",
  "source_location": ""
}
```

Mandatory first-pass fields:

- `document_id`
- `file_name`
- `document_type`
- `blocks`
- `block_id`
- `block_type`
- `text` or `rows`

Mandatory financial fields for formal experiments:

- `company` or `ticker`
- `period` or `fiscal_year`
- `table_title` for table-derived evidence where available
- `row_header` and `column_header` for table cell/row evidence where available
- `unit` or `currency` for numeric table evidence where available

### 3.2 QA Schema

Each QA file should be stored as:

```text
workbench/<dataset>/qa_train.json
workbench/<dataset>/qa_validation.json
workbench/<dataset>/qa_test.json
```

Each QA item should follow this shape:

```json
{
  "qa_id": "",
  "dataset": "",
  "question": "",
  "reference_answer": "",
  "answer_type": "numeric/fact/list/comparison/calculation/free_text",
  "question_type": "table/temporal/comparative/calculation/risk/definition/event/forecast",
  "company": "",
  "ticker": "",
  "industry": "",
  "period": "",
  "metric": "",
  "unit": "",
  "source_document": "",
  "gold_evidence_id": "",
  "gold_evidence_span": "",
  "supporting_evidence_ids": [],
  "gold_table_id": "",
  "gold_row_header": "",
  "gold_column_header": "",
  "calculation_formula": "",
  "calculation_inputs": [],
  "audit_status": "verified",
  "audit_notes": ""
}
```

For multi-evidence and calculation questions, `supporting_evidence_ids` should
be filled whenever the source dataset provides enough evidence annotation.

## 4. Dataset Splits

The project uses four experimental split roles:

| Split Role | Purpose | May Influence AutoResearch? |
| --- | --- | --- |
| Search/Train | Fit encoders, search chunking parameters, run exploratory hypotheses | Yes |
| Validation | Decide keep/reject for AutoSEAC-Fin versions | Yes, only through frozen metrics |
| Public Test | Final benchmark reporting | No |
| External Test | One-shot final generalization check | No |

### 4.1 Public Benchmark Split Policy

DocFinQA:

- Use official splits if available.
- Search/Train may use the official train split.
- Validation may use the official dev/validation split.
- Test must be held out until the final selected version is frozen.

TAT-QA:

- Use official train/dev/test splits if available.
- If a hidden official test set is not accessible, use the public dev set as
  validation and create a reproducible held-out split from train by document.
- Splits should be document-level, not QA-level, whenever possible.

FinanceBench:

- Prefer using FinanceBench as an external held-out public test.
- Do not use FinanceBench for iterative AutoResearch parameter search unless an
  official train/validation split is available and this protocol is updated.
- If only a limited public subset is available, report it as external
  evaluation, not as a training benchmark.

FinQA and MultiHiertt:

- Use as diagnostic validation/test sets for numerical and table-specific
  analysis.
- Do not let diagnostic improvements override the main validation decision
  unless this is declared before the AutoResearch run starts.

### 4.2 iFinD Split Policy

iFinD must be split by company, year, and document type when possible.

Recommended split:

| Split | Example |
| --- | --- |
| Search/Train | Earlier years and a subset of companies |
| Validation | Different companies or later years |
| Test | Held-out companies, held-out years, and preferably held-out industries |

The iFinD test set must remain frozen. It must not be used for:

- choosing chunking parameters;
- debugging individual examples;
- editing QA items;
- editing gold evidence;
- changing evaluator logic;
- deciding keep/reject in AutoResearch.

If a methodological change is inspired by iFinD test failures, a new test split
must be created and the old test split should be reported as contaminated.

## 5. Allowed Inputs During Optimization

During AutoResearch/AutoSEAC-Fin search, the algorithm may use:

- Search/Train corpus and QA files;
- Validation corpus and QA files for frozen metric computation;
- dataset metadata fields included in the canonical schema;
- training split gold evidence for metric computation;
- validation split gold evidence for keep/reject decisions;
- fixed query parsing rules or train-derived dictionaries;
- frozen embedding model selected before the run;
- fixed evaluation code selected before the run.

The algorithm may not use:

- Public Test QA labels during search;
- External Test QA labels during search;
- iFinD test labels during search;
- manual inspection of test failures for parameter changes;
- modified gold answers or evidence spans;
- evaluator changes motivated by improving a specific version score;
- hidden information unavailable in a real retrieval setting.

## 6. Chunking Methods To Compare

The minimum method set for main experiments is:

- fixed-size chunking;
- recursive character chunking;
- structure-only chunking;
- semantic chunking;
- existing `proposed_v6`;
- AutoSEAC-Fin.

Recommended additional baselines:

- LangChain RecursiveCharacterTextSplitter;
- LlamaIndex SentenceSplitter;
- LlamaIndex SemanticSplitter;
- parent-child retrieval baseline with non-financial chunking;
- long-context baseline where feasible;
- table-aware baseline where feasible.

All methods should be evaluated under the same encoder, top-k setting, metadata
availability, and retrieval reranking setting unless explicitly reported as a
separate condition.

## 7. AutoSEAC-Fin Method Contract

AutoSEAC-Fin should be implemented as a financial extension of the current
parent-child design rather than a separate unrelated pipeline.

The expected implementation path is:

```text
seac_chunking/finance_units.py
seac_chunking/finance_chunkers.py
seac_chunking/finance_query.py
seac_chunking/finance_evaluation.py
scripts/run_finance_chunking_experiment.py
scripts/run_autoseac_fin.py
```

### 7.1 Parent Chunk Contract

Parent chunks preserve coherent answer context. They are the units returned for
final answer generation and citation.

A parent chunk should:

- avoid crossing company boundaries;
- avoid crossing report-period boundaries when possible;
- avoid crossing major section boundaries unless needed for context;
- preserve a table's title, unit, header, and related rows when possible;
- retain provenance fields needed for citation;
- stay within `parent_max_tokens`.

### 7.2 Child Chunk Contract

Child chunks provide precise retrieval evidence within a parent.

Child chunk types include:

- paragraph child;
- table row child;
- table cell with row/column context child;
- formula child;
- risk bullet child;
- event sentence child;
- footnote child.

Every child chunk should carry:

- `parent_chunk_id`;
- `company`;
- `ticker`;
- `period`;
- `metric`;
- `unit`;
- `table_title`;
- `row_header`;
- `column_header`;
- source document and source location.

### 7.3 Financial Serialization Contract

Table-derived evidence must not be serialized as raw row text only. A row or
cell child should include explicit financial context:

```text
Company: <company>
Ticker: <ticker>
Period: <period>
Document: <document>
Table: <table_title>
Unit: <unit>
Row: <row_header>
Column: <column_header>
Value: <value>
```

The rendered text may be adjusted for the final corpus language, but the same
information must be present.

## 8. Boundary Score Specification

The first AutoSEAC-Fin boundary score should be:

```text
S(i) =
  w_sem * semantic_distance(u_i, u_{i+1})
+ w_struct * structural_transition(u_i, u_{i+1})
+ w_num * numeric_context_shift(u_i, u_{i+1})
+ w_time * temporal_context_shift(u_i, u_{i+1})
+ w_table * table_boundary_signal(u_i, u_{i+1})
- w_cont * continuity_signal(u_i, u_{i+1})
```

Initial feature definitions:

- `semantic_distance`: adjacent-unit embedding distance.
- `structural_transition`: heading, section, table, or document-structure
  transition.
- `numeric_context_shift`: change in numeric density, metric, unit, currency,
  or formula context.
- `temporal_context_shift`: change in fiscal year, quarter, reporting period,
  or date.
- `table_boundary_signal`: table start/end, table id change, table title change,
  header transition, row/column group change.
- `continuity_signal`: same company, same period, same table, same metric, same
  formula, or same section continuity.

Hard constraints should override the soft score when required:

- never split a table cell from its row/column/unit context in the rendered
  child representation;
- avoid parent chunks crossing different companies;
- avoid parent chunks crossing different fiscal periods unless the source
  document explicitly compares them;
- preserve table title and unit provenance for numeric evidence.

## 9. Retrieval Scoring Specification

The base hierarchical retrieval score is:

```text
score(parent) =
  alpha * sim(query, parent)
+ (1 - alpha) * max_child_sim(query, child in parent)
```

The financial metadata-aware score is:

```text
score(parent) =
  alpha * sim(query, parent)
+ (1 - alpha) * max_child_sim(query, child in parent)
+ beta_company * company_match
+ beta_period * period_match
+ beta_metric * metric_match
- wrong_company_penalty
- wrong_period_penalty
```

The paper should report retrieval in at least two settings:

1. chunk-only retrieval: no financial metadata reranking, used to isolate
   chunking quality;
2. metadata-aware retrieval: the same query parser and metadata reranker are
   applied fairly to all methods where possible.

AutoSEAC-Fin should not receive metadata advantages that baselines are denied
unless this is explicitly labeled as a method-specific condition.

## 10. Evaluation Metrics

### 10.1 Primary Retrieval Metrics

Primary retrieval metrics:

- EvidenceRecall@1;
- EvidenceRecall@3;
- EvidenceRecall@5;
- MRR;
- MultiEvidenceRecall;
- CitationCorrectness;
- NumericEvidenceCoverage;
- CostPenalty.

Definitions:

- `EvidenceRecall@K`: whether any top-k returned parent chunk contains the gold
  evidence id or span.
- `MRR`: reciprocal rank of the first chunk containing gold evidence.
- `MultiEvidenceRecall`: fraction of supporting evidence ids recovered in top-k.
- `CitationCorrectness`: whether the returned chunk can support the gold
  citation span.
- `NumericEvidenceCoverage`: whether all required numeric inputs for a
  calculation are retrieved.
- `CostPenalty`: normalized token count or chunk count of retrieved context.

### 10.2 Secondary Answer Metrics

Answer generation should be added after retrieval evaluation is stable.

Secondary answer metrics:

- AnswerAccuracy;
- NumericAccuracy;
- UnitMatch;
- PeriodMatch;
- FormulaInputMatch;
- EvidenceSupportedness.

Numerical questions should be evaluated by exact or tolerance-based rules where
possible. LLM judges should be used only for cases that cannot be reliably
evaluated with deterministic rules.

### 10.3 Boundary Diagnostics

Boundary metrics are diagnostic:

- boundary precision;
- boundary recall;
- boundary F1;
- table-boundary preservation rate;
- header-unit-value preservation rate;
- company-crossing violation count;
- period-crossing violation count.

Boundary metrics should not dominate the final objective.

## 11. Objective Function

The initial retrieval-only objective is:

```text
Objective =
  0.40 * EvidenceRecall@5
+ 0.25 * MRR
+ 0.15 * MultiEvidenceRecall
+ 0.10 * NumericEvidenceCoverage
+ 0.10 * CitationCorrectness
- 0.05 * CostPenalty
```

After answer generation is stable, the objective may be extended to:

```text
Objective =
  0.30 * EvidenceRecall@5
+ 0.20 * MRR
+ 0.20 * AnswerAccuracy
+ 0.15 * NumericAccuracy
+ 0.10 * CitationCorrectness
- 0.05 * CostPenalty
```

The objective weights must be declared before each AutoResearch run starts. They
must not be changed after seeing validation results unless a new run is started
and documented.

## 12. AutoResearch Search Protocol

AutoResearch is used as a controlled parameter and strategy search protocol. It
is not allowed to freely rewrite the experimental task.

### 12.1 Editable Items

AutoResearch may change:

- values in `configs/autoseac_fin.yaml`;
- one declared hypothesis per version;
- chunker parameters within the allowed range;
- feature weights within the allowed range;
- child chunk target sizes within the allowed range;
- retrieval fusion weights within the allowed range;
- metadata boost and penalty values within the allowed range;
- ablation flags declared in the config.

AutoResearch may not change:

- gold QA files;
- gold evidence files;
- public test files;
- external test files;
- iFinD test files;
- evaluation code during a run;
- dataset conversion outputs during a run;
- the objective function during a run;
- hidden test access policy;
- baseline implementations to make them weaker.

### 12.2 One-Hypothesis Rule

Each version should change either:

- one major hypothesis; or
- one coherent group of same-family parameters.

Examples of valid changes:

- increase `table_weight` and decrease `breakpoint_percentile` to preserve
  financial table context;
- tune only child chunk target/max sizes;
- tune only company/period/metric reranking boosts;
- enable or disable only footnote context injection.

Examples of invalid changes:

- changing table serialization, retrieval reranking, and objective weights in
  the same version;
- editing validation data after a poor result;
- removing hard examples;
- changing evaluator logic after seeing score drops.

### 12.3 Version Directory

Every AutoResearch version must write:

```text
experiments/autoseac_fin_versions/v0001/
  config.yaml
  hypothesis.md
  results_train.json
  results_validation.json
  retrieval_details.json
  error_analysis.md
  decision.json
```

The `decision.json` file must follow:

```json
{
  "version": "v0001",
  "hypothesis": "",
  "changed_parameters": {},
  "validation_score": 0.0,
  "previous_best_score": 0.0,
  "decision": "keep/reject",
  "reason": ""
}
```

### 12.4 Keep/Reject Rule

A version may be kept if:

- validation objective improves over previous best; and
- no primary constraint violation materially increases; and
- cost does not increase beyond the declared tolerance; and
- the improvement is not isolated to one accidental subset unless documented.

A version should be rejected if:

- validation objective decreases;
- improvement is caused by a bug or leakage;
- company/period/table preservation violations increase materially;
- cost increases without retrieval or answer benefit;
- the change weakens generalization across datasets.

## 13. Initial Configuration Search Space

The first configuration file should include:

```yaml
semantic_weight: 0.35
structure_weight: 0.20
numeric_weight: 0.15
temporal_weight: 0.10
table_weight: 0.20
continuity_penalty: 0.40

breakpoint_percentile: 0.80
parent_max_tokens: 512
parent_soft_target_tokens: 320

child_target_tokens: 96
child_max_tokens: 160

hierarchy_parent_weight: 0.35
company_match_boost: 0.10
period_match_boost: 0.10
metric_match_boost: 0.08
wrong_company_penalty: 0.20
wrong_period_penalty: 0.20

table_row_child_enabled: true
cell_context_enabled: true
footnote_context_enabled: true
```

Recommended parameter bounds:

| Parameter | Min | Max |
| --- | ---: | ---: |
| semantic_weight | 0.10 | 0.60 |
| structure_weight | 0.05 | 0.50 |
| numeric_weight | 0.00 | 0.40 |
| temporal_weight | 0.00 | 0.30 |
| table_weight | 0.00 | 0.50 |
| continuity_penalty | 0.00 | 1.00 |
| breakpoint_percentile | 0.60 | 0.95 |
| parent_max_tokens | 384 | 768 |
| parent_soft_target_tokens | 192 | 512 |
| child_target_tokens | 64 | 160 |
| child_max_tokens | 96 | 256 |
| hierarchy_parent_weight | 0.00 | 0.70 |
| company_match_boost | 0.00 | 0.30 |
| period_match_boost | 0.00 | 0.30 |
| metric_match_boost | 0.00 | 0.30 |
| wrong_company_penalty | 0.00 | 0.50 |
| wrong_period_penalty | 0.00 | 0.50 |

## 14. Reporting Requirements

The final paper should report:

- dataset statistics;
- split strategy;
- public benchmark license/access notes;
- iFinD privacy and license notes;
- all baseline settings;
- selected AutoSEAC-Fin configuration;
- validation trajectory;
- final test results;
- ablation results;
- robustness by document type and question type;
- error analysis;
- cost analysis;
- limitations.

Main table:

```text
Method | EvidenceRecall@5 | MRR | MultiEvidenceRecall | NumericEvidenceCoverage | CitationCorrectness | Cost
```

Recommended dataset rows:

- DocFinQA;
- TAT-QA;
- FinanceBench external test;
- iFinD private test;
- combined macro average.

Ablation table:

- w/o semantic score;
- w/o structure score;
- w/o numeric context;
- w/o temporal context;
- w/o table preservation;
- w/o company/period/metric boost;
- w/o parent-child retrieval;
- w/o citation constraint;
- w/o AutoResearch search.

## 15. Implementation Milestones

Milestone 1: TAT-QA retrieval-only pilot.

- Convert TAT-QA into canonical schema.
- Implement `finance_units.py`.
- Implement table row/cell serialization.
- Implement AutoSEAC-Fin chunker.
- Run fixed/recursive/structure/semantic/proposed_v6/autoseac_fin.
- Report EvidenceRecall@K and MRR.

Milestone 2: DocFinQA long-document pilot.

- Convert DocFinQA into canonical schema.
- Add long-document retrieval metrics and cost metrics.
- Compare against long-context baseline where feasible.

Milestone 3: AutoResearch search loop.

- Write `skills/autoseac_fin/SKILL.md`.
- Write `configs/autoseac_fin.yaml`.
- Write `scripts/run_autoseac_fin.py`.
- Run controlled train/validation search.
- Save version trajectory.

Milestone 4: External public test.

- Freeze final AutoSEAC-Fin configuration.
- Run FinanceBench once as external held-out public test.
- Do not tune on FinanceBench result.

Milestone 5: Diagnostics.

- Run FinQA numerical diagnostics.
- Run MultiHiertt table hierarchy diagnostics.
- Run ablations.

Milestone 6: iFinD validation.

- Confirm license.
- Build iFinD pilot.
- Freeze iFinD test.
- Run final selected version.
- Report private corpus statistics and anonymized examples if allowed.

## 16. Risk Controls

### 16.1 Test Leakage

Risk: AutoResearch overfits to public or iFinD test sets.

Control:

- only Search/Train and Validation can influence version decisions;
- test sets are final-only;
- all version decisions must cite validation metrics;
- test failures cannot trigger parameter edits without creating a new test set.

### 16.2 Metadata Advantage

Risk: AutoSEAC-Fin outperforms baselines because it receives richer metadata.

Control:

- report chunk-only retrieval;
- report metadata-aware retrieval where all methods receive the same reranker;
- clearly separate chunking gains from reranking gains.

### 16.3 Dataset Overbreadth

Risk: too many benchmarks dilute the paper narrative.

Control:

- main paper focuses on DocFinQA, TAT-QA, FinanceBench, and iFinD;
- FinQA and MultiHiertt are diagnostics;
- newer agentic or broad benchmarks are future work unless fully integrated.

### 16.4 LLM Judge Bias

Risk: answer evaluation depends on judge model preferences.

Control:

- use rule-based numeric evaluation wherever possible;
- use blind LLM judge only for free-text answers;
- sample 10%-20% for human audit;
- report judge-human agreement.

### 16.5 Private Data License

Risk: iFinD data cannot be publicly released.

Control:

- keep public benchmarks as reproducible core;
- report iFinD as private real-world validation;
- publish only statistics, protocol, code, and anonymized examples if permitted.

## 17. Current Recommended First Action

The next implementation step is:

```text
TAT-QA -> canonical corpus/QA schema -> finance_units.py -> AutoSEAC-Fin chunker -> retrieval-only evaluation
```

Do not start AutoResearch parameter search until this single-dataset pipeline is
stable and produces trustworthy retrieval metrics.

