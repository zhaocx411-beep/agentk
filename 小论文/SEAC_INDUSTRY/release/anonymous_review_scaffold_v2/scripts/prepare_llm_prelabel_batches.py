"""Split blinded boundary candidates into review batches for LLM pre-labelling.

The batches contain exactly the fields a human annotator sees in the A/B
workbooks. No SEAC prediction, score, or retrieval result is included.
"""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

PROTOCOL_VERSION = "industrial-seac-llm-prelabel-v1.0"
DEFAULT_PROJECTS = ["P02", "P03", "P04", "P05", "P06", "P07", "P08", "P09", "P10"]
VISIBLE_FIELDS = (
    "candidate_id",
    "project_id",
    "source_document",
    "document_type",
    "boundary_stratum",
    "left_section",
    "left_block_type",
    "left_evidence",
    "right_section",
    "right_block_type",
    "right_evidence",
)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def render(record: dict) -> str:
    left_section = record["left_section"] or "(none)"
    right_section = record["right_section"] or "(none)"
    right_display = "<same>" if right_section == left_section else right_section
    return "\n".join(
        [
            f'[{record["candidate_id"]}] stratum={record["boundary_stratum"]} doc_type={record["document_type"]}',
            f'  L({record["left_block_type"]}) sec={left_section}',
            f'    {record["left_evidence"]}',
            f'  R({record["right_block_type"]}) sec={right_display}',
            f'    {record["right_evidence"]}',
        ]
    )


def batch_records(records: list[dict], size: int) -> list[list[dict]]:
    return [records[start : start + size] for start in range(0, len(records), size)]


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workbench-root", default="workbench")
    parser.add_argument("--output-root", default="outputs/llm_prelabel_v1")
    parser.add_argument("--projects", nargs="*", default=DEFAULT_PROJECTS)
    parser.add_argument("--batch-size", type=int, default=75)
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    workbench = root / args.workbench_root
    output_root = root / args.output_root
    batch_dir = output_root / "batches"
    batch_dir.mkdir(parents=True, exist_ok=True)

    manifest = {
        "protocol_version": PROTOCOL_VERSION,
        "batch_size": args.batch_size,
        "label_source": "LLM_PRELABEL",
        "human_verification_required": True,
        "public_release_allowed": False,
        "projects": {},
    }
    total = 0
    for project in args.projects:
        source = workbench / project / "annotation_pack" / "boundary_blind_master.json"
        records = json.loads(source.read_text(encoding="utf-8"))
        seen = set()
        for record in records:
            candidate_id = record["candidate_id"]
            if candidate_id in seen:
                raise ValueError(f"{project}: duplicate candidate_id {candidate_id}")
            seen.add(candidate_id)

        batches = batch_records(records, args.batch_size)
        for index, batch in enumerate(batches, 1):
            name = f"{project}_b{index:02d}"
            payload = "\n".join(render(record) for record in batch)
            (batch_dir / f"{name}.txt").write_text(payload + "\n", encoding="utf-8")
            (batch_dir / f"{name}.json").write_text(
                json.dumps(
                    [{field: record[field] for field in VISIBLE_FIELDS} for record in batch],
                    ensure_ascii=False,
                    indent=2,
                )
                + "\n",
                encoding="utf-8",
            )
        manifest["projects"][project] = {
            "source": str(source.relative_to(root)),
            "source_sha256": sha256_file(source),
            "candidate_count": len(records),
            "batch_count": len(batches),
            "batch_names": [f"{project}_b{index:02d}" for index in range(1, len(batches) + 1)],
        }
        total += len(records)

    manifest["candidate_total"] = total
    (output_root / "batch_manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(json.dumps({"candidate_total": total, "projects": len(args.projects)}, indent=2))


if __name__ == "__main__":
    main()
