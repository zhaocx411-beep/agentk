# tatqa Conversion Report

Raw directory: `workbench/raw/tatqa`
Output directory: `workbench/tatqa`

## Notes

- Scaffold generated before raw-field mapping is implemented.
- Inspect TAT-QA raw files and update docs/dataset_field_mapping.md before filling this converter.
- Priority mapping targets: table rows, paragraphs, scale/unit, answer, derivation, and supporting evidence.

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
