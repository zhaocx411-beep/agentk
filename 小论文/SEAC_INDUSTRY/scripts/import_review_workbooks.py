from __future__ import annotations

import argparse
import hashlib
import json
import os
import tempfile
from copy import deepcopy
from datetime import datetime, timezone
from pathlib import Path

from openpyxl import load_workbook


BOUNDARY_PROJECTS = [f"P{index:02d}" for index in range(2, 11)]
QA_PROJECTS = ["P01", *BOUNDARY_PROJECTS]
BOUNDARY_HEADERS = [
    "candidate_id",
    "project_id",
    "source_document",
    "document_type",
    "boundary_stratum",
    "left_section",
    "left_block_type",
    "left_evidence",
    "right_section",
    "right_block_type",
    "right_evidence",
    "human_label",
    "review_status",
    "review_notes",
]
QA_HEADERS = [
    "qa_id",
    "project_id",
    "question",
    "reference_answer",
    "source_document",
    "source_section",
    "question_type",
    "difficulty",
    "gold_evidence_id",
    "gold_evidence_unit_id",
    "gold_evidence_span",
    "evidence_alignment_status",
    "privacy_flags",
    "approval_status",
    "privacy_review_status",
    "review_notes",
    "privacy_review_notes",
]
LABELS = {"BREAK", "NO_BREAK", "NOISE"}
BOUNDARY_REVIEW_STATUSES = {"COMPLETE", "NEEDS_DISCUSSION"}
APPROVAL_STATUSES = {"APPROVED", "REJECTED"}
PRIVACY_STATUSES = {"PENDING", "NO_RISK", "ANONYMIZED", "CLEARED", "REJECTED"}
PRIVACY_CLEARED = {"NO_RISK", "ANONYMIZED", "CLEARED"}
PRIVACY_FLAGS = {
    "PERSON_NAME_OR_CONTACT",
    "INTERNAL_NETWORK_OR_URL",
    "SECRET_OR_CREDENTIAL",
    "CUSTOMER_OR_VENDOR_IDENTITY",
    "SECURITY_ARCHITECTURE",
    "PROPRIETARY_BUSINESS_DATA",
}


def text(value: object) -> str:
    if value is None:
        return ""
    if isinstance(value, bool):
        return "TRUE" if value else "FALSE"
    return str(value).strip()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def canonical_json_bytes(value: object) -> bytes:
    return (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode("utf-8")


def keyed(records: list[dict], id_field: str, context: str) -> dict[str, dict]:
    output: dict[str, dict] = {}
    for record in records:
        record_id = text(record.get(id_field))
        if not record_id or record_id in output:
            raise ValueError(f"{context}: missing or duplicate {id_field}={record_id!r}")
        output[record_id] = record
    return output


def rows_as_records(headers: list[str], rows: list[list[object]], context: str) -> list[dict]:
    records = []
    for index, row in enumerate(rows, 2):
        if len(row) != len(headers):
            raise ValueError(
                f"{context}: row {index} has {len(row)} columns; expected {len(headers)}"
            )
        records.append(dict(zip(headers, row)))
    return records


def validate_boundary_rows(
    rows: list[list[object]], source: list[dict], project: str, slot: str
) -> list[dict]:
    if [text(value) for value in rows[0]] != BOUNDARY_HEADERS:
        raise ValueError(f"boundary {slot} {project}: header row was changed")
    expected = keyed(source, "candidate_id", f"boundary source {slot} {project}")
    reviewed_rows = rows[1:]
    if len(reviewed_rows) != len(source):
        raise ValueError(
            f"boundary {slot} {project}: expected {len(source)} rows, got {len(reviewed_rows)}"
        )
    reviewed = keyed(
        rows_as_records(BOUNDARY_HEADERS, reviewed_rows, f"boundary workbook {slot} {project}"),
        "candidate_id",
        f"boundary workbook {slot} {project}",
    )
    if set(reviewed) != set(expected):
        missing = sorted(set(expected) - set(reviewed))
        extra = sorted(set(reviewed) - set(expected))
        raise ValueError(
            f"boundary {slot} {project}: candidate set changed; missing={missing[:5]}, extra={extra[:5]}"
        )

    output = []
    for source_record in source:
        candidate_id = text(source_record["candidate_id"])
        row = reviewed[candidate_id]
        for field in BOUNDARY_HEADERS[:11]:
            if text(row[field]) != text(source_record.get(field)):
                raise ValueError(
                    f"boundary {slot} {project} {candidate_id}: immutable field {field} was changed"
                )
        label = text(row["human_label"]).upper()
        status = text(row["review_status"]).upper()
        notes = text(row["review_notes"])
        if label not in LABELS:
            raise ValueError(f"boundary {slot} {project} {candidate_id}: invalid or missing human_label")
        if status not in BOUNDARY_REVIEW_STATUSES:
            raise ValueError(f"boundary {slot} {project} {candidate_id}: review_status is not complete")
        if status == "NEEDS_DISCUSSION" and not notes:
            raise ValueError(
                f"boundary {slot} {project} {candidate_id}: NEEDS_DISCUSSION requires review_notes"
            )
        final = deepcopy(source_record)
        final.update(human_label=label, review_status=status, review_notes=notes)
        output.append(final)
    return output


def split_privacy_flags(value: object) -> list[str]:
    flags = [part.strip().upper() for part in text(value).split(";") if part.strip()]
    unknown = sorted(set(flags) - PRIVACY_FLAGS)
    if unknown:
        raise ValueError(f"unsupported privacy_flags={unknown}")
    return list(dict.fromkeys(flags))


def validate_qa_rows(rows: list[list[object]], source: list[dict], project: str) -> list[dict]:
    if [text(value) for value in rows[0]] != QA_HEADERS:
        raise ValueError(f"QA {project}: header row was changed")
    expected = keyed(source, "qa_id", f"QA source {project}")
    reviewed_rows = rows[1:]
    if len(reviewed_rows) != len(source):
        raise ValueError(f"QA {project}: expected {len(source)} rows, got {len(reviewed_rows)}")
    reviewed = keyed(
        rows_as_records(QA_HEADERS, reviewed_rows, f"QA workbook {project}"),
        "qa_id",
        f"QA workbook {project}",
    )
    if set(reviewed) != set(expected):
        missing = sorted(set(expected) - set(reviewed))
        extra = sorted(set(reviewed) - set(expected))
        raise ValueError(f"QA {project}: qa_id set changed; missing={missing[:5]}, extra={extra[:5]}")

    output = []
    for source_record in source:
        qa_id = text(source_record["qa_id"])
        row = reviewed[qa_id]
        if text(row["project_id"]) != project:
            raise ValueError(f"QA {project} {qa_id}: project_id was changed")
        approval = text(row["approval_status"]).upper()
        privacy = text(row["privacy_review_status"]).upper()
        review_notes = text(row["review_notes"])
        privacy_notes = text(row["privacy_review_notes"])
        if approval not in APPROVAL_STATUSES:
            raise ValueError(f"QA {project} {qa_id}: approval_status is not complete")
        if privacy not in PRIVACY_STATUSES:
            raise ValueError(f"QA {project} {qa_id}: unsupported privacy_review_status={privacy!r}")
        if approval == "APPROVED" and privacy not in PRIVACY_CLEARED:
            raise ValueError(f"QA {project} {qa_id}: approved item lacks privacy clearance")
        if privacy == "REJECTED" and approval != "REJECTED":
            raise ValueError(f"QA {project} {qa_id}: privacy rejection requires content rejection")
        if approval == "REJECTED" and not (review_notes or privacy_notes):
            raise ValueError(f"QA {project} {qa_id}: rejection requires a reason")
        if privacy in {"ANONYMIZED", "REJECTED"} and not privacy_notes:
            raise ValueError(f"QA {project} {qa_id}: {privacy} requires privacy_review_notes")
        flags = split_privacy_flags(row["privacy_flags"])
        if approval == "APPROVED":
            for required in ("question", "reference_answer", "gold_evidence_id", "gold_evidence_unit_id", "gold_evidence_span"):
                if not text(row[required]):
                    raise ValueError(f"QA {project} {qa_id}: approved item is missing {required}")

        final = deepcopy(source_record)
        for field in QA_HEADERS[2:12]:
            final[field] = text(row[field])
        final["privacy_flags"] = flags
        final["approval_status"] = approval
        final["privacy_review_status"] = privacy
        final["review_notes"] = review_notes
        final["privacy_review_notes"] = privacy_notes
        if "unit_alignment_status" in final:
            final["unit_alignment_status"] = final["evidence_alignment_status"]
        output.append(final)
    return output


def read_sheet_rows(workbook, sheet_name: str, headers: list[str], count: int) -> list[list[object]]:
    if sheet_name not in workbook.sheetnames:
        raise ValueError(f"missing worksheet {sheet_name}")
    sheet = workbook[sheet_name]
    if sheet.max_row is None:
        sheet.calculate_dimension(force=True)
    cells = list(
        sheet.iter_rows(
            min_row=1,
            max_row=count + 1,
            min_col=1,
            max_col=len(headers),
        )
    )
    if len(cells) != count + 1:
        raise ValueError(f"{sheet_name}: expected {count} records, worksheet has {max(len(cells) - 1, 0)}")
    for row in cells:
        for cell in row:
            if cell.data_type == "f":
                raise ValueError(f"{sheet_name}!{cell.coordinate}: formulas are not allowed in review data")
    actual_max_row = sheet.max_row or count + 1
    if actual_max_row > count + 1:
        for extra_row in sheet.iter_rows(
            min_row=count + 2,
            max_row=actual_max_row,
            min_col=1,
            max_col=len(headers),
        ):
            if any(text(cell.value) for cell in extra_row):
                raise ValueError(f"{sheet_name}: unexpected data after the expected {count} records")
    return [[cell.value for cell in row] for row in cells]


def read_json(path: Path) -> list[dict]:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, list):
        raise ValueError(f"{path}: expected a JSON list")
    return value


def write_outputs_atomically(outputs: dict[Path, bytes], force: bool) -> None:
    existing = [path for path in outputs if path.exists()]
    if existing and not force:
        raise FileExistsError(
            "review outputs already exist; pass --force only after preserving the earlier review: "
            + ", ".join(str(path) for path in existing[:5])
        )
    common_root = Path(os.path.commonpath([str(path.parent) for path in outputs])).resolve()
    common_root.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="seac-review-import-", dir=common_root) as temp_dir:
        staging = Path(temp_dir)
        staged: dict[Path, Path] = {}
        for index, (target, payload) in enumerate(outputs.items()):
            staged_path = staging / f"{index:03d}.json"
            staged_path.write_bytes(payload)
            staged[target] = staged_path
        for target, staged_path in staged.items():
            target.parent.mkdir(parents=True, exist_ok=True)
            os.replace(staged_path, target)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Import completed SEAC boundary and QA review workbooks with strict identity checks."
    )
    parser.add_argument("--boundary-a", type=Path)
    parser.add_argument("--boundary-b", type=Path)
    parser.add_argument("--qa", type=Path)
    # Scoping exists because P02-P10 boundaries moved to the pre-label plus
    # verification path, so a P01 privacy release must not require the A/B
    # boundary workbooks that path replaced.
    parser.add_argument("--boundary-projects", nargs="*", default=BOUNDARY_PROJECTS)
    parser.add_argument("--qa-projects", nargs="*", default=QA_PROJECTS)
    parser.add_argument("--workbench-root", type=Path, default=Path("workbench"))
    parser.add_argument("--check-only", action="store_true")
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()

    unknown = sorted((set(args.boundary_projects) - set(BOUNDARY_PROJECTS)) | (set(args.qa_projects) - set(QA_PROJECTS)))
    if unknown:
        raise ValueError(f"unknown projects requested: {unknown}")
    if not args.boundary_projects and not args.qa_projects:
        raise ValueError("nothing to import; select at least one boundary or QA project")

    required = {}
    if args.boundary_projects:
        required.update({"boundary_a": args.boundary_a, "boundary_b": args.boundary_b})
    if args.qa_projects:
        required["qa"] = args.qa
    missing = sorted(name for name, path in required.items() if path is None)
    if missing:
        raise ValueError(f"the selected projects need these workbooks: {', '.join(missing)}")

    workbook_paths = {name: path.resolve() for name, path in required.items()}
    for name, path in workbook_paths.items():
        if not path.is_file():
            raise FileNotFoundError(f"{name} workbook does not exist: {path}")

    boundary_books = {
        slot: load_workbook(workbook_paths[f"boundary_{slot}"], read_only=True, data_only=False)
        for slot in ("a", "b")
        if f"boundary_{slot}" in workbook_paths
    }
    qa_book = load_workbook(workbook_paths["qa"], read_only=True, data_only=False) if "qa" in workbook_paths else None
    workbench_root = args.workbench_root.resolve()
    outputs: dict[Path, bytes] = {}
    summary_projects: dict[str, dict] = {}

    for project in args.boundary_projects:
        project_summary: dict[str, int] = {}
        for slot in ("a", "b"):
            source_path = workbench_root / project / "annotation_pack" / f"boundary_blind_annotator_{slot}.json"
            source = read_json(source_path)
            rows = read_sheet_rows(boundary_books[slot], project, BOUNDARY_HEADERS, len(source))
            reviewed = validate_boundary_rows(rows, source, project, slot.upper())
            target = workbench_root / project / "annotation_review" / f"boundary_annotator_{slot}_reviewed.json"
            outputs[target] = canonical_json_bytes(reviewed)
            project_summary[f"boundary_{slot}_count"] = len(reviewed)
        summary_projects[project] = project_summary

    for project in args.qa_projects:
        source_path = (
            workbench_root / "P01" / "qa_privacy_review_pending.json"
            if project == "P01"
            else workbench_root / project / "annotation_pack" / "qa_draft.json"
        )
        source = read_json(source_path)
        rows = read_sheet_rows(qa_book, project, QA_HEADERS, len(source))
        reviewed = validate_qa_rows(rows, source, project)
        target = workbench_root / project / "annotation_review" / "qa_reviewed.json"
        outputs[target] = canonical_json_bytes(reviewed)
        summary_projects.setdefault(project, {})["qa_reviewed_count"] = len(reviewed)
        summary_projects[project]["qa_approved_count"] = sum(
            item["approval_status"] == "APPROVED" for item in reviewed
        )
        summary_projects[project]["qa_rejected_count"] = sum(
            item["approval_status"] == "REJECTED" for item in reviewed
        )

    output_hashes = {
        str(path.relative_to(workbench_root)): hashlib.sha256(payload).hexdigest()
        for path, payload in outputs.items()
    }
    summary = {
        "protocol_version": "industrial-seac-workbook-import-v1.0",
        "validated_at_utc": datetime.now(timezone.utc).isoformat(),
        "check_only": args.check_only,
        "boundary_projects": list(args.boundary_projects),
        "qa_projects": list(args.qa_projects),
        "input_workbook_sha256": {
            name: sha256_file(path) for name, path in workbook_paths.items()
        },
        "projects": summary_projects,
        "output_sha256": output_hashes,
    }
    summary_path = workbench_root / "annotation_review_import_summary.json"
    outputs[summary_path] = canonical_json_bytes(summary)
    if not args.check_only:
        write_outputs_atomically(outputs, args.force)
    print(json.dumps(summary, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
