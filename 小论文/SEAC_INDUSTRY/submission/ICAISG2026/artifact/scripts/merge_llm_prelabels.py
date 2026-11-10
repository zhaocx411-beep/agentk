"""Validate and merge LLM pre-labels into per-project provenance files.

Pre-labels are not a human gold standard. The merged files carry
`label_source=LLM_PRELABEL` and are rejected by the formal gates until the
human verification stage in `promote_llm_prelabels.py` has been completed.
"""

from __future__ import annotations

import argparse
import hashlib
import json
from collections import Counter, defaultdict
from pathlib import Path

PROTOCOL_VERSION = "industrial-seac-llm-prelabel-v1.0"
LABELS = {"BREAK", "NO_BREAK", "NOISE"}
FLAGS = {"CLEAR", "NEEDS_HUMAN_REVIEW"}


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def read_label_lines(path: Path) -> list[dict]:
    records = []
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        stripped = line.strip()
        if not stripped:
            continue
        try:
            record = json.loads(stripped)
        except json.JSONDecodeError as error:
            raise ValueError(f"{path.name}:{number}: invalid JSON ({error})") from error
        for field in ("id", "label", "flag", "note"):
            if field not in record:
                raise ValueError(f"{path.name}:{number}: missing field {field!r}")
        label = str(record["label"]).strip().upper()
        flag = str(record["flag"]).strip().upper()
        if label not in LABELS:
            raise ValueError(f"{path.name}:{number}: invalid label {record['label']!r}")
        if flag not in FLAGS:
            raise ValueError(f"{path.name}:{number}: invalid flag {record['flag']!r}")
        if not str(record["note"]).strip():
            raise ValueError(f"{path.name}:{number}: empty note for {record['id']}")
        records.append({"id": str(record["id"]).strip(), "label": label, "flag": flag, "note": str(record["note"]).strip()})
    return records


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workbench-root", default="workbench")
    parser.add_argument("--output-root", default="outputs/llm_prelabel_v1")
    parser.add_argument("--model-id", default="claude-opus-5-thinking (Cursor agent)")
    parser.add_argument("--check-only", action="store_true")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    output_root = root / args.output_root
    manifest = json.loads((output_root / "batch_manifest.json").read_text(encoding="utf-8"))
    label_dir = output_root / "labels"

    errors: list[str] = []
    labels_by_project: dict[str, dict[str, dict]] = defaultdict(dict)
    for project, info in manifest["projects"].items():
        for name in info["batch_names"]:
            path = label_dir / f"{name}.jsonl"
            if not path.is_file():
                errors.append(f"missing label file: {path.relative_to(root)}")
                continue
            expected = [
                item["candidate_id"]
                for item in json.loads((output_root / "batches" / f"{name}.json").read_text(encoding="utf-8"))
            ]
            try:
                records = read_label_lines(path)
            except ValueError as error:
                errors.append(str(error))
                continue
            seen = [record["id"] for record in records]
            if seen != expected:
                missing = sorted(set(expected) - set(seen))
                extra = sorted(set(seen) - set(expected))
                if missing or extra:
                    errors.append(
                        f"{name}: candidate mismatch (missing={missing[:5]}, unexpected={extra[:5]})"
                    )
                else:
                    errors.append(f"{name}: labels are out of candidate order")
                continue
            for record in records:
                labels_by_project[project][record["id"]] = record

    if errors:
        print(json.dumps({"ready": False, "error_count": len(errors), "errors": errors[:40]}, ensure_ascii=False, indent=2))
        raise SystemExit(1)

    summary = {
        "protocol_version": PROTOCOL_VERSION,
        "label_source": "LLM_PRELABEL",
        "model_id": args.model_id,
        "human_verification_required": True,
        "formal_gold_standard": False,
        "projects": {},
        "totals": {"candidates": 0, "needs_human_review": 0, "labels": {}},
    }
    total_labels: Counter[str] = Counter()
    written = []
    for project, info in manifest["projects"].items():
        source = root / info["source"]
        if sha256_file(source) != info["source_sha256"]:
            raise ValueError(f"{project}: blind master changed after batches were prepared")
        records = json.loads(source.read_text(encoding="utf-8"))
        project_labels = labels_by_project[project]
        if set(project_labels) != {item["candidate_id"] for item in records}:
            raise ValueError(f"{project}: pre-label coverage does not match the blind master")

        merged = []
        label_counts: Counter[str] = Counter()
        stratum_counts: dict[str, Counter[str]] = defaultdict(Counter)
        needs_review = 0
        for record in records:
            entry = project_labels[record["candidate_id"]]
            label_counts[entry["label"]] += 1
            stratum_counts[record["boundary_stratum"]][entry["label"]] += 1
            needs_review += entry["flag"] == "NEEDS_HUMAN_REVIEW"
            merged.append(
                {
                    **{key: value for key, value in record.items() if key not in {"human_label", "review_status", "review_notes"}},
                    "label_source": "LLM_PRELABEL",
                    "model_id": args.model_id,
                    "prelabel": entry["label"],
                    "prelabel_flag": entry["flag"],
                    "prelabel_notes": entry["note"],
                    "human_verified": False,
                    "human_label": "",
                    "review_status": "LLM_PRELABELED",
                }
            )
        total_labels.update(label_counts)
        summary["projects"][project] = {
            "candidate_count": len(merged),
            "labels": dict(sorted(label_counts.items())),
            "needs_human_review": needs_review,
            "labels_by_stratum": {
                stratum: dict(sorted(counts.items())) for stratum, counts in sorted(stratum_counts.items())
            },
        }
        summary["totals"]["candidates"] += len(merged)
        summary["totals"]["needs_human_review"] += needs_review

        target = output_root / f"boundary_llm_prelabel_{project}.json"
        if not args.check_only:
            target.write_text(json.dumps(merged, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
            written.append(str(target.relative_to(root)))

    summary["totals"]["labels"] = dict(sorted(total_labels.items()))
    if not args.check_only:
        for project in manifest["projects"]:
            path = output_root / f"boundary_llm_prelabel_{project}.json"
            summary["projects"][project]["output_sha256"] = sha256_file(path)
        (output_root / "prelabel_summary.json").write_text(
            json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )
    print(json.dumps({"ready": True, "written": written, "totals": summary["totals"]}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
