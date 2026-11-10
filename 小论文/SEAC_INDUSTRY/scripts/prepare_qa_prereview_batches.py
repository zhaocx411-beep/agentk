"""Prepare QA content pre-review batches with resolved evidence text.

For every QA draft the batch carries the question, the reference answer, the
declared parent block, the declared minimal evidence unit, the declared span and
the actual unit text resolved from the corpus, plus mechanical checks that do not
require judgement (unit existence, span containment, parent consistency,
near-duplicate questions).
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from collections import defaultdict
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from seac_chunking.units import document_units

PROTOCOL_VERSION = "industrial-seac-qa-prereview-v1.0"
DEFAULT_PROJECTS = ["P01", "P02", "P03", "P04", "P05", "P06", "P07", "P08", "P09", "P10"]


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def normalize(text: str) -> str:
    return re.sub(r"\s+", "", text or "")


def unit_index(corpus: list[dict]) -> dict[str, dict]:
    index: dict[str, dict] = {}
    for document in corpus:
        for unit in document_units(document):
            index[unit.unit_id] = {
                "text": unit.text,
                "block_id": unit.source_block_id,
                "block_type": unit.block_type,
                "heading_path": unit.heading_path,
                "file_name": document["file_name"],
            }
    return index


def find_qa_source(workbench: Path, project: str) -> Path:
    candidates = [
        workbench / project / "annotation_pack" / "qa_draft.json",
        workbench / project / "qa_review_set.json",
    ]
    for candidate in candidates:
        if candidate.is_file():
            return candidate
    raise FileNotFoundError(f"{project}: no QA draft found in {[str(item) for item in candidates]}")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workbench-root", default="workbench")
    parser.add_argument("--output-root", default="outputs/qa_prereview_v1")
    parser.add_argument("--projects", nargs="*", default=DEFAULT_PROJECTS)
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    workbench = root / args.workbench_root
    output_root = root / args.output_root
    batch_dir = output_root / "batches"
    batch_dir.mkdir(parents=True, exist_ok=True)

    manifest = {"protocol_version": PROTOCOL_VERSION, "projects": {}, "record_total": 0}
    for project in args.projects:
        qa_path = find_qa_source(workbench, project)
        corpus_path = workbench / project / "corpus.json"
        records = json.loads(qa_path.read_text(encoding="utf-8"))
        units = unit_index(json.loads(corpus_path.read_text(encoding="utf-8")))

        question_groups: dict[str, list[str]] = defaultdict(list)
        for record in records:
            question_groups[normalize(record.get("question", ""))].append(record.get("qa_id", ""))

        rendered = []
        payload = []
        for record in records:
            qa_id = record.get("qa_id", "")
            composite = f"{project}-{qa_id}"
            unit_id = record.get("gold_evidence_unit_id") or ""
            span = record.get("gold_evidence_span") or ""
            unit = units.get(unit_id)
            unit_text = unit["text"] if unit else ""
            checks = {
                "unit_resolved": bool(unit),
                "span_in_unit": bool(unit) and normalize(span) in normalize(unit_text),
                "unit_under_declared_block": bool(unit) and unit["block_id"] == record.get("gold_evidence_id"),
                "answer_in_span": bool(normalize(record.get("reference_answer", "")))
                and normalize(record.get("reference_answer", "")) in normalize(span),
                "duplicate_question_ids": [
                    other for other in question_groups[normalize(record.get("question", ""))] if other != qa_id
                ],
            }
            payload.append(
                {
                    "id": composite,
                    "qa_id": qa_id,
                    "project_id": project,
                    "question": record.get("question", ""),
                    "reference_answer": record.get("reference_answer", ""),
                    "question_type": record.get("question_type", ""),
                    "source_document": record.get("source_document", ""),
                    "source_section": record.get("source_section", ""),
                    "gold_evidence_id": record.get("gold_evidence_id", ""),
                    "gold_evidence_unit_id": unit_id,
                    "gold_evidence_span": span,
                    "resolved_unit_text": unit_text,
                    "resolved_unit_block_type": unit["block_type"] if unit else "",
                    "resolved_unit_heading_path": unit["heading_path"] if unit else "",
                    "mechanical_checks": checks,
                }
            )
            flags = [name for name, value in checks.items() if name != "duplicate_question_ids" and not value]
            rendered.append(
                "\n".join(
                    [
                        f'[{composite}] type={record.get("question_type", "")} doc={record.get("source_document", "")}',
                        f'  Q: {record.get("question", "")}',
                        f'  A: {record.get("reference_answer", "")}',
                        f'  parent_block={record.get("gold_evidence_id", "")}',
                        f"  unit={unit_id} block_type={unit['block_type'] if unit else '(unresolved)'}",
                        f"  heading={unit['heading_path'] if unit else ''}",
                        f"  span: {span}",
                        f"  unit_text: {unit_text[:600]}",
                        f'  checks_failed: {", ".join(flags) or "none"}'
                        + (
                            f'; duplicate_of: {", ".join(checks["duplicate_question_ids"])}'
                            if checks["duplicate_question_ids"]
                            else ""
                        ),
                    ]
                )
            )

        (batch_dir / f"{project}.json").write_text(
            json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )
        (batch_dir / f"{project}.txt").write_text("\n".join(rendered) + "\n", encoding="utf-8")
        manifest["projects"][project] = {
            "qa_source": str(qa_path.relative_to(root)),
            "qa_source_sha256": sha256_file(qa_path),
            "record_count": len(records),
            "unresolved_units": sum(1 for item in payload if not item["mechanical_checks"]["unit_resolved"]),
            "span_not_in_unit": sum(1 for item in payload if not item["mechanical_checks"]["span_in_unit"]),
            "answer_not_in_span": sum(1 for item in payload if not item["mechanical_checks"]["answer_in_span"]),
            "duplicate_question_records": sum(
                1 for item in payload if item["mechanical_checks"]["duplicate_question_ids"]
            ),
        }
        manifest["record_total"] += len(records)

    (output_root / "prereview_manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(json.dumps(manifest, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
