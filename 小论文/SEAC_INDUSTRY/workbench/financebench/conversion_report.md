# financebench Conversion Report

Raw directory: `workbench/raw/financebench`
Output directory: `workbench/financebench`

## Notes

- Scaffold generated before raw-field mapping is implemented.
- Inspect FinanceBench raw files and update docs/dataset_field_mapping.md before filling this converter.
- Treat as external held-out test unless official train/validation splits are documented.
- Priority mapping targets: evidence strings, source pages, company, ticker, filing/document id, and answer.

## Statistics

```json
{
  "documents": 0,
  "blocks": 0,
  "document_types": {},
  "block_types": {},
  "qa_counts": {
    "train": 0,
    "validation": 0,
    "test": 0
  },
  "question_types": {
    "train": {},
    "validation": {},
    "test": {}
  },
  "answer_types": {
    "train": {},
    "validation": {},
    "test": {}
  },
  "evidence_coverage": {
    "train": {
      "items_with_gold_evidence_id": 0,
      "items_with_mapped_gold_evidence_id": 0,
      "coverage": 0.0
    },
    "validation": {
      "items_with_gold_evidence_id": 0,
      "items_with_mapped_gold_evidence_id": 0,
      "coverage": 0.0
    },
    "test": {
      "items_with_gold_evidence_id": 0,
      "items_with_mapped_gold_evidence_id": 0,
      "coverage": 0.0
    }
  }
}
```
