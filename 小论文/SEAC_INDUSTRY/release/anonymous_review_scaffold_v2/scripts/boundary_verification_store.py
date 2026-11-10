"""Read and write the boundary verification workbook for the annotation tool.

The workbook is the authoritative record, so this module never keeps a private
copy of the labels: it reads them on load and writes each decision straight back,
touching only `human_label`, `review_status` and `review_notes`. Saves are atomic
so an interrupted write cannot leave a half-written workbook behind.

Nothing here reads the pre-label files. The verification is blind by design, and
the tool must not be able to leak a model label into the annotator's view.
"""

from __future__ import annotations

import os
import re
import tempfile
from pathlib import Path

from openpyxl import load_workbook

LABELS = ("BREAK", "NO_BREAK", "NOISE")
REVIEW_STATUSES = ("COMPLETE", "NEEDS_DISCUSSION")
EDITABLE = ("human_label", "review_status", "review_notes")
TABLE_MARKERS = re.compile(r"表头:\s*(.*?)\s*数据行:\s*(.*)", re.S)


def cell_text(value) -> str:
    return "" if value is None else str(value).strip()


def parse_table_evidence(evidence: str) -> dict:
    """Split a table-row unit into header and data cells so it can be shown aligned.

    Table rows arrive as `表头: a | b | c 数据行: x | y | z`, which is unreadable in
    a spreadsheet cell. Pairing the two lists lets the tool drop the columns whose
    header and value repeat, which is what makes a merged-cell table legible.
    """

    match = TABLE_MARKERS.search(evidence or "")
    if not match:
        return {"kind": "text", "text": evidence or ""}
    header = [part.strip() for part in match.group(1).split("|")]
    data = [part.strip() for part in match.group(2).split("|")]
    width = max(len(header), len(data))
    header += [""] * (width - len(header))
    data += [""] * (width - len(data))
    columns = [
        {"header": head, "value": value}
        for index, (head, value) in enumerate(zip(header, data))
        # A merged cell repeats its text across the columns it spans; keeping the
        # first occurrence preserves the content and removes the visual noise.
        if index == 0 or (head, value) != (header[index - 1], data[index - 1])
    ]
    return {"kind": "table", "columns": columns, "collapsed": width - len(columns)}


class VerificationStore:
    """A workbook-backed view of one verification pack."""

    def __init__(self, path: Path) -> None:
        self.path = Path(path)
        self.workbook = load_workbook(self.path)
        self.projects = [name for name in self.workbook.sheetnames if name != "Guide"]
        if not self.projects:
            raise ValueError(f"{self.path.name}: no project sheets found")
        self.headers = {}
        self.rows: list[dict] = []
        for project in self.projects:
            sheet = self.workbook[project]
            headers = [cell_text(cell.value) for cell in sheet[1]]
            missing = [name for name in EDITABLE if name not in headers]
            if missing:
                raise ValueError(f"{project}: sheet is missing column(s) {', '.join(missing)}")
            self.headers[project] = headers
            for number in range(2, sheet.max_row + 1):
                values = {
                    header: cell_text(sheet.cell(row=number, column=position + 1).value)
                    for position, header in enumerate(headers)
                }
                if not values.get("candidate_id"):
                    continue
                self.rows.append({"project": project, "row": number, **values})

    def payload(self, index: int) -> dict:
        """Render one candidate for the annotator, without any model-derived hint."""

        row = self.rows[index]
        return {
            "index": index,
            "total": len(self.rows),
            "candidate_id": row["candidate_id"],
            "project_id": row["project"],
            "row_group": row.get("row_group", ""),
            "boundary_stratum": row.get("boundary_stratum", ""),
            "source_document": row.get("source_document", ""),
            "document_type": row.get("document_type", ""),
            "left": {
                "section": row.get("left_section", ""),
                "block_type": row.get("left_block_type", ""),
                "evidence": parse_table_evidence(row.get("left_evidence", "")),
            },
            "right": {
                "section": row.get("right_section", ""),
                "block_type": row.get("right_block_type", ""),
                "evidence": parse_table_evidence(row.get("right_evidence", "")),
            },
            "same_section": bool(
                row.get("left_section") and row.get("left_section") == row.get("right_section")
            ),
            "human_label": row.get("human_label", ""),
            "review_status": row.get("review_status", ""),
            "review_notes": row.get("review_notes", ""),
        }

    def decide(self, index: int, label: str, status: str, notes: str) -> dict:
        """Record one decision in memory and in the workbook."""

        if label and label not in LABELS:
            raise ValueError(f"unsupported label {label!r}")
        if status and status not in REVIEW_STATUSES:
            raise ValueError(f"unsupported review status {status!r}")
        if status == "NEEDS_DISCUSSION" and not notes.strip():
            raise ValueError("a row flagged NEEDS_DISCUSSION needs a reason")
        row = self.rows[index]
        row["human_label"] = label
        row["review_status"] = status
        row["review_notes"] = notes.strip()
        sheet = self.workbook[row["project"]]
        headers = self.headers[row["project"]]
        for header in EDITABLE:
            sheet.cell(row=row["row"], column=headers.index(header) + 1, value=row[header] or None)
        self.save()
        return self.progress()

    def save(self) -> None:
        # An atomic replace would silently resurrect a workbook that was moved or
        # deleted after load, sending the annotator's decisions into a file nobody
        # is tracking. Refuse instead, so the session can be pointed at the real one.
        if not self.path.is_file():
            raise ValueError(f"{self.path} no longer exists; decision was not saved")
        directory = self.path.parent
        handle, temporary = tempfile.mkstemp(prefix=".verification-", suffix=".xlsx", dir=directory)
        os.close(handle)
        try:
            self.workbook.save(temporary)
            os.replace(temporary, self.path)
        finally:
            if Path(temporary).exists():
                Path(temporary).unlink()

    def is_decided(self, row: dict) -> bool:
        return row.get("human_label", "") in LABELS and row.get("review_status", "") in REVIEW_STATUSES

    def next_undecided(self, start: int) -> int | None:
        order = list(range(start, len(self.rows))) + list(range(0, start))
        for index in order:
            if not self.is_decided(self.rows[index]):
                return index
        return None

    def progress(self) -> dict:
        per_project: dict[str, dict] = {}
        for row in self.rows:
            entry = per_project.setdefault(row["project"], {"total": 0, "decided": 0, "flagged": 0})
            entry["total"] += 1
            if self.is_decided(row):
                entry["decided"] += 1
                if row["review_status"] == "NEEDS_DISCUSSION":
                    entry["flagged"] += 1
        decided = sum(entry["decided"] for entry in per_project.values())
        return {
            "total": len(self.rows),
            "decided": decided,
            "remaining": len(self.rows) - decided,
            "flagged": sum(entry["flagged"] for entry in per_project.values()),
            "projects": per_project,
            "label_counts": {
                label: sum(row.get("human_label") == label for row in self.rows) for label in LABELS
            },
        }
