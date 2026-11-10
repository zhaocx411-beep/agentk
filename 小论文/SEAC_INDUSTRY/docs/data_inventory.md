# Financial Dataset Inventory

This inventory tracks the public benchmark files used by AutoSEAC-Fin.

Keep this document synchronized with the local `workbench/raw/` directory. Raw
downloaded files should not be edited manually; all normalization should happen
through conversion scripts.

## Local Directory Layout

```text
workbench/raw/docfinqa/       # Original downloaded DocFinQA files
workbench/raw/tatqa/          # Original downloaded TAT-QA files
workbench/raw/financebench/   # Original downloaded FinanceBench files

workbench/docfinqa/           # Converted canonical corpus/QA files
workbench/tatqa/              # Converted canonical corpus/QA files
workbench/financebench/       # Converted canonical corpus/QA files
```

Expected converted outputs:

```text
corpus.json
qa_train.json
qa_validation.json
qa_test.json
dataset_statistics.json
conversion_report.md
```

Some datasets may not provide all three official QA splits. If a split is
missing, document the fallback in `conversion_report.md`.

## Dataset Inventory Table

| Dataset | Source URL | Downloaded Date | Raw Path | Official Splits | License Status | Gold Evidence | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| DocFinQA | TBD | TBD | `workbench/raw/docfinqa/` | TBD | TBD | TBD | Long-document financial QA; preferred first long-document benchmark. |
| TAT-QA | TBD | TBD | `workbench/raw/tatqa/` | TBD | TBD | TBD | Preferred first table-aware benchmark. |
| FinanceBench | TBD | TBD | `workbench/raw/financebench/` | TBD | TBD | TBD | External held-out public test unless a documented official split is available. |

## Per-Dataset Checklist

Fill one checklist per dataset after download.

### DocFinQA

- Source URL:
- Download command or manual download notes:
- Downloaded date:
- Raw file names:
- File checksums if available:
- Official split files:
- License file path:
- Can redistribute raw data: yes/no/unknown
- Can publish converted corpus: yes/no/unknown
- Can publish converted QA: yes/no/unknown
- Has gold evidence ids/spans: yes/no/partial
- Has page/source citations: yes/no/partial
- Has table structure: yes/no/partial
- Has formulas/programs: yes/no/partial
- Conversion script:
- Conversion status:
- Known issues:

### TAT-QA

- Source URL:
- Download command or manual download notes:
- Downloaded date:
- Raw file names:
- File checksums if available:
- Official split files:
- License file path:
- Can redistribute raw data: yes/no/unknown
- Can publish converted corpus: yes/no/unknown
- Can publish converted QA: yes/no/unknown
- Has gold evidence ids/spans: yes/no/partial
- Has page/source citations: yes/no/partial
- Has table structure: yes/no/partial
- Has formulas/programs: yes/no/partial
- Conversion script:
- Conversion status:
- Known issues:

### FinanceBench

- Source URL:
- Download command or manual download notes:
- Downloaded date:
- Raw file names:
- File checksums if available:
- Official split files:
- License file path:
- Can redistribute raw data: yes/no/unknown
- Can publish converted corpus: yes/no/unknown
- Can publish converted QA: yes/no/unknown
- Has gold evidence ids/spans: yes/no/partial
- Has page/source citations: yes/no/partial
- Has table structure: yes/no/partial
- Has formulas/programs: yes/no/partial
- Conversion script:
- Conversion status:
- Known issues:

## Conversion Rules

- Do not edit files under `workbench/raw/` manually.
- Converted files must be reproducible from raw files and scripts.
- Any dropped sample must be counted and explained in `conversion_report.md`.
- Any generated evidence id must be deterministic.
- Any missing canonical field should be an empty string, empty list, or explicit
  `null`, not an invented value.
- Dataset-specific source fields should be preserved in metadata when useful.

