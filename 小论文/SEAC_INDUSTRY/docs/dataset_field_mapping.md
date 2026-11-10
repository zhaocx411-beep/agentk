# Financial Dataset Field Mapping

This document tracks how each public benchmark maps into the canonical
AutoSEAC-Fin corpus and QA schema.

The mapping should be updated after inspecting the downloaded raw files. Use
empty strings or empty lists for unavailable fields; do not invent metadata.

## Canonical Corpus Fields

| Canonical Field | Meaning | Required First Pass |
| --- | --- | --- |
| `dataset` | Dataset name | yes |
| `document_id` | Stable document id | yes |
| `file_name` | Human-readable document/file name | yes |
| `document_type` | 10-K, 10-Q, annual report, earnings release, etc. | yes |
| `company` | Company name | recommended |
| `ticker` | Ticker/symbol | recommended |
| `industry` | Industry/sector | optional |
| `report_date` | Report publication/date field | optional |
| `fiscal_year` | Fiscal year | recommended |
| `fiscal_quarter` | Fiscal quarter | optional |
| `source_uri` | Original source URL/path | optional |
| `license` | Dataset/document license note | optional |
| `blocks` | Ordered document blocks | yes |

## Canonical Block Fields

| Canonical Field | Meaning | Required First Pass |
| --- | --- | --- |
| `block_id` | Stable evidence id | yes |
| `block_index` | Order within document | yes |
| `block_type` | heading/paragraph/table/table_row/table_cell/etc. | yes |
| `heading_path` | Section path | optional |
| `text` | Text content or table preview | yes if no `rows` |
| `rows` | Table rows | yes for table blocks |
| `table_id` | Stable table id | recommended for tables |
| `table_title` | Table title/caption | recommended for tables |
| `row_header` | Row header for row/cell evidence | recommended |
| `column_header` | Column header for cell evidence | recommended |
| `metric` | Financial metric | recommended |
| `unit` | Unit/scale | recommended for numeric evidence |
| `currency` | Currency | optional |
| `period` | Period/year/quarter associated with evidence | recommended |
| `source_page` | Page if available | optional |
| `source_location` | Original paragraph/table/cell locator | optional |

## Canonical QA Fields

| Canonical Field | Meaning | Required First Pass |
| --- | --- | --- |
| `qa_id` | Stable QA id | yes |
| `dataset` | Dataset name | yes |
| `question` | Question text | yes |
| `reference_answer` | Gold/reference answer | yes |
| `answer_type` | numeric/fact/list/comparison/calculation/free_text | recommended |
| `question_type` | table/temporal/comparative/calculation/etc. | recommended |
| `company` | Company name | optional |
| `ticker` | Ticker | optional |
| `industry` | Industry | optional |
| `period` | Year/quarter/period | optional |
| `metric` | Financial metric | optional |
| `unit` | Unit/scale | optional |
| `source_document` | Linked document id/name | recommended |
| `gold_evidence_id` | Primary gold evidence id | required for retrieval |
| `gold_evidence_span` | Gold evidence text/span | recommended |
| `supporting_evidence_ids` | All required evidence ids | recommended |
| `gold_table_id` | Gold table id | optional |
| `gold_row_header` | Gold row header | optional |
| `gold_column_header` | Gold column header | optional |
| `calculation_formula` | Formula/program | optional |
| `calculation_inputs` | Required numeric inputs | optional |
| `audit_status` | verified/pending/fix/drop | recommended |
| `audit_notes` | Audit notes | optional |

## TAT-QA Mapping

Update this table after raw file inspection.

| Raw Field | Canonical Field | Transform | Status |
| --- | --- | --- | --- |
| TBD | `dataset` | constant `tatqa` | pending |
| TBD | `document_id` | stable source document/table id | pending |
| TBD | `question` | copy | pending |
| TBD | `reference_answer` | normalize answer text/value | pending |
| TBD | `gold_evidence_id` | paragraph/table uid if available, otherwise generated block id | pending |
| TBD | `gold_evidence_span` | supporting paragraph/table text if available | pending |
| TBD | `unit` | map scale/unit metadata | pending |
| TBD | `rows` | preserve table rows | pending |

Known risks:

- scale/unit may be stored separately from answer;
- supporting facts may point to table cells or paragraphs rather than a single
  block;
- generated block ids must remain deterministic.

## DocFinQA Mapping

Update this table after raw file inspection.

| Raw Field | Canonical Field | Transform | Status |
| --- | --- | --- | --- |
| TBD | `dataset` | constant `docfinqa` | pending |
| TBD | `document_id` | SEC report/document id | pending |
| TBD | `question` | copy | pending |
| TBD | `reference_answer` | normalize answer text/value | pending |
| TBD | `gold_evidence_id` | map to paragraph/table block id | pending |
| TBD | `supporting_evidence_ids` | preserve all supporting evidence where available | pending |
| TBD | `calculation_formula` | preserve reasoning program if available | pending |

Known risks:

- full-document source blocks may require separate parsing;
- evidence from original FinQA may need remapping into full-document blocks;
- long documents make evidence id alignment the main conversion risk.

## FinanceBench Mapping

Update this table after raw file inspection.

| Raw Field | Canonical Field | Transform | Status |
| --- | --- | --- | --- |
| TBD | `dataset` | constant `financebench` | pending |
| TBD | `document_id` | filing/company/year id | pending |
| TBD | `question` | copy | pending |
| TBD | `reference_answer` | copy/normalize | pending |
| TBD | `gold_evidence_span` | copy evidence string if license permits | pending |
| TBD | `source_page` | preserve page if available | pending |
| TBD | `company` | map company name | pending |
| TBD | `ticker` | map ticker if available | pending |
| TBD | `period` | map fiscal/reporting period | pending |

Known risks:

- public subset may not expose all original data points;
- gold evidence strings may not directly map to local document blocks without
  filing PDFs/text;
- treat as external held-out test unless official split status is clear.

