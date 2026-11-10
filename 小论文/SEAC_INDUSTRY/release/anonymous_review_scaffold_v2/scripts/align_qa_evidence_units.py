from __future__ import annotations

import argparse
import json
import re
from collections import Counter
from pathlib import Path


def normalize(text: str) -> str:
    return re.sub(r"\s+", " ", str(text)).strip()


def align_item(item: dict, block: dict) -> dict:
    aligned = dict(item)
    rows = block.get("rows") or []
    if not rows:
        aligned["gold_evidence_unit_id"] = block["block_id"]
        aligned["unit_alignment_status"] = "BLOCK_IS_UNIT"
        return aligned

    span = normalize(item.get("gold_evidence_span", ""))
    exact_matches = []
    containing_matches = []
    for row_index, row in enumerate(rows, 1):
        row_text = normalize(" | ".join(str(cell) for cell in row))
        if span and row_text == span:
            exact_matches.append(row_index)
        elif span and span in row_text:
            containing_matches.append(row_index)
    matches = exact_matches or containing_matches
    if len(matches) == 1:
        aligned["gold_evidence_unit_id"] = f'{block["block_id"]}#R{matches[0]:03d}'
        aligned["unit_alignment_status"] = (
            "UNIQUE_EXACT_ROW" if exact_matches else "UNIQUE_ROW_CONTAINS_GOLD_SPAN"
        )
    elif len(matches) > 1:
        aligned["gold_evidence_unit_id"] = ""
        aligned["unit_alignment_status"] = "AMBIGUOUS_MULTIPLE_ROWS"
        aligned["unit_alignment_candidates"] = [
            f'{block["block_id"]}#R{row_index:03d}' for row_index in matches
        ]
    else:
        aligned["gold_evidence_unit_id"] = ""
        aligned["unit_alignment_status"] = "NO_MATCH_REQUIRES_HUMAN_REVIEW"
    return aligned


def align(corpus: list[dict], questions: list[dict]) -> tuple[list[dict], dict]:
    blocks = {
        block["block_id"]: block
        for document in corpus
        for block in document["blocks"]
    }
    output = []
    missing_blocks = []
    for item in questions:
        evidence_id = item.get("gold_evidence_id", "")
        block = blocks.get(evidence_id)
        if not block:
            aligned = dict(item)
            aligned["gold_evidence_unit_id"] = ""
            aligned["unit_alignment_status"] = "MISSING_EVIDENCE_BLOCK"
            missing_blocks.append(item.get("qa_id", ""))
        else:
            aligned = align_item(item, block)
        output.append(aligned)
    statuses = Counter(item["unit_alignment_status"] for item in output)
    table_items = [item for item in output if item.get("evidence_block_type") == "table"]
    summary = {
        "protocol_version": "industrial-seac-qa-unit-alignment-v1.0",
        "qa_count": len(output),
        "status_counts": dict(sorted(statuses.items())),
        "table_qa_count": len(table_items),
        "table_qa_aligned_count": sum(bool(item.get("gold_evidence_unit_id")) for item in table_items),
        "missing_block_qa_ids": missing_blocks,
    }
    return output, summary


def main() -> None:
    parser = argparse.ArgumentParser(description="Align QA gold spans to precise corpus units.")
    parser.add_argument("--corpus", required=True)
    parser.add_argument("--qa", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--summary")
    parser.add_argument("--require-all-table", action="store_true")
    args = parser.parse_args()

    corpus = json.loads(Path(args.corpus).read_text(encoding="utf-8"))
    questions = json.loads(Path(args.qa).read_text(encoding="utf-8"))
    aligned, summary = align(corpus, questions)
    output_path = Path(args.output)
    output_path.write_text(json.dumps(aligned, ensure_ascii=False, indent=2), encoding="utf-8")
    summary_path = Path(args.summary) if args.summary else output_path.with_suffix(".alignment_summary.json")
    summary_path.write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    if args.require_all_table and summary["table_qa_aligned_count"] != summary["table_qa_count"]:
        raise ValueError("Not all table QA records have a unique unit alignment")


if __name__ == "__main__":
    main()
