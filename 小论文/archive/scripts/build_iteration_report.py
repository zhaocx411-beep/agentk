import csv
import json
from pathlib import Path


BASELINE_PATH = Path("experiments/P01_bge_m3/results.json")
V2_PATH = Path("experiments/P01_bge_m3_proposed_v2/results.json")
V3_PATH = Path("experiments/P01_bge_m3_proposed_v3/results.json")
V4_PATH = Path("experiments/P01_bge_m3_proposed_v4/results.json")
V5_PATH = Path("experiments/P01_bge_m3_proposed_v5/results.json")
OUTPUT_DIR = Path("experiments/P01_bge_m3_iteration_report")


def row(name, result):
    return {
        "method": name,
        "chunk_count": result["chunk_count"],
        "mean_chunk_tokens": result["mean_chunk_tokens"],
        "boundary_precision": result["boundary"]["precision"],
        "boundary_recall": result["boundary"]["recall"],
        "boundary_f1": result["boundary"]["f1"],
        "mrr": result["retrieval"]["mrr"],
        "hit_rate@1": result["retrieval"]["hit_rate@1"],
        "hit_rate@3": result["retrieval"]["hit_rate@3"],
        "hit_rate@5": result["retrieval"]["hit_rate@5"],
    }


def main():
    baseline = json.loads(BASELINE_PATH.read_text(encoding="utf-8"))
    v2 = json.loads(V2_PATH.read_text(encoding="utf-8"))["proposed"]
    v3 = json.loads(V3_PATH.read_text(encoding="utf-8"))["proposed"]
    v4 = json.loads(V4_PATH.read_text(encoding="utf-8"))["proposed"]
    v5 = json.loads(V5_PATH.read_text(encoding="utf-8"))["proposed"]
    rows = [row(name, result) for name, result in baseline.items() if name != "proposed"]
    rows.extend(
        [
            row("proposed_v1", baseline["proposed"]),
            row("proposed_v2_context_index", v2),
            row("proposed_v3_balanced", v3),
            row("proposed_v4_heading_anchored", v4),
            row("proposed_v5_structured_long_units", v5),
        ]
    )

    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    (OUTPUT_DIR / "iteration_results.json").write_text(json.dumps(rows, ensure_ascii=False, indent=2), encoding="utf-8")
    with (OUTPUT_DIR / "iteration_results.csv").open("w", encoding="utf-8-sig", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)
    print(json.dumps(rows, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
