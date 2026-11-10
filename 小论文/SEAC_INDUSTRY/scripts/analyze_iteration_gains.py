import json
from pathlib import Path


QA_PATH = Path("workbench/P01/qa_review_set.json")
BASE_DIR = Path("experiments/P01_bge_m3")
LATEST_DIR = Path("experiments/P01_bge_m3_proposed_v5")
OUTPUT = Path("experiments/P01_bge_m3_iteration_report/per_question_gains.json")


def ranks(path):
    return {item["qa_id"]: item["rank"] for item in json.loads(path.read_text(encoding="utf-8"))}


def main():
    questions = {item["qa_id"]: item for item in json.loads(QA_PATH.read_text(encoding="utf-8"))}
    methods = {
        method: ranks(BASE_DIR / f"{method}_retrieval_details.json")
        for method in ("fixed", "recursive", "structure", "semantic", "proposed")
    }
    methods["proposed_latest"] = ranks(LATEST_DIR / "proposed_retrieval_details.json")
    rows = []
    for qa_id, item in questions.items():
        baseline_ranks = [methods[method][qa_id] for method in ("fixed", "recursive", "structure", "semantic")]
        best_baseline = min(rank for rank in baseline_ranks if rank is not None)
        v1 = methods["proposed"][qa_id]
        latest = methods["proposed_latest"][qa_id]
        rows.append(
            {
                "qa_id": qa_id,
                "question_type": item["question_type"],
                "question": item["question"],
                "best_baseline_rank": best_baseline,
                "proposed_v1_rank": v1,
                "proposed_latest_rank": latest,
                "latest_vs_v1_gain": (v1 - latest) if v1 is not None and latest is not None else None,
                "latest_vs_best_baseline_gain": best_baseline - latest if latest is not None else None,
            }
        )
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.write_text(json.dumps(rows, ensure_ascii=False, indent=2), encoding="utf-8")
    improved = sum(item["latest_vs_v1_gain"] > 0 for item in rows if item["latest_vs_v1_gain"] is not None)
    worsened = sum(item["latest_vs_v1_gain"] < 0 for item in rows if item["latest_vs_v1_gain"] is not None)
    print(json.dumps({"improved_vs_v1": improved, "worsened_vs_v1": worsened, "total": len(rows)}, ensure_ascii=False))


if __name__ == "__main__":
    main()
