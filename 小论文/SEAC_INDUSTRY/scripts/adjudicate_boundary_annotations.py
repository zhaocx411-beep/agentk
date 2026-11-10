from __future__ import annotations

import argparse
import json
from collections import Counter
from pathlib import Path


LABELS = {"BREAK", "NO_BREAK", "NOISE"}


def cohen_kappa(labels_a: list[str], labels_b: list[str]) -> float:
    if len(labels_a) != len(labels_b):
        raise ValueError("Annotator label lists have different lengths")
    if not labels_a:
        return 0.0
    observed = sum(left == right for left, right in zip(labels_a, labels_b)) / len(labels_a)
    counts_a = Counter(labels_a)
    counts_b = Counter(labels_b)
    expected = sum(
        counts_a[label] / len(labels_a) * counts_b[label] / len(labels_b)
        for label in LABELS
    )
    return (observed - expected) / (1 - expected) if expected < 1 else 1.0


def keyed(records: list[dict], slot: str) -> dict[str, dict]:
    output = {}
    for item in records:
        candidate_id = item.get("candidate_id", "")
        if not candidate_id or candidate_id in output:
            raise ValueError(f"{slot} has a missing or duplicate candidate_id: {candidate_id!r}")
        output[candidate_id] = item
    return output


def merge_annotations(records_a: list[dict], records_b: list[dict]) -> tuple[list[dict], dict]:
    by_a = keyed(records_a, "annotator A")
    by_b = keyed(records_b, "annotator B")
    if set(by_a) != set(by_b):
        missing_a = sorted(set(by_b) - set(by_a))
        missing_b = sorted(set(by_a) - set(by_b))
        raise ValueError(f"Candidate sets differ; missing in A={missing_a[:5]}, missing in B={missing_b[:5]}")

    invalid = []
    incomplete = []
    merged = []
    complete_a = []
    complete_b = []
    for candidate_id in sorted(by_a):
        item_a, item_b = by_a[candidate_id], by_b[candidate_id]
        label_a = str(item_a.get("human_label", "")).strip().upper()
        label_b = str(item_b.get("human_label", "")).strip().upper()
        if label_a and label_a not in LABELS:
            invalid.append(f"A:{candidate_id}:{label_a}")
        if label_b and label_b not in LABELS:
            invalid.append(f"B:{candidate_id}:{label_b}")
        if label_a not in LABELS or label_b not in LABELS:
            incomplete.append(candidate_id)
        else:
            complete_a.append(label_a)
            complete_b.append(label_b)

        base = {
            key: value
            for key, value in item_a.items()
            if key not in {"human_label", "review_status", "review_notes", "annotator_slot"}
        }
        agreement = label_a in LABELS and label_a == label_b
        base.update(
            {
                "first_annotator_label": label_a,
                "second_annotator_label": label_b,
                "first_annotator_notes": item_a.get("review_notes", ""),
                "second_annotator_notes": item_b.get("review_notes", ""),
                "agreement": agreement,
                "human_label": label_a if agreement else "",
                "adjudication_label": "",
                "adjudication_notes": "",
                "review_status": "AGREED" if agreement else "NEEDS_ADJUDICATION",
            }
        )
        merged.append(base)

    if invalid:
        raise ValueError("Invalid labels: " + ", ".join(invalid[:10]))
    complete_pairs = len(complete_a)
    agreements = sum(left == right for left, right in zip(complete_a, complete_b))
    summary = {
        "protocol_version": "industrial-seac-boundary-adjudication-v1.0",
        "candidate_count": len(merged),
        "complete_pair_count": complete_pairs,
        "incomplete_count": len(incomplete),
        "incomplete_candidate_ids": incomplete,
        "agreement_count": agreements,
        "disagreement_count": complete_pairs - agreements,
        "raw_agreement": agreements / complete_pairs if complete_pairs else 0.0,
        "cohen_kappa": cohen_kappa(complete_a, complete_b) if complete_pairs else 0.0,
        "annotator_a_labels": dict(sorted(Counter(complete_a).items())),
        "annotator_b_labels": dict(sorted(Counter(complete_b).items())),
    }
    return merged, summary


def finalize(merged: list[dict]) -> list[dict]:
    output = []
    unresolved = []
    for item in merged:
        final = dict(item)
        if not final.get("agreement"):
            adjudicated = str(final.get("adjudication_label", "")).strip().upper()
            if adjudicated not in LABELS:
                unresolved.append(final.get("candidate_id", ""))
                continue
            final["human_label"] = adjudicated
            final["review_status"] = "ADJUDICATED"
        elif final.get("human_label") not in LABELS:
            unresolved.append(final.get("candidate_id", ""))
            continue
        output.append(final)
    if unresolved:
        raise ValueError(
            f"{len(unresolved)} candidates are unresolved (examples: {', '.join(unresolved[:10])})"
        )
    return output


def main() -> None:
    parser = argparse.ArgumentParser(description="Merge double-blind boundary annotations and prepare adjudication.")
    parser.add_argument("--annotator-a", required=True)
    parser.add_argument("--annotator-b", required=True)
    parser.add_argument("--merged-output", required=True)
    parser.add_argument("--summary-output", required=True)
    parser.add_argument("--final-output")
    args = parser.parse_args()

    records_a = json.loads(Path(args.annotator_a).read_text(encoding="utf-8"))
    records_b = json.loads(Path(args.annotator_b).read_text(encoding="utf-8"))
    merged, summary = merge_annotations(records_a, records_b)
    merged_path = Path(args.merged_output)
    summary_path = Path(args.summary_output)
    merged_path.write_text(json.dumps(merged, ensure_ascii=False, indent=2), encoding="utf-8")
    summary_path.write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    if args.final_output:
        final_records = finalize(merged)
        Path(args.final_output).write_text(
            json.dumps(final_records, ensure_ascii=False, indent=2), encoding="utf-8"
        )


if __name__ == "__main__":
    main()
