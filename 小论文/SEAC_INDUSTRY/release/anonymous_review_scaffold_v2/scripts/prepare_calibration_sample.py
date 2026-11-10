"""Select a frozen, development-only P01 boundary calibration sample."""

from __future__ import annotations

import argparse
import hashlib
import json
from collections import Counter, defaultdict
from pathlib import Path


SEED = 20260804
QUOTAS = {
    ("heading_transition", "BREAK"): 4,
    ("heading_transition", "NO_BREAK"): 4,
    ("heading_transition", "NOISE"): 4,
    ("paragraph_transition", "BREAK"): 1,
    ("paragraph_transition", "NO_BREAK"): 5,
    ("paragraph_transition", "NOISE"): 2,
    ("table_boundary", "BREAK"): 4,
    ("table_boundary", "NO_BREAK"): 4,
    ("table_boundary", "NOISE"): 2,
}
DISAGREEMENT_QUOTAS = {
    ("heading_transition", "BREAK"): 2,
    ("heading_transition", "NO_BREAK"): 1,
    ("heading_transition", "NOISE"): 1,
    ("paragraph_transition", "BREAK"): 1,
    ("table_boundary", "NOISE"): 1,
}


def stable_key(seed: int, value: str) -> str:
    return hashlib.sha256(f"{seed}:{value}".encode("utf-8")).hexdigest()


def balanced_pick(rows: list[dict], count: int, seed: int, used: set[str]) -> list[dict]:
    by_document = defaultdict(list)
    for row in rows:
        if row["candidate_id"] not in used:
            by_document[row["source_document"]].append(row)
    for document_rows in by_document.values():
        document_rows.sort(key=lambda row: stable_key(seed, row["candidate_id"]))
    documents = sorted(by_document, key=lambda name: stable_key(seed + 1, name))
    selected = []
    offsets = Counter()
    while len(selected) < count:
        progressed = False
        for document in documents:
            offset = offsets[document]
            if offset >= len(by_document[document]):
                continue
            row = by_document[document][offset]
            offsets[document] += 1
            selected.append(row)
            used.add(row["candidate_id"])
            progressed = True
            if len(selected) == count:
                break
        if not progressed:
            break
    if len(selected) != count:
        raise ValueError(f"Unable to select {count} calibration rows; found {len(selected)}")
    return selected


def select_calibration(rows: list[dict], seed: int = SEED) -> list[dict]:
    selected = []
    used: set[str] = set()
    for index, (stratum, label) in enumerate(QUOTAS):
        pool = [
            row
            for row in rows
            if row.get("boundary_type") == stratum and row.get("human_label") == label
        ]
        disagreement_count = DISAGREEMENT_QUOTAS.get((stratum, label), 0)
        disagreements = [
            row
            for row in pool
            if row.get("first_annotator_label") != row.get("second_annotator_label")
        ]
        agreements = [
            row
            for row in pool
            if row.get("first_annotator_label") == row.get("second_annotator_label")
        ]
        selected.extend(balanced_pick(disagreements, disagreement_count, seed + index * 7, used))
        selected.extend(
            balanced_pick(
                agreements,
                QUOTAS[(stratum, label)] - disagreement_count,
                seed + index * 7 + 3,
                used,
            )
        )
    selected.sort(key=lambda row: stable_key(seed + 1000, row["candidate_id"]))
    output = []
    for index, row in enumerate(selected, 1):
        copied = dict(row)
        copied["calibration_id"] = f"CAL{index:03d}"
        output.append(copied)
    return output


def main() -> None:
    parser = argparse.ArgumentParser(description="Select the private P01 calibration exercise cases.")
    parser.add_argument(
        "--input",
        type=Path,
        default=Path("workbench/P01/candidate_breakpoints_adjudicated.json"),
    )
    parser.add_argument(
        "--output",
        type=Path,
        default=Path("workbench/P01/calibration_sample_v1.json"),
    )
    parser.add_argument("--seed", type=int, default=SEED)
    args = parser.parse_args()
    if args.output.exists():
        raise FileExistsError("Calibration sample already exists; choose a new versioned output path")
    rows = json.loads(args.input.read_text(encoding="utf-8"))
    sample = select_calibration(rows, args.seed)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(sample, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    distribution = Counter((item["boundary_type"], item["human_label"]) for item in sample)
    report = {
        "protocol_version": "industrial-seac-boundary-calibration-v1.0",
        "seed": args.seed,
        "case_count": len(sample),
        "development_project_only": True,
        "formal_test_labels": False,
        "adjudicated_disagreement_count": sum(
            item.get("first_annotator_label") != item.get("second_annotator_label")
            for item in sample
        ),
        "distribution": {
            f"{key[0]}::{key[1]}": value for key, value in sorted(distribution.items())
        },
        "output": str(args.output),
        "output_sha256": hashlib.sha256(args.output.read_bytes()).hexdigest(),
    }
    print(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
