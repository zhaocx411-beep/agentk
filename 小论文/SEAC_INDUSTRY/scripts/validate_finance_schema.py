from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.finance_dataset_utils import DATASET_SPLITS, read_json


DOCUMENT_REQUIRED = ("dataset", "document_id", "file_name", "document_type", "blocks")
BLOCK_REQUIRED = ("block_id", "block_index", "block_type")
QA_REQUIRED = ("qa_id", "dataset", "question", "reference_answer")


def is_missing(value: Any) -> bool:
    return value is None or value == "" or value == []


def validate_corpus(corpus: list[dict], strict_financial_metadata: bool = False) -> tuple[list[str], set[str]]:
    errors = []
    evidence_ids = set()
    seen_documents = set()
    seen_blocks = set()

    if not isinstance(corpus, list):
        return ["corpus must be a JSON array"], evidence_ids

    for doc_index, document in enumerate(corpus, 1):
        prefix = f"document[{doc_index}]"
        if not isinstance(document, dict):
            errors.append(f"{prefix}: must be an object")
            continue
        for field in DOCUMENT_REQUIRED:
            if field not in document or is_missing(document.get(field)):
                errors.append(f"{prefix}: missing required field {field!r}")
        document_id = document.get("document_id", "")
        if document_id:
            if document_id in seen_documents:
                errors.append(f"{prefix}: duplicate document_id {document_id!r}")
            seen_documents.add(document_id)
        blocks = document.get("blocks", [])
        if not isinstance(blocks, list):
            errors.append(f"{prefix}: blocks must be a list")
            continue
        if strict_financial_metadata:
            if not document.get("company") and not document.get("ticker"):
                errors.append(f"{prefix}: strict mode requires company or ticker")
            if not document.get("fiscal_year") and not document.get("report_date"):
                errors.append(f"{prefix}: strict mode requires fiscal_year or report_date")
        for block_index, block in enumerate(blocks, 1):
            block_prefix = f"{prefix}.blocks[{block_index}]"
            if not isinstance(block, dict):
                errors.append(f"{block_prefix}: must be an object")
                continue
            for field in BLOCK_REQUIRED:
                if field not in block or is_missing(block.get(field)):
                    errors.append(f"{block_prefix}: missing required field {field!r}")
            block_id = block.get("block_id", "")
            if block_id:
                if block_id in seen_blocks:
                    errors.append(f"{block_prefix}: duplicate block_id {block_id!r}")
                seen_blocks.add(block_id)
                evidence_ids.add(block_id)
            has_text = bool(block.get("text"))
            has_rows = bool(block.get("rows"))
            if not has_text and not has_rows:
                errors.append(f"{block_prefix}: expected text or rows")
            if block.get("block_type") in {"table", "table_row", "table_cell"} and strict_financial_metadata:
                if not block.get("table_title") and not block.get("table_id"):
                    errors.append(f"{block_prefix}: strict mode requires table_title or table_id for table evidence")
                if not block.get("unit") and not block.get("currency"):
                    errors.append(f"{block_prefix}: strict mode recommends unit or currency for table evidence")
    return errors, evidence_ids


def validate_qa_items(
    items: list[dict],
    *,
    split: str,
    evidence_ids: set[str],
    require_gold_evidence: bool = False,
) -> list[str]:
    errors = []
    seen_qa = set()
    if not isinstance(items, list):
        return [f"qa_{split}: must be a JSON array"]
    for index, item in enumerate(items, 1):
        prefix = f"qa_{split}[{index}]"
        if not isinstance(item, dict):
            errors.append(f"{prefix}: must be an object")
            continue
        for field in QA_REQUIRED:
            if field not in item or is_missing(item.get(field)):
                errors.append(f"{prefix}: missing required field {field!r}")
        qa_id = item.get("qa_id", "")
        if qa_id:
            if qa_id in seen_qa:
                errors.append(f"{prefix}: duplicate qa_id {qa_id!r}")
            seen_qa.add(qa_id)
        gold_id = item.get("gold_evidence_id", "")
        if require_gold_evidence and not gold_id:
            errors.append(f"{prefix}: require-gold-evidence mode requires gold_evidence_id")
        if gold_id and gold_id not in evidence_ids:
            errors.append(f"{prefix}: gold_evidence_id {gold_id!r} is not present in corpus block ids")
        supporting = item.get("supporting_evidence_ids", [])
        if supporting and not isinstance(supporting, list):
            errors.append(f"{prefix}: supporting_evidence_ids must be a list")
        for evidence_id in supporting if isinstance(supporting, list) else []:
            if evidence_id not in evidence_ids:
                errors.append(f"{prefix}: supporting evidence id {evidence_id!r} is not present in corpus block ids")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description="Validate AutoSEAC-Fin canonical corpus/QA files.")
    parser.add_argument("--dataset-dir", required=True)
    parser.add_argument("--strict-financial-metadata", action="store_true")
    parser.add_argument("--require-gold-evidence", action="store_true")
    parser.add_argument("--json", action="store_true", help="Print validation report as JSON.")
    args = parser.parse_args()

    dataset_dir = Path(args.dataset_dir)
    corpus_path = dataset_dir / "corpus.json"
    if not corpus_path.exists():
        print(f"Missing corpus file: {corpus_path}", file=sys.stderr)
        return 2

    corpus = read_json(corpus_path)
    errors, evidence_ids = validate_corpus(corpus, strict_financial_metadata=args.strict_financial_metadata)

    split_counts = {}
    for split in DATASET_SPLITS:
        qa_file = dataset_dir / f"qa_{split}.json"
        if not qa_file.exists():
            split_counts[split] = None
            continue
        items = read_json(qa_file)
        split_counts[split] = len(items) if isinstance(items, list) else None
        errors.extend(
            validate_qa_items(
                items,
                split=split,
                evidence_ids=evidence_ids,
                require_gold_evidence=args.require_gold_evidence,
            )
        )

    report = {
        "dataset_dir": str(dataset_dir),
        "document_count": len(corpus) if isinstance(corpus, list) else None,
        "block_count": len(evidence_ids),
        "qa_split_counts": split_counts,
        "error_count": len(errors),
        "errors": errors,
    }

    if args.json:
        print(json.dumps(report, ensure_ascii=False, indent=2))
    else:
        print(f"dataset_dir={dataset_dir}")
        print(f"documents={report['document_count']}")
        print(f"blocks={report['block_count']}")
        print(f"qa_split_counts={split_counts}")
        if errors:
            print(f"errors={len(errors)}")
            for error in errors[:100]:
                print(f"- {error}")
            if len(errors) > 100:
                print(f"... {len(errors) - 100} more errors")
        else:
            print("schema validation passed")
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())

