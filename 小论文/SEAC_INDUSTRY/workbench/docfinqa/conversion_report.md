# docfinqa Conversion Report

Raw directory: `workbench/raw/docfinqa`
Output directory: `workbench/docfinqa`

## Notes

- Scaffold generated before raw-field mapping is implemented.
- Inspect DocFinQA raw files and update docs/dataset_field_mapping.md before filling this converter.
- Priority mapping targets: full-document blocks, original evidence ids/spans, formula/program fields, and QA split policy.

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
