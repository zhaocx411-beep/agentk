"""Read and write the privacy review workbook for the local review view.

The P01 privacy decision is not a text-matching task. The pre-screen flagged all
27 rows `CUSTOMER_OR_VENDOR_IDENTITY` by scanning the question, the answer, the
evidence span, the document name and the section path *jointly*, so the flag never
says which of those fields actually carries the customer name. That distinction is
the whole decision: a row whose name appears only in the source file name has
clean question content, while a row whose answer states the name does not.

This module therefore locates every configured identifier per field and reports
where it occurs, so the reviewer sees the difference instead of rereading five
columns. It also carries the two couplings that would otherwise only surface
several commands later: a privacy rejection has to reject the content as well, and
editing the evidence span to anonymize it in place breaks the span-containment
check in `finalize_qa_review.py`.

Validation is kept identical to `import_review_workbooks.py`, so a decision this
view accepts is one the import gate accepts.
"""

from __future__ import annotations

import json
import os
import re
import sys
import tempfile
from pathlib import Path

from openpyxl import load_workbook

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from seac_chunking.units import document_units  # noqa: E402

# Mirrors import_review_workbooks.PRIVACY_STATUSES minus PENDING, which is the
# unset state rather than a decision.
PRIVACY_DECISIONS = ("NO_RISK", "ANONYMIZED", "CLEARED", "REJECTED")
PRIVACY_CLEARED = {"NO_RISK", "ANONYMIZED", "CLEARED"}
# A reason is mandatory for these two, matching the import gate.
REASON_REQUIRED = {"ANONYMIZED", "REJECTED"}
EDITABLE = ("privacy_review_status", "privacy_review_notes", "approval_status")
# The fields the pre-screen scanned together, in the order the reviewer reads them.
SCANNED_FIELDS = (
    "question",
    "reference_answer",
    "gold_evidence_span",
    "source_document",
    "source_section",
)
# A field in this set is QA content that would be published; the others are
# provenance metadata that never leaves the workbench.
CONTENT_FIELDS = {"question", "reference_answer", "gold_evidence_span"}


def cell_text(value) -> str:
    return "" if value is None else str(value).strip()


def normalize(text: str) -> str:
    """Strip all whitespace, exactly as `finalize_qa_review.normalize` does.

    A stricter comparison here would flag rows the gate accepts, and the reviewer
    would waste time chasing seven phantom mismatches.
    """

    return re.sub(r"\s+", "", str(text or ""))


def shared_prefix(names: list[str], minimum: int = 2) -> str:
    """Return the longest common prefix of the corpus file names.

    Every document of one project is named after the engagement, so the shared
    prefix is the customer identifier that triggered the pre-screen flag. Deriving
    it this way keeps the name out of the command line and out of shell history.
    """

    if not names:
        return ""
    prefix = names[0]
    for name in names[1:]:
        while prefix and not name.startswith(prefix):
            prefix = prefix[:-1]
    prefix = prefix.strip(" -_·")
    return prefix if len(prefix) >= minimum else ""


def highlight(text: str, identifiers: list[str]) -> list[dict]:
    """Split text into plain and sensitive runs, longest identifier first."""

    if not text:
        return []
    ordered = sorted({item for item in identifiers if item}, key=len, reverse=True)
    if not ordered:
        return [{"text": text, "sensitive": False}]
    runs: list[dict] = []
    position = 0
    while position < len(text):
        match = ""
        for identifier in ordered:
            if text.startswith(identifier, position):
                match = identifier
                break
        if match:
            runs.append({"text": match, "sensitive": True})
            position += len(match)
            continue
        # Accumulate plain characters onto the previous plain run so the view does
        # not receive one run per character.
        if runs and not runs[-1]["sensitive"]:
            runs[-1]["text"] += text[position]
        else:
            runs.append({"text": text[position], "sensitive": False})
        position += 1
    return runs


class PrivacyReviewStore:
    """One project sheet of the privacy review workbook, with its corpus."""

    def __init__(self, path: Path, project: str, corpus: Path | None = None, identifiers: list[str] | None = None) -> None:
        self.path = Path(path)
        self.project = project
        self.workbook = load_workbook(self.path)
        if project not in self.workbook.sheetnames:
            raise ValueError(f"{self.path.name}: no sheet named {project}")
        sheet = self.workbook[project]
        self.headers = [cell_text(cell.value) for cell in sheet[1]]
        missing = [name for name in (*EDITABLE, *SCANNED_FIELDS) if name not in self.headers]
        if missing:
            raise ValueError(f"{project}: sheet is missing column(s) {', '.join(missing)}")
        self.rows: list[dict] = []
        for number in range(2, sheet.max_row + 1):
            values = {
                header: cell_text(sheet.cell(row=number, column=position + 1).value)
                for position, header in enumerate(self.headers)
            }
            if not values.get("qa_id"):
                continue
            self.rows.append({"row": number, **values})
        if not self.rows:
            raise ValueError(f"{project}: sheet has no reviewable rows")
        self.documents: list[str] = []
        self.units = self.load_units(corpus)
        self.identifiers = self.resolve_identifiers(identifiers)

    def load_units(self, corpus: Path | None) -> dict[str, str]:
        """Index the project's retrieval units so evidence shows its full context."""

        if corpus is None:
            return {}
        units: dict[str, str] = {}
        for document in json.loads(Path(corpus).read_text(encoding="utf-8")):
            self.documents.append(str(document.get("file_name", "")))
            for unit in document_units(document):
                units[unit.unit_id] = unit.text
        return units

    def resolve_identifiers(self, identifiers: list[str] | None) -> list[str]:
        """Combine the name derived from the corpus with any passed explicitly."""

        derived = shared_prefix(self.documents)
        supplied = [item.strip() for item in (identifiers or []) if item.strip()]
        return sorted({item for item in (*supplied, derived) if item}, key=len, reverse=True)

    def occurrences(self, row: dict) -> dict:
        """Report which scanned fields carry an identifier, and whether any is content.

        `content_exposed` is the answer to the question the reviewer actually has:
        would clearing this row publish the customer name, or only record it in a
        workbench file?
        """

        fields = [name for name in SCANNED_FIELDS if any(item in row.get(name, "") for item in self.identifiers)]
        return {
            "fields": fields,
            "content_exposed": bool(CONTENT_FIELDS.intersection(fields)),
            "metadata_only": bool(fields) and not CONTENT_FIELDS.intersection(fields),
        }

    def payload(self, index: int) -> dict:
        row = self.rows[index]
        unit_id = row.get("gold_evidence_unit_id", "")
        unit_text = self.units.get(unit_id, "")
        span = row.get("gold_evidence_span", "")
        return {
            "index": index,
            "total": len(self.rows),
            "qa_id": row["qa_id"],
            "project_id": row.get("project_id", self.project),
            "question": highlight(row.get("question", ""), self.identifiers),
            "reference_answer": highlight(row.get("reference_answer", ""), self.identifiers),
            "gold_evidence_span": highlight(span, self.identifiers),
            "source_document": highlight(row.get("source_document", ""), self.identifiers),
            "source_section": highlight(row.get("source_section", ""), self.identifiers),
            "question_type": row.get("question_type", ""),
            "gold_evidence_unit_id": unit_id,
            "unit_text": highlight(unit_text, self.identifiers),
            "unit_resolved": bool(unit_text),
            # The span must stay in the unit or finalize_qa_review rejects the
            # record, so the view has to warn before an in-place edit is tried.
            "span_in_unit": bool(unit_text) and normalize(span) in normalize(unit_text),
            "privacy_flags": row.get("privacy_flags", ""),
            "approval_status": row.get("approval_status", ""),
            "review_notes": row.get("review_notes", ""),
            "privacy_review_status": row.get("privacy_review_status", ""),
            "privacy_review_notes": row.get("privacy_review_notes", ""),
            "occurrences": self.occurrences(row),
            "identifiers": self.identifiers,
        }

    def decide(self, index: int, status: str, notes: str) -> dict:
        """Record one privacy decision in memory and in the workbook.

        A rejection also rejects the content, because `import_review_workbooks.py`
        refuses a privacy rejection that leaves `approval_status` approved. The
        content reviewer's own note is left untouched; the privacy reason carries
        why the item was dropped.
        """

        if status not in PRIVACY_DECISIONS:
            raise ValueError(f"unsupported privacy_review_status {status!r}")
        if status in REASON_REQUIRED and not notes.strip():
            reason = "a substitution rule" if status == "ANONYMIZED" else "a reason"
            raise ValueError(f"{status} requires {reason} in privacy_review_notes")
        row = self.rows[index]
        row["privacy_review_status"] = status
        row["privacy_review_notes"] = notes.strip()
        row["approval_status"] = "REJECTED" if status == "REJECTED" else row["approval_status"]
        sheet = self.workbook[self.project]
        for header in EDITABLE:
            sheet.cell(row=row["row"], column=self.headers.index(header) + 1, value=row[header] or None)
        self.save()
        return self.progress()

    def save(self) -> None:
        # An atomic replace would silently resurrect a workbook that was moved or
        # deleted after load, sending decisions into a file nobody is tracking.
        if not self.path.is_file():
            raise ValueError(f"{self.path} no longer exists; decision was not saved")
        handle, temporary = tempfile.mkstemp(prefix=".privacy-", suffix=".xlsx", dir=self.path.parent)
        os.close(handle)
        try:
            self.workbook.save(temporary)
            os.replace(temporary, self.path)
        finally:
            if Path(temporary).exists():
                Path(temporary).unlink()

    def is_decided(self, row: dict) -> bool:
        return row.get("privacy_review_status", "") in PRIVACY_DECISIONS

    def next_undecided(self, start: int) -> int | None:
        order = list(range(start, len(self.rows))) + list(range(0, start))
        for index in order:
            if not self.is_decided(self.rows[index]):
                return index
        return None

    def progress(self) -> dict:
        decided = sum(self.is_decided(row) for row in self.rows)
        cleared = sum(
            row.get("privacy_review_status", "") in PRIVACY_CLEARED for row in self.rows
        )
        return {
            "total": len(self.rows),
            "decided": decided,
            "remaining": len(self.rows) - decided,
            "cleared": cleared,
            "status_counts": {
                status: sum(row.get("privacy_review_status") == status for row in self.rows)
                for status in PRIVACY_DECISIONS
            },
            "content_exposed": sum(self.occurrences(row)["content_exposed"] for row in self.rows),
        }
