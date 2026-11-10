"""Verify supplementary QA drafts written to close per-project question shortfalls.

A supplementary draft is accepted only if the corpus supports it: the declared
evidence unit exists, the span occurs in that unit, the span is distinctive
enough that its equivalence set stays within the ceiling, the answer is grounded
in the span, and neither the question nor the evidence duplicates an existing
candidate. Accepted drafts remain `PENDING` for human approval.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections import Counter
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parent))

from merge_qa_readjudication import answer_grounding
from resolve_qa_evidence_equivalents import corpus_units, own_content

from seac_chunking.units import document_units

PROTOCOL_VERSION = "industrial-seac-qa-supplement-v1.0"
USABLE_STATES = {"APPROVED_SINGLE_ROUND", "CANDIDATE_MULTI_EVIDENCE", "CANDIDATE_REWRITE"}
REQUIRED = ("question", "reference_answer", "question_type", "source_document", "gold_evidence_span")


UNINFORMATIVE_HEADERS = {"主档", "明细档", "备注", "", "-"}


def normalize(text: str) -> str:
    return re.sub(r"\s+", "", text or "")


def header_reliability(unit_text: str) -> dict:
    """Judge whether a table row's header can justify the roles an answer asserts.

    A grounded answer can still misattribute a cell: character-level grounding
    passes when the answer names a column role the header never states. Merged or
    placeholder header cells are where that happens, so they are surfaced for the
    human reviewer instead of being silently accepted. Prose units carry no column
    roles to misattribute and are therefore out of scope.
    """

    if "数据行:" not in unit_text:
        return {"status": "NOT_TABLE", "labels": []}
    match = re.search(r"表头:(.*?)\n数据行:", unit_text, flags=re.S)
    if not match:
        return {"status": "NO_HEADER", "labels": []}
    labels = [" ".join(cell.split()) for cell in match.group(1).split("|")]
    informative = [label for label in labels if label not in UNINFORMATIVE_HEADERS]
    duplicated = sorted({label for label in informative if informative.count(label) > 1})
    if not informative:
        return {"status": "UNINFORMATIVE_HEADER", "labels": labels}
    if duplicated:
        return {"status": "DUPLICATE_HEADER_LABELS", "labels": labels, "duplicated": duplicated}
    return {"status": "RELIABLE", "labels": labels}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workbench-root", default="workbench")
    parser.add_argument("--readjudication-root", default="outputs/qa_readjudication_v1")
    parser.add_argument("--supplement-root", default="outputs/qa_supplement_v1")
    parser.add_argument("--projects", nargs="*", default=["P07", "P08", "P09"])
    parser.add_argument("--max-occurrences", type=int, default=12)
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    supplement_root = root / args.supplement_root
    supplement_root.mkdir(parents=True, exist_ok=True)

    report = {"protocol_version": PROTOCOL_VERSION, "projects": {}, "totals": Counter()}
    for project in args.projects:
        drafts_path = supplement_root / "drafts" / f"{project}.jsonl"
        if not drafts_path.is_file():
            raise SystemExit(f"missing supplementary drafts: {drafts_path.relative_to(root)}")
        existing = json.loads(
            (root / args.readjudication_root / f"qa_candidates_{project}.json").read_text(encoding="utf-8")
        )
        used_units = {
            unit_id
            for item in existing
            if item["candidate_state"] in USABLE_STATES
            for unit_id in (item.get("gold_evidence_unit_ids") or [item.get("gold_evidence_unit_id", "")])
        }
        used_questions = {normalize(item["question"]) for item in existing if item["candidate_state"] in USABLE_STATES}
        corpus = json.loads((root / args.workbench_root / project / "corpus.json").read_text(encoding="utf-8"))
        units = corpus_units(corpus)
        by_id = {unit["unit_id"]: unit for unit in units}
        unit_texts = {unit.unit_id: unit.text for document in corpus for unit in document_units(document)}

        accepted = []
        rejected = []
        for number, line in enumerate(drafts_path.read_text(encoding="utf-8").splitlines(), 1):
            stripped = line.strip()
            if not stripped:
                continue
            draft = json.loads(stripped)
            missing = [field for field in REQUIRED if not str(draft.get(field, "")).strip()]
            if missing:
                rejected.append({"line": number, "reasons": [f"missing:{field}" for field in missing]})
                continue

            span = own_content(draft["gold_evidence_span"])
            matches = [unit for unit in units if span and span in unit["normalized"]]
            declared = draft.get("gold_evidence_unit_id", "")
            reasons = []
            if not matches:
                reasons.append("span_not_in_corpus")
            if len(matches) > args.max_occurrences:
                reasons.append("span_not_distinctive")
            if declared and declared not in by_id:
                reasons.append("declared_unit_missing")
            elif declared and declared not in {unit["unit_id"] for unit in matches}:
                reasons.append("span_not_in_declared_unit")
            grounding = answer_grounding(draft["reference_answer"], span)
            if grounding["mode"] == "NOT_GROUNDED":
                reasons.append(f'answer_not_grounded:{",".join(grounding["ungrounded_segments"][:3])}')
            if normalize(draft["question"]) in used_questions:
                reasons.append("duplicate_question")
            if matches and {unit["unit_id"] for unit in matches} <= used_units:
                reasons.append("evidence_already_used")
            if reasons:
                rejected.append({"line": number, "question": draft["question"], "reasons": reasons})
                continue

            header = header_reliability(unit_texts.get(matches[0]["unit_id"], ""))
            accepted.append(
                {
                    "question": draft["question"],
                    "header_reliability": header["status"],
                    "header_labels": header["labels"],
                    "needs_role_check": header["status"] not in {"RELIABLE", "NOT_TABLE"},
                    "reference_answer": draft["reference_answer"],
                    "question_type": draft["question_type"],
                    "source_document": draft["source_document"],
                    "source_section": draft.get("source_section", ""),
                    "gold_evidence_id": matches[0]["block_id"],
                    "gold_evidence_ids": sorted({unit["block_id"] for unit in matches}),
                    "gold_evidence_unit_id": matches[0]["unit_id"],
                    "gold_evidence_unit_ids": [unit["unit_id"] for unit in matches],
                    "gold_evidence_span": draft["gold_evidence_span"],
                    "gold_evidence_spans": [draft["gold_evidence_span"]],
                    "project_id": project,
                    "draft_origin": "llm_authored_supplement_for_human_review",
                    "answer_grounding": grounding["mode"],
                    "evidence_equivalence": {
                        "status": "EQUIVALENT_SET" if len(matches) > 1 else "UNIQUE",
                        "occurrence_count": len(matches),
                        "unit_ids": [unit["unit_id"] for unit in matches],
                        "documents": sorted({unit["document"] for unit in matches}),
                    },
                    "candidate_state": "CANDIDATE_SUPPLEMENT",
                    "approval_status": "PENDING",
                    "privacy_review_status": "PENDING",
                    "human_approved": False,
                }
            )
            used_questions.add(normalize(draft["question"]))

        for index, item in enumerate(accepted, 1):
            item["qa_id"] = f"QS{index:03d}"
            item["qa_key"] = f'{project}-{item["qa_id"]}'
        (supplement_root / f"qa_supplement_{project}.json").write_text(
            json.dumps(accepted, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )
        role_check = [item["qa_key"] for item in accepted if item["needs_role_check"]]
        report["projects"][project] = {
            "accepted": len(accepted),
            "rejected": len(rejected),
            "rejections": rejected,
            "needs_role_check": role_check,
            "existing_usable": sum(1 for item in existing if item["candidate_state"] in USABLE_STATES),
        }
        report["totals"]["accepted"] += len(accepted)
        report["totals"]["rejected"] += len(rejected)
        report["totals"]["needs_role_check"] += len(role_check)

    report["totals"] = dict(sorted(report["totals"].items()))
    (supplement_root / "supplement_report.json").write_text(
        json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
