"""Import the filled QA approval workbook into the formal per-project QA files.

This is the only path from a human decision to `qa_approved.json`. Nothing here
may invent, upgrade or infer an approval: the script reads the four reviewer
columns, refuses to write while any row is undecided, and re-derives every
evidence field from the corpus instead of trusting the workbook. The candidate
columns are compared against the generated workbook so an edited question or span
is reported rather than silently promoted.

Only records that are approved on content and cleared on privacy are written.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from collections import Counter
from pathlib import Path

from openpyxl import load_workbook

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
sys.path.insert(0, str(Path(__file__).resolve().parent))

from build_qa_approval_workbook import HEADERS, INPUT_START, USABLE_STATES, collect
from qa_question_rules import classify as classify_question
from resolve_qa_evidence_equivalents import corpus_units, own_content

PROTOCOL_VERSION = "industrial-seac-qa-import-v1.0"
APPROVAL_VALUES = {"APPROVED", "REJECTED"}
PRIVACY_CLEARED = {"NO_RISK", "ANONYMIZED", "CLEARED"}
PRIVACY_VALUES = PRIVACY_CLEARED | {"REJECTED"}
DIFFICULTIES = {"Easy", "Medium", "Hard"}
DEFAULT_PROJECTS = ["P02", "P03", "P04", "P05", "P06", "P07", "P08", "P09", "P10"]
# The phrase reviewers used for the one generator defect that dominates the
# rejections: the question names a document section instead of asking the fact.
# Counting the reason they wrote keeps the tally attributable to them.
SECTION_SCANNING_MARKER = "章节扫描"
# Reviewers wrote "同上" instead of repeating the reason on a consecutive row with
# the same verdict, so a continuation inherits the preceding rejection's reason.
CONTINUATION_MARKERS = ("同上", "同前")
# Reasons are written as "内容：… 隐私：…", so the content clause has to be isolated
# before its opening words can be read.
CONTENT_CLAUSE = re.compile(r"^\s*内容\s*[:：]\s*")


def cell_text(value) -> str:
    return "" if value is None else str(value).strip()


def comparable(value) -> str:
    """Collapse whitespace so a reviewer's incidental reformatting is not an edit."""

    return re.sub(r"\s+", "", cell_text(value))


def cites_section_scanning(notes: str, previous: dict | None) -> bool:
    """Report whether a rejection reason names the section-scanning defect.

    A reason that opens with a continuation marker refers back to the preceding
    rejection in the same sheet, so it inherits that row's classification instead
    of being read as an unrelated reason.
    """

    if SECTION_SCANNING_MARKER in notes:
        return True
    content = CONTENT_CLAUSE.sub("", notes)
    if content.startswith(CONTINUATION_MARKERS) and previous is not None:
        return bool(previous["section_scanning"])
    return False


def display(path: Path, root: Path) -> str:
    try:
        return str(path.relative_to(root))
    except ValueError:
        return str(path)


def candidate_records(root: Path, readjudication_root: Path, supplement_root: Path, project: str) -> dict[str, dict]:
    """Index every usable candidate of one project by its stable `qa_key`."""

    records = {}
    candidates = json.loads((readjudication_root / f"qa_candidates_{project}.json").read_text(encoding="utf-8"))
    for item in candidates:
        if item["candidate_state"] in USABLE_STATES:
            records[item["qa_key"]] = item
    supplement_path = supplement_root / f"qa_supplement_{project}.json"
    if supplement_path.is_file():
        for item in json.loads(supplement_path.read_text(encoding="utf-8")):
            records[item["qa_key"]] = item
    return records


def read_sheet(sheet, expected_rows: list[dict]) -> list[dict]:
    """Read one project sheet, checking layout and the immutability of candidate columns."""

    header = [cell_text(cell.value) for cell in sheet[1][: len(HEADERS)]]
    if header != HEADERS:
        raise ValueError(f"{sheet.title}: header row does not match the generated workbook")
    rows = list(sheet.iter_rows(min_row=2, max_col=len(HEADERS), values_only=True))
    rows = [row for row in rows if any(cell_text(value) for value in row)]
    if len(rows) != len(expected_rows):
        raise ValueError(f"{sheet.title}: {len(rows)} rows present, expected {len(expected_rows)}")

    parsed = []
    for number, (row, expected) in enumerate(zip(rows, expected_rows), 2):
        values = dict(zip(HEADERS, row))
        edits = [
            header
            for header in HEADERS[: INPUT_START - 1]
            if comparable(values[header]) != comparable(expected.get(header, ""))
        ]
        if edits:
            raise ValueError(f"{sheet.title}:{number}: candidate columns were edited: {', '.join(edits)}")
        parsed.append(
            {
                "row": number,
                "qa_key": cell_text(values["qa_key"]),
                "needs_role_check": comparable(values["needs_role_check"]) == "YES",
                "approval_status": cell_text(values["approval_status"]).upper(),
                "privacy_review_status": cell_text(values["privacy_review_status"]).upper(),
                "difficulty": cell_text(values["difficulty"]),
                "review_notes": cell_text(values["review_notes"]),
            }
        )
    return parsed


def validate_decision(decision: dict) -> list[str]:
    """Return why a reviewed row cannot be trusted, empty when the decision is usable."""

    problems = []
    approval = decision["approval_status"]
    privacy = decision["privacy_review_status"]
    if approval not in APPROVAL_VALUES:
        problems.append(f"invalid approval_status {approval!r}")
    if privacy not in PRIVACY_VALUES:
        problems.append(f"invalid privacy_review_status {privacy!r}")
    if approval == "APPROVED" and decision["difficulty"] not in DIFFICULTIES:
        problems.append(f"approved row needs a difficulty, found {decision['difficulty']!r}")
    if approval != "APPROVED" and decision["difficulty"]:
        problems.append("non-approved row carries a difficulty")
    if "REJECTED" in {approval, privacy} and not decision["review_notes"]:
        problems.append("rejection without a reason in review_notes")
    if privacy == "ANONYMIZED" and not decision["review_notes"]:
        problems.append("anonymized row must record the substitution rule in review_notes")
    if approval == "APPROVED" and decision["needs_role_check"] and not decision["review_notes"]:
        problems.append("row flagged needs_role_check must record the column-role confirmation")
    return problems


def evidence_fields(record: dict, units: list[dict], max_occurrences: int) -> tuple[dict, list[str]]:
    """Re-derive the evidence set from the corpus and report why it cannot be used."""

    span = own_content(record.get("gold_evidence_span", ""))
    declared = record.get("gold_evidence_unit_ids") or [record.get("gold_evidence_unit_id", "")]
    matches = [unit for unit in units if span and span in unit["normalized"]]
    unit_ids = [unit["unit_id"] for unit in matches]
    problems = []
    if not matches:
        problems.append("evidence span is absent from the corpus")
    if len(matches) > max_occurrences:
        problems.append(f"evidence span occurs {len(matches)} times, above the ceiling")
    missing = [unit_id for unit_id in declared if unit_id and unit_id not in unit_ids]
    if missing:
        problems.append(f'declared evidence units no longer carry the span: {", ".join(missing)}')
    return (
        {
            "gold_evidence_unit_id": unit_ids[0] if unit_ids else "",
            "gold_evidence_unit_ids": unit_ids,
            "gold_evidence_id": matches[0]["block_id"] if matches else "",
            "gold_evidence_ids": sorted({unit["block_id"] for unit in matches}),
            "gold_evidence_span": record.get("gold_evidence_span", ""),
            "gold_evidence_spans": [record.get("gold_evidence_span", "")],
            "evidence_occurrence_count": len(matches),
            "evidence_documents": sorted({unit["document"] for unit in matches}),
        },
        problems,
    )


def build_record(record: dict, decision: dict, evidence: dict, project: str) -> dict:
    """Assemble the formal QA record, keeping the provenance of how it was drafted."""

    return {
        "qa_id": record["qa_id"],
        "project_id": project,
        "question": record["question"],
        "reference_answer": record["reference_answer"],
        "question_type": record.get("question_type", ""),
        "source_document": record.get("source_document", ""),
        "source_section": record.get("source_section", ""),
        **evidence,
        "difficulty": decision["difficulty"],
        "review_status": "APPROVED",
        "audit_status": "APPROVED",
        "review_notes": decision["review_notes"],
        "privacy_review_status": decision["privacy_review_status"],
        "suggested_privacy_flags": record.get("suggested_privacy_flags", []),
        "human_approved": True,
        "evidence_model": "multi_evidence_any_hit",
        "candidate_state": record["candidate_state"],
        "draft_origin": record.get("draft_origin", "rule_generated_with_llm_prereview"),
        "answer_grounding": record.get("answer_grounding")
        or record.get("proposed_rewrite", {}).get("answer_grounding", ""),
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workbench-root", default="workbench")
    parser.add_argument("--readjudication-root", default="outputs/qa_readjudication_v1")
    parser.add_argument("--supplement-root", default="outputs/qa_supplement_v1")
    parser.add_argument("--approval-root", default="outputs/qa_approval_v1")
    parser.add_argument("--workbook", default="outputs/qa_approval_v1/SEAC_QA_approval.xlsx")
    parser.add_argument("--manifest", default="configs/formal_run_manifest.json")
    parser.add_argument("--projects", nargs="*", default=DEFAULT_PROJECTS)
    parser.add_argument("--max-occurrences", type=int, default=12)
    parser.add_argument("--write", action="store_true", help="write workbench/<project>/qa_approved.json")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    workbook_path = root / args.workbook
    readjudication_root = root / args.readjudication_root
    supplement_root = root / args.supplement_root
    approval_root = root / args.approval_root
    minimums = {
        project["project_id"]: project.get("minimum_qa_count", 0)
        for project in json.loads((root / args.manifest).read_text(encoding="utf-8"))["projects"]
    }

    expected = collect(root, readjudication_root, supplement_root, args.projects)
    workbook = load_workbook(workbook_path, data_only=True)
    report = {
        "protocol_version": PROTOCOL_VERSION,
        "workbook": display(workbook_path, root),
        "workbook_sha256": hashlib.sha256(workbook_path.read_bytes()).hexdigest(),
        "written": args.write,
        "projects": {},
        "totals": Counter(),
    }

    for project in args.projects:
        decisions = read_sheet(workbook[project], expected[project])
        records = candidate_records(root, readjudication_root, supplement_root, project)
        units = corpus_units(
            json.loads((root / args.workbench_root / project / "corpus.json").read_text(encoding="utf-8"))
        )

        approved = []
        pending = []
        problems = []
        rule_excluded = []
        rejected = []
        counts: Counter[str] = Counter()
        for decision in decisions:
            record = records[decision["qa_key"]]
            if not decision["approval_status"] and not decision["privacy_review_status"]:
                pending.append(decision["qa_key"])
                counts["PENDING"] += 1
                continue
            found = validate_decision(decision)
            if not decision["approval_status"] or not decision["privacy_review_status"]:
                found.append("row is half decided; both approval and privacy must be set")
            if found:
                problems.append({"qa_key": decision["qa_key"], "row": decision["row"], "problems": found})
                counts["INVALID"] += 1
                continue
            if decision["approval_status"] != "APPROVED" or decision["privacy_review_status"] not in PRIVACY_CLEARED:
                counts["REJECTED"] += 1
                rejected.append(
                    {
                        "qa_key": decision["qa_key"],
                        "reason": decision["review_notes"],
                        # Tallied from the reason the reviewer wrote, not inferred
                        # from the question, so the paper can attribute it.
                        "section_scanning": cites_section_scanning(
                            decision["review_notes"], rejected[-1] if rejected else None
                        ),
                    }
                )
                continue
            # §6.1 is applied mechanically after the human round. A row it fires on
            # is excluded without editing the reviewer's decision, so the workbook
            # keeps what the reviewer actually wrote.
            verdict = classify_question(record.get("question", ""))
            if verdict["excluded"]:
                rule_excluded.append(
                    {
                        "qa_key": decision["qa_key"],
                        "rule": verdict["rule"],
                        "reason": verdict["reason"],
                        "prefix": verdict["prefix"],
                        "workbook_decision": decision["approval_status"],
                    }
                )
                counts["RULE_EXCLUDED"] += 1
                continue
            evidence, evidence_problems = evidence_fields(record, units, args.max_occurrences)
            if evidence_problems:
                problems.append(
                    {"qa_key": decision["qa_key"], "row": decision["row"], "problems": evidence_problems}
                )
                counts["INVALID"] += 1
                continue
            approved.append(build_record(record, decision, evidence, project))
            counts["APPROVED"] += 1

        complete = not pending and not problems
        target = root / args.workbench_root / project / "qa_approved.json"
        if args.write and complete:
            target.write_text(json.dumps(approved, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        report["projects"][project] = {
            "candidate_count": len(decisions),
            "counts": dict(sorted(counts.items())),
            "approved_count": len(approved),
            "pending": pending,
            "problems": problems,
            "rule_excluded": rule_excluded,
            "rejected": rejected,
            "section_scanning_rejections": sum(item["section_scanning"] for item in rejected),
            "complete": complete,
            "minimum_qa_count": minimums.get(project, 0),
            "meets_minimum": len(approved) >= minimums.get(project, 0),
            "multi_evidence_records": sum(1 for item in approved if len(item["gold_evidence_unit_ids"]) > 1),
            "written": bool(args.write and complete),
            "target": display(target, root),
        }
        report["totals"].update(counts)
        report["totals"]["projects_complete"] += complete

    report["totals"] = dict(sorted(report["totals"].items()))
    approval_root.mkdir(parents=True, exist_ok=True)
    (approval_root / "import_report.json").write_text(
        json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    blocked = [project for project, info in report["projects"].items() if not info["complete"]]
    # A mechanically excluded row overrides a human approval, so it is surfaced in
    # the summary rather than left for someone to find in the report file.
    rule_excluded = {
        project: [item["qa_key"] for item in info["rule_excluded"]]
        for project, info in report["projects"].items()
        if info["rule_excluded"]
    }
    print(
        json.dumps(
            {
                "totals": report["totals"],
                "blocked_projects": blocked,
                "rule_excluded": rule_excluded,
                "below_minimum": [
                    project
                    for project, info in report["projects"].items()
                    if not info["meets_minimum"]
                ],
                "written": [project for project, info in report["projects"].items() if info["written"]],
            },
            ensure_ascii=False,
            indent=2,
        )
    )
    if blocked and args.write:
        raise SystemExit(
            f'Cannot promote {", ".join(blocked)}: the workbook has undecided or invalid rows; '
            "see outputs/qa_approval_v1/import_report.json"
        )


if __name__ == "__main__":
    main()
