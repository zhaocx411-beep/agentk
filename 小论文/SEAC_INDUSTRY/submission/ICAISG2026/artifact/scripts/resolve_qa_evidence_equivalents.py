"""Resolve every corpus location that carries a QA record's evidence span.

Enterprise document sets reuse whole passages across the technical agreement,
the proposal and the requirements specification of one project. A single gold
evidence unit is then indefensible: the same sentence legitimately exists in
several places. This script turns the declared single location into the full
equivalence set so retrieval can be credited for returning any of them.

Spans that occur implausibly often are not expanded. Such a span is not a
distinctive piece of evidence, and the question needs rewriting instead.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from collections import Counter
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from seac_chunking.units import document_units

PROTOCOL_VERSION = "industrial-seac-qa-evidence-equivalence-v1.0"
DEFAULT_PROJECTS = ["P02", "P03", "P04", "P05", "P06", "P07", "P08", "P09", "P10"]
MIN_SPAN_CHARS = 8


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def normalize(text: str) -> str:
    return re.sub(r"\s+", "", text or "")


def own_content(text: str) -> str:
    """Return the unit's own content, dropping the repeated table-header context.

    Table-row units are rendered as `表头: <header>` followed by `数据行: <row>`.
    The header is context copied onto every row of the table, so matching against
    it would report every row of a table as an equivalent location for evidence
    that in fact occurs once.
    """

    marker = "数据行:"
    normalized = normalize(text)
    position = normalized.find(marker)
    return normalized[position + len(marker) :] if position != -1 else normalized


def corpus_units(corpus: list[dict]) -> list[dict]:
    units = []
    for document in corpus:
        for unit in document_units(document):
            units.append(
                {
                    "unit_id": unit.unit_id,
                    "block_id": unit.source_block_id,
                    "document": document["file_name"],
                    "normalized": own_content(unit.text),
                }
            )
    return units


def resolve(record: dict, units: list[dict], max_occurrences: int) -> dict:
    span = own_content(record.get("gold_evidence_span", ""))
    declared_unit = record.get("gold_evidence_unit_id", "")
    if len(span) < MIN_SPAN_CHARS:
        return {
            "status": "SPAN_TOO_SHORT",
            "occurrence_count": 0,
            "unit_ids": [declared_unit] if declared_unit else [],
            "block_ids": [record.get("gold_evidence_id", "")],
            "documents": [record.get("source_document", "")],
        }

    matches = [unit for unit in units if span and span in unit["normalized"]]
    unit_ids = [unit["unit_id"] for unit in matches]
    documents = sorted({unit["document"] for unit in matches})
    status = "UNIQUE"
    if len(matches) > max_occurrences:
        status = "TOO_MANY_OCCURRENCES"
    elif len(matches) > 1:
        status = "EQUIVALENT_SET"
    elif not matches:
        status = "SPAN_NOT_FOUND"
    return {
        "status": status,
        "occurrence_count": len(matches),
        "unit_ids": unit_ids,
        "block_ids": sorted({unit["block_id"] for unit in matches}),
        "documents": documents,
        "cross_document": len(documents) > 1,
        "declared_unit_included": declared_unit in unit_ids,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workbench-root", default="workbench")
    parser.add_argument("--output-root", default="outputs/qa_evidence_equivalence_v1")
    parser.add_argument("--projects", nargs="*", default=DEFAULT_PROJECTS)
    parser.add_argument("--max-occurrences", type=int, default=12)
    parser.add_argument("--write", action="store_true", help="write the expanded QA records")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    workbench = root / args.workbench_root
    output_root = root / args.output_root
    output_root.mkdir(parents=True, exist_ok=True)

    report = {
        "protocol_version": PROTOCOL_VERSION,
        "max_occurrences": args.max_occurrences,
        "min_span_chars": MIN_SPAN_CHARS,
        "projects": {},
        "totals": {"records": 0, "status": Counter(), "occurrence_histogram": Counter(), "cross_document": 0},
    }
    for project in args.projects:
        qa_path = workbench / project / "annotation_pack" / "qa_draft.json"
        corpus_path = workbench / project / "corpus.json"
        records = json.loads(qa_path.read_text(encoding="utf-8"))
        units = corpus_units(json.loads(corpus_path.read_text(encoding="utf-8")))

        expanded = []
        statuses: Counter[str] = Counter()
        cross_document = 0
        for record in records:
            resolution = resolve(record, units, args.max_occurrences)
            statuses[resolution["status"]] += 1
            cross_document += bool(resolution.get("cross_document"))
            report["totals"]["occurrence_histogram"][min(resolution["occurrence_count"], 20)] += 1
            item = dict(record)
            if resolution["status"] in {"UNIQUE", "EQUIVALENT_SET"}:
                item["gold_evidence_unit_ids"] = resolution["unit_ids"]
                item["gold_evidence_ids"] = resolution["block_ids"]
                item["gold_evidence_spans"] = [record.get("gold_evidence_span", "")]
            item["evidence_equivalence"] = resolution
            expanded.append(item)

        if args.write:
            (output_root / f"qa_evidence_expanded_{project}.json").write_text(
                json.dumps(expanded, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
            )
        report["projects"][project] = {
            "qa_source": str(qa_path.relative_to(root)),
            "qa_source_sha256": sha256_file(qa_path),
            "record_count": len(records),
            "status": dict(sorted(statuses.items())),
            "cross_document_records": cross_document,
            "max_observed_occurrences": max(
                (item["evidence_equivalence"]["occurrence_count"] for item in expanded), default=0
            ),
        }
        report["totals"]["records"] += len(records)
        report["totals"]["status"].update(statuses)
        report["totals"]["cross_document"] += cross_document

    report["totals"]["status"] = dict(sorted(report["totals"]["status"].items()))
    report["totals"]["occurrence_histogram"] = dict(sorted(report["totals"]["occurrence_histogram"].items()))
    (output_root / "equivalence_report.json").write_text(
        json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(json.dumps({"totals": report["totals"], "projects": report["projects"]}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
