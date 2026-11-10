"""Validate and merge QA content pre-review verdicts.

The merged records are advisory input for the human QA reviewer. Privacy status
stays `PENDING` for every record: privacy release requires the data owner's
authorization and is never produced by this pipeline.
"""

from __future__ import annotations

import argparse
import json
from collections import Counter
from pathlib import Path

PROTOCOL_VERSION = "industrial-seac-qa-prereview-v1.0"
VERDICTS = {"APPROVED", "REJECTED", "NEEDS_REWRITE"}
DIFFICULTIES = {"Easy", "Medium", "Hard"}
PRIVACY_FLAGS = {
    "PERSON_NAME_OR_CONTACT",
    "INTERNAL_NETWORK_OR_URL",
    "SECRET_OR_CREDENTIAL",
    "CUSTOMER_OR_VENDOR_IDENTITY",
    "SECURITY_ARCHITECTURE",
    "PROPRIETARY_BUSINESS_DATA",
}


def read_verdicts(path: Path) -> list[dict]:
    records = []
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        stripped = line.strip()
        if not stripped:
            continue
        record = json.loads(stripped)
        for field in ("id", "content_verdict", "difficulty", "suggested_privacy_flags", "note"):
            if field not in record:
                raise ValueError(f"{path.name}:{number}: missing field {field!r}")
        if record["content_verdict"] not in VERDICTS:
            raise ValueError(f"{path.name}:{number}: invalid verdict {record['content_verdict']!r}")
        if record["difficulty"] not in DIFFICULTIES:
            raise ValueError(f"{path.name}:{number}: invalid difficulty {record['difficulty']!r}")
        if not isinstance(record["suggested_privacy_flags"], list):
            raise ValueError(f"{path.name}:{number}: suggested_privacy_flags must be a list")
        unknown = set(record["suggested_privacy_flags"]) - PRIVACY_FLAGS
        if unknown:
            raise ValueError(f"{path.name}:{number}: unknown privacy flags {sorted(unknown)}")
        if not str(record["note"]).strip():
            raise ValueError(f"{path.name}:{number}: empty note for {record['id']}")
        records.append(record)
    return records


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--prereview-root", default="outputs/qa_prereview_v1")
    parser.add_argument("--manifest", default="configs/formal_run_manifest.json")
    parser.add_argument("--model-id", default="claude-opus-5-thinking (Cursor agent)")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    prereview_root = root / args.prereview_root
    manifest = json.loads((prereview_root / "prereview_manifest.json").read_text(encoding="utf-8"))
    minimums = {
        project["project_id"]: project.get("minimum_qa_count", 0)
        for project in json.loads((root / args.manifest).read_text(encoding="utf-8"))["projects"]
    }

    summary = {
        "protocol_version": PROTOCOL_VERSION,
        "review_source": "LLM_CONTENT_PREREVIEW",
        "model_id": args.model_id,
        "privacy_decisions_included": False,
        "formal_gold_standard": False,
        "projects": {},
        "totals": {"records": 0, "verdicts": Counter(), "difficulty": Counter(), "flagged_privacy": 0},
    }
    merged_all = []
    for project, info in manifest["projects"].items():
        batch = json.loads((prereview_root / "batches" / f"{project}.json").read_text(encoding="utf-8"))
        verdicts = read_verdicts(prereview_root / "labels" / f"{project}.jsonl")
        expected = [item["id"] for item in batch]
        if [item["id"] for item in verdicts] != expected:
            raise ValueError(f"{project}: verdict ids or order do not match the batch")

        by_id = {item["id"]: item for item in verdicts}
        merged = []
        for item in batch:
            verdict = by_id[item["id"]]
            merged.append(
                {
                    **item,
                    "content_verdict": verdict["content_verdict"],
                    "suggested_difficulty": verdict["difficulty"],
                    "suggested_privacy_flags": verdict["suggested_privacy_flags"],
                    "prereview_notes": verdict["note"],
                    "review_source": "LLM_CONTENT_PREREVIEW",
                    "approval_status": "PENDING",
                    "privacy_review_status": "PENDING",
                }
            )
        (prereview_root / f"qa_content_prereview_{project}.json").write_text(
            json.dumps(merged, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )
        merged_all.extend(merged)

        verdict_counts = Counter(item["content_verdict"] for item in merged)
        difficulty_counts = Counter(item["suggested_difficulty"] for item in merged)
        flagged = sum(1 for item in merged if item["suggested_privacy_flags"])
        approved = verdict_counts["APPROVED"]
        recoverable = approved + verdict_counts["NEEDS_REWRITE"]
        summary["projects"][project] = {
            "record_count": len(merged),
            "verdicts": dict(sorted(verdict_counts.items())),
            "difficulty": dict(sorted(difficulty_counts.items())),
            "privacy_flagged_records": flagged,
            "minimum_qa_count": minimums.get(project, 0),
            "meets_minimum_with_approved_only": approved >= minimums.get(project, 0),
            "meets_minimum_after_rewrite": recoverable >= minimums.get(project, 0),
        }
        summary["totals"]["records"] += len(merged)
        summary["totals"]["verdicts"].update(verdict_counts)
        summary["totals"]["difficulty"].update(difficulty_counts)
        summary["totals"]["flagged_privacy"] += flagged

    summary["totals"]["verdicts"] = dict(sorted(summary["totals"]["verdicts"].items()))
    summary["totals"]["difficulty"] = dict(sorted(summary["totals"]["difficulty"].items()))
    (prereview_root / "qa_prereview_summary.json").write_text(
        json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )

    lines = [
        "# QA 内容预审汇总",
        "",
        f'- 记录总数：{summary["totals"]["records"]}',
        f'- 判定分布：{summary["totals"]["verdicts"]}',
        f'- 建议隐私标记记录数：{summary["totals"]["flagged_privacy"]}',
        "- 隐私放行状态全部保持 PENDING，需由获授权的人工审核员决定。",
        "",
        "| 项目 | 记录 | APPROVED | NEEDS_REWRITE | REJECTED | 最低题数 | 仅用APPROVED达标 | 改写后可达标 |",
        "|---|---:|---:|---:|---:|---:|---|---|",
    ]
    for project, info in summary["projects"].items():
        verdicts = info["verdicts"]
        lines.append(
            f'| {project} | {info["record_count"]} | {verdicts.get("APPROVED", 0)} | '
            f'{verdicts.get("NEEDS_REWRITE", 0)} | {verdicts.get("REJECTED", 0)} | '
            f'{info["minimum_qa_count"]} | {"是" if info["meets_minimum_with_approved_only"] else "否"} | '
            f'{"是" if info["meets_minimum_after_rewrite"] else "否"} |'
        )
    (prereview_root / "qa_prereview_summary.md").write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(json.dumps({"totals": summary["totals"], "projects": summary["projects"]}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
