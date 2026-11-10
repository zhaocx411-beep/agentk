"""Prepare re-adjudication batches for QA drafts under the multi-evidence model.

Records that the content pre-review could not approve are re-examined once the
evidence equivalence set is known. Each batch line carries the draft, the earlier
verdict and its reason, the resolved equivalence status, and the section paths of
every equivalent location so the reviewer can tell genuine passage reuse from a
question stem that is simply under-specified.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections import Counter
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from seac_chunking.units import document_units

PROTOCOL_VERSION = "industrial-seac-qa-readjudication-v1.0"
DEFAULT_PROJECTS = ["P02", "P03", "P04", "P05", "P06", "P07", "P08", "P09", "P10"]
REOPEN = {"REJECTED", "NEEDS_REWRITE"}


def unit_context(corpus: list[dict]) -> dict[str, dict]:
    context = {}
    for document in corpus:
        for unit in document_units(document):
            context[unit.unit_id] = {
                "document": document["file_name"],
                "heading_path": unit.heading_path,
                "block_type": unit.block_type,
            }
    return context


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workbench-root", default="workbench")
    parser.add_argument("--equivalence-root", default="outputs/qa_evidence_equivalence_v1")
    parser.add_argument("--prereview-root", default="outputs/qa_prereview_v1")
    parser.add_argument("--output-root", default="outputs/qa_readjudication_v1")
    parser.add_argument("--projects", nargs="*", default=DEFAULT_PROJECTS)
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    output_root = root / args.output_root
    batch_dir = output_root / "batches"
    batch_dir.mkdir(parents=True, exist_ok=True)

    manifest = {"protocol_version": PROTOCOL_VERSION, "projects": {}, "record_total": 0}
    for project in args.projects:
        expanded = json.loads(
            (root / args.equivalence_root / f"qa_evidence_expanded_{project}.json").read_text(encoding="utf-8")
        )
        prereview = {
            item["id"]: item
            for item in json.loads(
                (root / args.prereview_root / f"qa_content_prereview_{project}.json").read_text(encoding="utf-8")
            )
        }
        context = unit_context(
            json.loads((root / args.workbench_root / project / "corpus.json").read_text(encoding="utf-8"))
        )

        payload = []
        rendered = []
        for record in expanded:
            composite = f'{project}-{record["qa_id"]}'
            earlier = prereview.get(composite)
            if not earlier or earlier["content_verdict"] not in REOPEN:
                continue
            resolution = record["evidence_equivalence"]
            locations = [
                {
                    "unit_id": unit_id,
                    "document": context.get(unit_id, {}).get("document", ""),
                    "heading_path": context.get(unit_id, {}).get("heading_path", ""),
                }
                for unit_id in resolution.get("unit_ids", [])[:12]
            ]
            payload.append(
                {
                    "id": composite,
                    "qa_id": record["qa_id"],
                    "project_id": project,
                    "question": record.get("question", ""),
                    "reference_answer": record.get("reference_answer", ""),
                    "question_type": record.get("question_type", ""),
                    "source_document": record.get("source_document", ""),
                    "gold_evidence_span": record.get("gold_evidence_span", ""),
                    "declared_unit_id": record.get("gold_evidence_unit_id", ""),
                    "earlier_verdict": earlier["content_verdict"],
                    "earlier_notes": earlier["prereview_notes"],
                    "equivalence_status": resolution["status"],
                    "occurrence_count": resolution["occurrence_count"],
                    "cross_document": resolution.get("cross_document", False),
                    "equivalent_locations": locations,
                }
            )
            location_lines = [
                f'    - {item["document"]} :: {item["heading_path"] or "(no heading)"} :: {item["unit_id"]}'
                for item in locations
            ]
            rendered.append(
                "\n".join(
                    [
                        f'[{composite}] earlier={earlier["content_verdict"]} type={record.get("question_type", "")}',
                        f'  Q: {record.get("question", "")}',
                        f'  A: {record.get("reference_answer", "")}',
                        f'  span: {record.get("gold_evidence_span", "")}',
                        f'  earlier_reason: {earlier["prereview_notes"]}',
                        f'  equivalence: {resolution["status"]} occurrences={resolution["occurrence_count"]} '
                        f'cross_document={resolution.get("cross_document", False)}',
                        "  locations:",
                        *location_lines,
                    ]
                )
            )

        (batch_dir / f"{project}.json").write_text(
            json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )
        (batch_dir / f"{project}.txt").write_text("\n".join(rendered) + "\n", encoding="utf-8")
        manifest["projects"][project] = {
            "reopened_count": len(payload),
            "earlier_verdicts": dict(sorted(Counter(item["earlier_verdict"] for item in payload).items())),
            "equivalence_status": dict(sorted(Counter(item["equivalence_status"] for item in payload).items())),
        }
        manifest["record_total"] += len(payload)

    (output_root / "readjudication_manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(json.dumps(manifest, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
