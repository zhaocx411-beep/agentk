from __future__ import annotations

import argparse
import json
import re
import sys
from collections import Counter
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from seac_chunking.units import document_units


APPROVED = {"APPROVED", "KEEP", "VERIFIED"}
REJECTED = {"REJECTED", "DELETE"}
PRIVACY_CLEARED = {"CLEARED", "NO_RISK", "ANONYMIZED"}


def normalize(text: str) -> str:
    return re.sub(r"\s+", "", str(text or ""))


def evidence_lookup(documents: list[dict]) -> tuple[set[str], dict[str, str]]:
    blocks = set()
    units = {}
    for document in documents:
        for block in document["blocks"]:
            blocks.add(block["block_id"])
        for unit in document_units(document):
            units[unit.unit_id] = unit.text
    return blocks, units


def audit_questions(documents: list[dict], questions: list[dict]) -> tuple[list[dict], dict]:
    blocks, units = evidence_lookup(documents)
    approved = []
    issues = []
    statuses = Counter()
    for item in questions:
        qa_id = item.get("qa_id", "<unknown>")
        status = str(item.get("approval_status") or item.get("audit_status") or "PENDING").strip().upper()
        statuses[status] += 1
        if status in REJECTED or status == "PENDING":
            continue
        if status not in APPROVED:
            issues.append(f"{qa_id}: unsupported approval_status={status}")
            continue
        item_issues = []
        if not str(item.get("question", "")).strip():
            item_issues.append("missing question")
        if not str(item.get("reference_answer", "")).strip():
            item_issues.append("missing reference_answer")
        block_id = item.get("gold_evidence_id", "")
        unit_id = item.get("gold_evidence_unit_id", "")
        if block_id not in blocks:
            item_issues.append("missing gold_evidence_id in corpus")
        if not unit_id or unit_id not in units:
            item_issues.append("missing gold_evidence_unit_id in corpus")
        elif normalize(item.get("gold_evidence_span")) not in normalize(units[unit_id]):
            item_issues.append("gold_evidence_span is not contained in gold unit")
        privacy_status = str(item.get("privacy_review_status", "")).strip().upper()
        if privacy_status not in PRIVACY_CLEARED:
            item_issues.append("privacy review is not cleared")
        if item_issues:
            issues.append(f'{qa_id}: {", ".join(item_issues)}')
            continue
        finalized = dict(item)
        finalized["audit_status"] = "APPROVED"
        finalized["review_status"] = "APPROVED"
        approved.append(finalized)
    summary = {
        "protocol_version": "industrial-seac-qa-finalization-v1.0",
        "input_count": len(questions),
        "approval_status_counts": dict(sorted(statuses.items())),
        "approved_count": len(approved),
        "issue_count": len(issues),
        "issues": issues,
        "formal_ready": bool(approved) and not issues and statuses.get("PENDING", 0) == 0,
    }
    return approved, summary


def main() -> None:
    parser = argparse.ArgumentParser(description="Validate reviewed QA records and emit formal approved QA.")
    parser.add_argument("--corpus", required=True)
    parser.add_argument("--qa-review", required=True)
    parser.add_argument("--approved-output", required=True)
    parser.add_argument("--summary-output", required=True)
    parser.add_argument("--require-ready", action="store_true")
    args = parser.parse_args()

    documents = json.loads(Path(args.corpus).read_text(encoding="utf-8"))
    questions = json.loads(Path(args.qa_review).read_text(encoding="utf-8"))
    approved, summary = audit_questions(documents, questions)
    Path(args.approved_output).write_text(
        json.dumps(approved, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    Path(args.summary_output).write_text(
        json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    print(json.dumps({key: value for key, value in summary.items() if key != "issues"}, ensure_ascii=False, indent=2))
    if args.require_ready and not summary["formal_ready"]:
        raise ValueError("QA review is not formal-ready; inspect the summary issues")


if __name__ == "__main__":
    main()
