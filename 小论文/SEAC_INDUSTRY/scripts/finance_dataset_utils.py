from __future__ import annotations

import json
from collections import Counter
from pathlib import Path
from typing import Any


DATASET_SPLITS = ("train", "validation", "test")


def read_json(path: str | Path) -> Any:
    return json.loads(Path(path).read_text(encoding="utf-8"))


def write_json(path: str | Path, data: Any) -> None:
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")


def clean_text(value: Any) -> str:
    if value is None:
        return ""
    return " ".join(str(value).split())


def canonical_document(
    *,
    dataset: str,
    document_id: str,
    file_name: str = "",
    document_type: str = "",
    company: str = "",
    ticker: str = "",
    industry: str = "",
    report_date: str = "",
    fiscal_year: str = "",
    fiscal_quarter: str = "",
    source_uri: str = "",
    license_note: str = "",
    blocks: list[dict] | None = None,
    metadata: dict | None = None,
) -> dict:
    return {
        "dataset": dataset,
        "document_id": document_id,
        "file_name": file_name or document_id,
        "document_type": document_type,
        "company": company,
        "ticker": ticker,
        "industry": industry,
        "report_date": report_date,
        "fiscal_year": fiscal_year,
        "fiscal_quarter": fiscal_quarter,
        "source_uri": source_uri,
        "license": license_note,
        "metadata": metadata or {},
        "blocks": blocks or [],
    }


def canonical_block(
    *,
    block_id: str,
    block_index: int,
    block_type: str,
    text: str = "",
    rows: list[list[str]] | None = None,
    heading_path: str = "",
    table_id: str = "",
    table_title: str = "",
    row_header: str = "",
    column_header: str = "",
    metric: str = "",
    unit: str = "",
    currency: str = "",
    period: str = "",
    source_page: str = "",
    source_location: str = "",
    metadata: dict | None = None,
) -> dict:
    return {
        "block_id": block_id,
        "block_index": block_index,
        "block_type": block_type,
        "heading_path": heading_path,
        "text": clean_text(text),
        "rows": rows or [],
        "table_id": table_id,
        "table_title": table_title,
        "row_header": row_header,
        "column_header": column_header,
        "metric": metric,
        "unit": unit,
        "currency": currency,
        "period": period,
        "source_page": source_page,
        "source_location": source_location,
        "metadata": metadata or {},
    }


def canonical_qa(
    *,
    qa_id: str,
    dataset: str,
    question: str,
    reference_answer: Any,
    answer_type: str = "",
    question_type: str = "",
    company: str = "",
    ticker: str = "",
    industry: str = "",
    period: str = "",
    metric: str = "",
    unit: str = "",
    source_document: str = "",
    gold_evidence_id: str = "",
    gold_evidence_span: str = "",
    supporting_evidence_ids: list[str] | None = None,
    gold_table_id: str = "",
    gold_row_header: str = "",
    gold_column_header: str = "",
    calculation_formula: str = "",
    calculation_inputs: list[dict] | None = None,
    audit_status: str = "pending",
    audit_notes: str = "",
    metadata: dict | None = None,
) -> dict:
    return {
        "qa_id": qa_id,
        "dataset": dataset,
        "question": clean_text(question),
        "reference_answer": clean_text(reference_answer),
        "answer_type": answer_type,
        "question_type": question_type,
        "company": company,
        "ticker": ticker,
        "industry": industry,
        "period": period,
        "metric": metric,
        "unit": unit,
        "source_document": source_document,
        "gold_evidence_id": gold_evidence_id,
        "gold_evidence_span": clean_text(gold_evidence_span),
        "supporting_evidence_ids": supporting_evidence_ids or [],
        "gold_table_id": gold_table_id,
        "gold_row_header": gold_row_header,
        "gold_column_header": gold_column_header,
        "calculation_formula": calculation_formula,
        "calculation_inputs": calculation_inputs or [],
        "audit_status": audit_status,
        "audit_notes": audit_notes,
        "metadata": metadata or {},
    }


def table_preview(rows: list[list[str]], limit: int = 2) -> str:
    preview_rows = rows[:limit]
    return " || ".join(" | ".join(clean_text(cell) for cell in row) for row in preview_rows)


def dataset_statistics(corpus: list[dict], qa_splits: dict[str, list[dict]]) -> dict:
    documents = len(corpus)
    blocks = [block for document in corpus for block in document.get("blocks", [])]
    block_types = Counter(block.get("block_type", "") for block in blocks)
    document_types = Counter(document.get("document_type", "") for document in corpus)
    qa_counts = {split: len(items) for split, items in qa_splits.items()}
    question_types = {
        split: Counter(item.get("question_type", "") for item in items)
        for split, items in qa_splits.items()
    }
    answer_types = {
        split: Counter(item.get("answer_type", "") for item in items)
        for split, items in qa_splits.items()
    }
    evidence_ids = {block.get("block_id") for block in blocks}
    evidence_coverage = {}
    for split, items in qa_splits.items():
        with_gold = [item for item in items if item.get("gold_evidence_id")]
        covered = [
            item for item in with_gold
            if item.get("gold_evidence_id") in evidence_ids
        ]
        evidence_coverage[split] = {
            "items_with_gold_evidence_id": len(with_gold),
            "items_with_mapped_gold_evidence_id": len(covered),
            "coverage": len(covered) / len(with_gold) if with_gold else 0.0,
        }
    return {
        "documents": documents,
        "blocks": len(blocks),
        "document_types": dict(document_types),
        "block_types": dict(block_types),
        "qa_counts": qa_counts,
        "question_types": {split: dict(counts) for split, counts in question_types.items()},
        "answer_types": {split: dict(counts) for split, counts in answer_types.items()},
        "evidence_coverage": evidence_coverage,
    }


def write_conversion_report(
    path: str | Path,
    *,
    dataset: str,
    raw_dir: str | Path,
    output_dir: str | Path,
    notes: list[str],
    statistics: dict | None = None,
) -> None:
    lines = [
        f"# {dataset} Conversion Report",
        "",
        f"Raw directory: `{raw_dir}`",
        f"Output directory: `{output_dir}`",
        "",
        "## Notes",
        "",
    ]
    lines.extend(f"- {note}" for note in notes)
    if statistics is not None:
        lines.extend(["", "## Statistics", "", "```json", json.dumps(statistics, ensure_ascii=False, indent=2), "```"])
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text("\n".join(lines) + "\n", encoding="utf-8")


def qa_path(output_dir: str | Path, split: str) -> Path:
    if split not in DATASET_SPLITS:
        raise ValueError(f"Unknown split {split!r}; expected one of {DATASET_SPLITS}")
    return Path(output_dir) / f"qa_{split}.json"

