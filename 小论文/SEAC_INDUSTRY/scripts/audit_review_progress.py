"""Read-only progress audit for SEAC boundary and QA review workbooks."""

from __future__ import annotations

import argparse
import json
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

from openpyxl import load_workbook

try:
    from import_review_workbooks import (
        APPROVAL_STATUSES,
        BOUNDARY_HEADERS,
        BOUNDARY_PROJECTS,
        BOUNDARY_REVIEW_STATUSES,
        LABELS,
        PRIVACY_STATUSES,
        QA_HEADERS,
        QA_PROJECTS,
        read_json,
        read_sheet_rows,
        rows_as_records,
        sha256_file,
        split_privacy_flags,
        text,
        validate_boundary_rows,
        validate_qa_rows,
    )
except ModuleNotFoundError:  # Support importing as scripts.audit_review_progress in tests.
    from scripts.import_review_workbooks import (
        APPROVAL_STATUSES,
        BOUNDARY_HEADERS,
        BOUNDARY_PROJECTS,
        BOUNDARY_REVIEW_STATUSES,
        LABELS,
        PRIVACY_STATUSES,
        QA_HEADERS,
        QA_PROJECTS,
        read_json,
        read_sheet_rows,
        rows_as_records,
        sha256_file,
        split_privacy_flags,
        text,
        validate_boundary_rows,
        validate_qa_rows,
    )


def boundary_row_state(row: dict, source: dict, project: str, slot: str) -> tuple[str, str]:
    candidate_id = text(source.get("candidate_id"))
    if text(row.get("candidate_id")) != candidate_id:
        return "invalid", "candidate_id changed"
    for field in BOUNDARY_HEADERS[:11]:
        if text(row.get(field)) != text(source.get(field)):
            return "invalid", f"immutable field {field} changed"

    label = text(row.get("human_label")).upper()
    status = text(row.get("review_status")).upper()
    notes = text(row.get("review_notes"))
    if label and label not in LABELS:
        return "invalid", f"unsupported human_label={label!r}"
    if status not in {"", "UNLABELED", *BOUNDARY_REVIEW_STATUSES}:
        return "invalid", f"unsupported review_status={status!r}"
    if not label and status in {"", "UNLABELED"} and not notes:
        return "pending", ""
    if not label or status not in BOUNDARY_REVIEW_STATUSES:
        return "partial", "label or completion status is still missing"
    if status == "NEEDS_DISCUSSION" and not notes:
        return "partial", "NEEDS_DISCUSSION still needs review_notes"
    try:
        validate_boundary_rows(
            [BOUNDARY_HEADERS, [row.get(field) for field in BOUNDARY_HEADERS]],
            [source],
            project,
            slot,
        )
    except ValueError as exc:
        return "invalid", str(exc)
    return "complete", ""


def qa_row_state(row: dict, source: dict, project: str) -> tuple[str, str]:
    if text(row.get("qa_id")) != text(source.get("qa_id")):
        return "invalid", "qa_id changed"
    if text(row.get("project_id")) != project:
        return "invalid", "project_id changed"
    approval = text(row.get("approval_status")).upper()
    privacy = text(row.get("privacy_review_status")).upper()
    if approval not in {"", "PENDING", "DRAFT_NOT_APPROVED", *APPROVAL_STATUSES}:
        return "invalid", f"unsupported approval_status={approval!r}"
    if privacy not in PRIVACY_STATUSES | {""}:
        return "invalid", f"unsupported privacy_review_status={privacy!r}"
    try:
        split_privacy_flags(row.get("privacy_flags"))
    except ValueError as exc:
        return "invalid", str(exc)
    if approval in {"", "PENDING", "DRAFT_NOT_APPROVED"}:
        return "pending", "" if privacy in {"", "PENDING"} else "privacy reviewed; QA decision pending"
    if privacy in {"", "PENDING"}:
        return "partial", "QA decision recorded; privacy review pending"
    try:
        validate_qa_rows(
            [QA_HEADERS, [row.get(field) for field in QA_HEADERS]],
            [source],
            project,
        )
    except ValueError as exc:
        return "invalid", str(exc)
    return "complete", ""


def audit_sheet(workbook, sheet_name: str, headers: list[str], source: list[dict], row_state) -> dict:
    try:
        rows = read_sheet_rows(workbook, sheet_name, headers, len(source))
        records = rows_as_records(headers, rows[1:], sheet_name)
    except ValueError as exc:
        return {
            "total": len(source),
            "counts": {"complete": 0, "pending": 0, "partial": 0, "invalid": len(source)},
            "issues": [{"record_id": "", "message": str(exc)}],
            "sheet_valid": False,
        }

    source_by_id = {text(item[headers[0]]): item for item in source}
    record_ids = [text(item.get(headers[0])) for item in records]
    duplicate_ids = sorted(identifier for identifier, count in Counter(record_ids).items() if count > 1)
    missing_ids = sorted(set(source_by_id) - set(record_ids))
    extra_ids = sorted(set(record_ids) - set(source_by_id))
    identity_issues = []
    if duplicate_ids:
        identity_issues.append({"record_id": "", "message": f"duplicate IDs: {duplicate_ids[:5]}"})
    if missing_ids:
        identity_issues.append({"record_id": "", "message": f"missing IDs: {missing_ids[:5]}"})
    if extra_ids:
        identity_issues.append({"record_id": "", "message": f"unexpected IDs: {extra_ids[:5]}"})
    if identity_issues:
        return {
            "total": len(source),
            "counts": {"complete": 0, "pending": 0, "partial": 0, "invalid": len(source)},
            "issues": identity_issues,
            "sheet_valid": False,
        }

    counts = Counter()
    issues = []
    for row in records:
        record_id = text(row[headers[0]])
        state, message = row_state(row, source_by_id[record_id])
        counts[state] += 1
        if message:
            issues.append({"record_id": record_id, "state": state, "message": message})
    normalized_counts = {state: counts[state] for state in ("complete", "pending", "partial", "invalid")}
    return {
        "total": len(source),
        "counts": normalized_counts,
        "completion_rate": round(normalized_counts["complete"] / len(source), 6) if source else 1.0,
        "issues": issues[:50],
        "issue_count": len(issues),
        "sheet_valid": normalized_counts["invalid"] == 0,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description="Audit SEAC review progress without writing review data.")
    parser.add_argument(
        "--boundary-a",
        type=Path,
        default=Path("outputs/annotation_workbooks_v1/SEAC_boundary_annotator_A.xlsx"),
    )
    parser.add_argument(
        "--boundary-b",
        type=Path,
        default=Path("outputs/annotation_workbooks_v1/SEAC_boundary_annotator_B.xlsx"),
    )
    parser.add_argument(
        "--qa",
        type=Path,
        default=Path("outputs/annotation_workbooks_v1/SEAC_QA_privacy_review.xlsx"),
    )
    parser.add_argument("--workbench-root", type=Path, default=Path("workbench"))
    parser.add_argument("--require-complete", action="store_true")
    args = parser.parse_args()

    paths = {
        "boundary_a": args.boundary_a.resolve(),
        "boundary_b": args.boundary_b.resolve(),
        "qa": args.qa.resolve(),
    }
    missing = [str(path) for path in paths.values() if not path.is_file()]
    if missing:
        raise FileNotFoundError("missing review workbooks: " + ", ".join(missing))

    books = {
        "A": load_workbook(paths["boundary_a"], read_only=True, data_only=False),
        "B": load_workbook(paths["boundary_b"], read_only=True, data_only=False),
        "QA": load_workbook(paths["qa"], read_only=True, data_only=False),
    }
    workbench_root = args.workbench_root.resolve()
    boundary = {"A": {}, "B": {}}
    for slot in ("A", "B"):
        for project in BOUNDARY_PROJECTS:
            source = read_json(
                workbench_root
                / project
                / "annotation_pack"
                / f"boundary_blind_annotator_{slot.lower()}.json"
            )
            boundary[slot][project] = audit_sheet(
                books[slot],
                project,
                BOUNDARY_HEADERS,
                source,
                lambda row, expected, project=project, slot=slot: boundary_row_state(
                    row, expected, project, slot
                ),
            )

    qa = {}
    for project in QA_PROJECTS:
        source_path = (
            workbench_root / "P01" / "qa_privacy_review_pending.json"
            if project == "P01"
            else workbench_root / project / "annotation_pack" / "qa_draft.json"
        )
        source = read_json(source_path)
        qa[project] = audit_sheet(
            books["QA"],
            project,
            QA_HEADERS,
            source,
            lambda row, expected, project=project: qa_row_state(row, expected, project),
        )

    all_sheets = [*boundary["A"].values(), *boundary["B"].values(), *qa.values()]
    totals = Counter()
    for sheet in all_sheets:
        totals.update(sheet["counts"])
    ready = totals["pending"] == totals["partial"] == totals["invalid"] == 0
    report = {
        "protocol_version": "industrial-seac-review-progress-v1.0",
        "audited_at_utc": datetime.now(timezone.utc).isoformat(),
        "input_workbook_sha256": {name: sha256_file(path) for name, path in paths.items()},
        "totals": {state: totals[state] for state in ("complete", "pending", "partial", "invalid")},
        "ready_for_strict_import": ready,
        "boundary": boundary,
        "qa": qa,
    }
    print(json.dumps(report, ensure_ascii=False, indent=2))
    if totals["invalid"]:
        sys.exit(1)
    if args.require_complete and not ready:
        sys.exit(2)


if __name__ == "__main__":
    main()
