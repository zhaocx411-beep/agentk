"""Read and write the QA approval workbook for the terminal-review tool.

The workbook stays the authoritative record: this module reads the four reviewer
columns on load and writes each decision straight back, touching nothing else.
Saves are atomic so an interrupted write cannot leave a half-written workbook.

The reason a dedicated tool exists is the multi-evidence model. A candidate whose
span occurs in several corpus locations can only be judged by reading all of them,
and a spreadsheet cell shows at most a truncated list of unit ids. Here every
location is resolved from `corpus.json` and rendered with the evidence span
highlighted, which is what makes the "do these locations agree?" question
answerable.

Validation is delegated to the importer so the tool cannot accept a decision the
promotion step would later reject.
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
sys.path.insert(0, str(Path(__file__).resolve().parent))

from build_qa_approval_workbook import HEADERS
from import_qa_approval_workbook import (
    APPROVAL_VALUES,
    DIFFICULTIES,
    PRIVACY_VALUES,
    validate_decision,
)
from resolve_qa_evidence_equivalents import normalize, own_content
from seac_chunking.units import document_units

EDITABLE = ("approval_status", "privacy_review_status", "difficulty", "review_notes")
TABLE_MARKERS = re.compile(r"表头:\s*(.*?)\s*数据行:\s*(.*)", re.S)
# Reading more than this many locations for one candidate is not a judgement a
# reviewer can make anyway; a span that pervasive fails the distinctiveness gate.
LOCATION_LIMIT = 12


def cell_text(value) -> str:
    return "" if value is None else str(value).strip()


def stripped_index(text: str) -> tuple[str, list[int]]:
    """Return the whitespace-free text and each kept character's original offset.

    Evidence spans are matched after whitespace removal, but the highlight has to
    land on the readable text, so the mapping back is kept explicitly.
    """

    kept = [position for position, character in enumerate(text) if not character.isspace()]
    return "".join(text[position] for position in kept), kept


def highlight(text: str, span: str) -> list[dict]:
    """Split `text` into consecutive matched and unmatched segments of `span`."""

    target = normalize(span)
    stripped, kept = stripped_index(text)
    if not target or target not in stripped:
        return [{"text": text, "match": False}]
    segments = []
    cursor = 0
    position = stripped.find(target)
    while position != -1:
        start = kept[position]
        end = kept[position + len(target) - 1] + 1
        if start > cursor:
            segments.append({"text": text[cursor:start], "match": False})
        segments.append({"text": text[start:end], "match": True})
        cursor = end
        position = stripped.find(target, position + len(target))
    if cursor < len(text):
        segments.append({"text": text[cursor:], "match": False})
    return segments


def marked_cells(values: list[str], span: str) -> list[bool]:
    """Report which data cells of a table row carry the evidence span.

    `own_content` joins the row's cells with the pipe that survives whitespace
    normalisation, so a span's offset in that joined string identifies the cells
    it covers. This is what lets the tool point at the column whose semantic role
    the reviewer has to confirm.
    """

    target = normalize(span)
    sizes = [len(normalize(value)) for value in values]
    bounds = []
    cursor = 0
    for size in sizes:
        bounds.append((cursor, cursor + size))
        cursor += size + 1
    joined = "|".join(normalize(value) for value in values)
    hits = []
    position = joined.find(target) if target else -1
    while position != -1:
        hits.append((position, position + len(target)))
        position = joined.find(target, position + 1)
    return [
        any(start < hit_end and hit_start < end for hit_start, hit_end in hits)
        for start, end in bounds
    ]


def render_evidence(text: str, span: str) -> dict:
    """Render a unit as highlighted prose, or as an aligned grid when it is a table row.

    The two lines are returned as they are, header row above data row, rather than
    as header-and-value pairs. Several of these tables use their first row as a
    label grid rather than as column names, and pairing the rows up as if the
    first were a header would quietly assert a column semantics that is exactly
    what the reviewer has been asked to check.
    """

    match = TABLE_MARKERS.search(text or "")
    if not match:
        return {"kind": "text", "segments": highlight(text or "", span)}
    header = [part.strip() for part in match.group(1).split("|")]
    data = [part.strip() for part in match.group(2).split("|")]
    width = max(len(header), len(data))
    header += [""] * (width - len(header))
    data += [""] * (width - len(data))
    marks = marked_cells(data, span)
    # When the span covers the whole row there is no column to point at, and
    # marking every cell would only drown the cases where it points at one.
    if all(marks):
        marks = [False] * width

    columns = []
    collapsed = 0
    for index in range(width):
        # A merged cell repeats its text across the columns it spans; keeping the
        # first occurrence preserves the content and removes the visual noise.
        if index and (header[index], data[index]) == (header[index - 1], data[index - 1]):
            columns[-1]["match"] = columns[-1]["match"] or marks[index]
            collapsed += 1
            continue
        columns.append({"header": header[index], "value": data[index], "match": marks[index]})
    return {"kind": "table", "columns": columns, "collapsed": collapsed}


class ApprovalStore:
    """A workbook-backed view of the QA approval pack, with evidence resolved."""

    def __init__(self, path: Path, workbench_root: Path, max_occurrences: int = 12) -> None:
        self.path = Path(path)
        self.workbench_root = Path(workbench_root)
        self.max_occurrences = max_occurrences
        self.workbook = load_workbook(self.path)
        self.projects = [name for name in self.workbook.sheetnames if name != "Guide"]
        if not self.projects:
            raise ValueError(f"{self.path.name}: no project sheets found")
        self.headers: dict[str, list[str]] = {}
        self.rows: list[dict] = []
        self._units: dict[str, list[dict]] = {}
        for project in self.projects:
            sheet = self.workbook[project]
            headers = [cell_text(cell.value) for cell in sheet[1][: len(HEADERS)]]
            if headers != HEADERS:
                raise ValueError(f"{project}: sheet header does not match the generated workbook")
            self.headers[project] = headers
            for number in range(2, sheet.max_row + 1):
                values = {
                    header: cell_text(sheet.cell(row=number, column=position + 1).value)
                    for position, header in enumerate(headers)
                }
                if not values["qa_key"]:
                    continue
                self.rows.append({"project": project, "row": number, **values})

    # ---------- corpus ----------

    def unit_index(self, project: str) -> list[dict]:
        """Index one project's corpus units, keeping the raw text for display."""

        if project not in self._units:
            corpus = json.loads(
                (self.workbench_root / project / "corpus.json").read_text(encoding="utf-8")
            )
            units = []
            for document in corpus:
                for unit in document_units(document):
                    units.append(
                        {
                            "unit_id": unit.unit_id,
                            "document": document["file_name"],
                            "heading_path": unit.heading_path,
                            "block_type": unit.block_type,
                            "text": unit.text,
                            "own": own_content(unit.text),
                        }
                    )
            self._units[project] = units
        return self._units[project]

    # ---------- reading ----------

    def payload(self, index: int) -> dict:
        """Render one candidate together with every corpus location of its evidence."""

        row = self.rows[index]
        span = row["gold_evidence_span"]
        target = own_content(span)
        matches = [unit for unit in self.unit_index(row["project"]) if target and target in unit["own"]]
        answer = normalize(row["reference_answer"])
        return {
            "index": index,
            "total": len(self.rows),
            "qa_key": row["qa_key"],
            "project_id": row["project_id"] or row["project"],
            "candidate_state": row["candidate_state"],
            "question_type": row["question_type"],
            "question": row["question"],
            "reference_answer": row["reference_answer"],
            "gold_evidence_span": span,
            "answer_grounding": row["answer_grounding"],
            # The 81 pre-approved and multi-evidence rows carry no grounding verdict
            # from the rewrite check, so the literal relation is derived here rather
            # than left blank in the reviewer's view.
            "answer_is_substring": bool(answer and target and answer in target),
            "needs_role_check": row["needs_role_check"].upper() == "YES",
            "prereview_notes": row["prereview_notes"],
            "suggested_privacy_flags": row["suggested_privacy_flags"],
            "suggested_difficulty": row["suggested_difficulty"],
            "occurrences": len(matches),
            "locations": [
                {
                    "unit_id": unit["unit_id"],
                    "document": unit["document"],
                    "heading_path": unit["heading_path"],
                    "block_type": unit["block_type"],
                    "evidence": render_evidence(unit["text"], span),
                }
                for unit in matches[:LOCATION_LIMIT]
            ],
            "locations_hidden": max(0, len(matches) - LOCATION_LIMIT),
            "span_missing": not matches,
            "over_ceiling": len(matches) > self.max_occurrences,
            "decision": {header: row[header] for header in EDITABLE},
        }

    # ---------- writing ----------

    def reasons(self, decision: dict) -> list[str]:
        """Explain in the reviewer's language why a decision cannot be recorded.

        The importer's `validate_decision` stays the authority on what is
        acceptable; these messages only restate the same conditions. The caller
        checks both, so any drift shows up as a rejected write rather than as a
        decision the promotion step would silently refuse later.
        """

        approval = decision["approval_status"]
        privacy = decision["privacy_review_status"]
        problems = []
        if not approval or not privacy:
            problems.append("内容结论与隐私结论必须同时给出")
        if approval and approval not in APPROVAL_VALUES:
            problems.append(f"内容结论 {approval!r} 不是 APPROVED 或 REJECTED")
        if privacy and privacy not in PRIVACY_VALUES:
            problems.append(f"隐私结论 {privacy!r} 不在允许取值内")
        if approval == "APPROVED" and decision["difficulty"] not in DIFFICULTIES:
            problems.append("放行的题必须给出难度（Easy / Medium / Hard）")
        if approval and approval != "APPROVED" and decision["difficulty"]:
            problems.append("被拒的题不应带难度")
        if "REJECTED" in {approval, privacy} and not decision["review_notes"]:
            problems.append("拒绝必须在备注里写明理由")
        if privacy == "ANONYMIZED" and not decision["review_notes"]:
            problems.append("判为 ANONYMIZED 必须在备注里写明替换规则")
        if approval == "APPROVED" and decision["needs_role_check"] and not decision["review_notes"]:
            problems.append("该题表头存疑，放行前须在备注里写明列语义的核对结论")
        return problems

    def decide(self, index: int, approval: str, privacy: str, difficulty: str, notes: str) -> dict:
        """Record one reviewed row in memory and in the workbook."""

        row = self.rows[index]
        decision = {
            "approval_status": approval.strip().upper(),
            "privacy_review_status": privacy.strip().upper(),
            "difficulty": difficulty.strip(),
            "review_notes": notes.strip(),
            "needs_role_check": row["needs_role_check"].upper() == "YES",
        }
        problems = self.reasons(decision)
        if not problems and (not decision["approval_status"] or not decision["privacy_review_status"]):
            problems.append("内容结论与隐私结论必须同时给出")
        if not problems:
            problems = validate_decision(decision)
        if problems:
            raise ValueError("；".join(problems))

        for header in EDITABLE:
            row[header] = decision[header]
        sheet = self.workbook[row["project"]]
        headers = self.headers[row["project"]]
        for header in EDITABLE:
            sheet.cell(row=row["row"], column=headers.index(header) + 1, value=row[header] or None)
        self.save()
        return self.progress()

    def save(self) -> None:
        # An atomic replace would silently resurrect a workbook that was moved or
        # deleted after load, sending the reviewer's decisions into a file nobody
        # is tracking. Refuse instead, so the session can be pointed at the real one.
        if not self.path.is_file():
            raise ValueError(f"{self.path} no longer exists; decision was not saved")
        directory = self.path.parent
        handle, temporary = tempfile.mkstemp(prefix=".qa-approval-", suffix=".xlsx", dir=directory)
        os.close(handle)
        try:
            self.workbook.save(temporary)
            os.replace(temporary, self.path)
        finally:
            if Path(temporary).exists():
                Path(temporary).unlink()

    # ---------- progress ----------

    def is_decided(self, row: dict) -> bool:
        return (
            row["approval_status"] in APPROVAL_VALUES
            and row["privacy_review_status"] in PRIVACY_VALUES
        )

    def next_undecided(self, start: int) -> int | None:
        order = list(range(start, len(self.rows))) + list(range(0, start))
        for index in order:
            if not self.is_decided(self.rows[index]):
                return index
        return None

    def progress(self) -> dict:
        per_project: dict[str, dict] = {}
        for row in self.rows:
            entry = per_project.setdefault(row["project"], {"total": 0, "decided": 0, "approved": 0})
            entry["total"] += 1
            if self.is_decided(row):
                entry["decided"] += 1
                if row["approval_status"] == "APPROVED":
                    entry["approved"] += 1
        decided = sum(entry["decided"] for entry in per_project.values())
        approved = sum(entry["approved"] for entry in per_project.values())
        return {
            "total": len(self.rows),
            "decided": decided,
            "remaining": len(self.rows) - decided,
            "approved": approved,
            "rejected": decided - approved,
            "projects": per_project,
        }
