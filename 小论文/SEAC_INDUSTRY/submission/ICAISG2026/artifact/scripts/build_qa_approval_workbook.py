"""Build the human QA approval workbook for the multi-evidence candidate set.

The workbook gathers every candidate a human must decide on: drafts approved in
the first content pre-review, drafts released under the multi-evidence model,
verified rewrite proposals, and supplementary drafts authored to close per-project
shortfalls. Each row carries the provenance a reviewer needs to judge it, and the
approval and privacy columns start empty because no automated step may set them.
"""

from __future__ import annotations

import argparse
import hashlib
import json
from collections import Counter
from pathlib import Path

from openpyxl import Workbook
from openpyxl.styles import Alignment, Font, PatternFill
from openpyxl.utils import get_column_letter
from openpyxl.worksheet.datavalidation import DataValidation

PROTOCOL_VERSION = "industrial-seac-qa-approval-v1.0"
USABLE_STATES = {"APPROVED_SINGLE_ROUND", "CANDIDATE_MULTI_EVIDENCE", "CANDIDATE_REWRITE"}
HEADERS = [
    "qa_key",
    "project_id",
    "candidate_state",
    "question_type",
    "question",
    "reference_answer",
    "gold_evidence_span",
    "evidence_occurrences",
    "evidence_documents",
    "gold_evidence_unit_ids",
    "answer_grounding",
    "needs_role_check",
    "prereview_notes",
    "suggested_privacy_flags",
    "suggested_difficulty",
    "approval_status",
    "privacy_review_status",
    "difficulty",
    "review_notes",
]
WIDTHS = [14, 9, 24, 16, 54, 40, 54, 12, 30, 40, 20, 14, 46, 30, 12, 16, 20, 12, 34]
INPUT_START = 16


def display(path: Path, root: Path) -> str:
    try:
        return str(path.relative_to(root))
    except ValueError:
        return str(path)


def text_list(values: list[str], limit: int = 6) -> str:
    shown = values[:limit]
    suffix = f" (+{len(values) - limit})" if len(values) > limit else ""
    return ", ".join(shown) + suffix


def collect(root: Path, readjudication_root: Path, supplement_root: Path, projects: list[str]) -> dict[str, list[dict]]:
    rows: dict[str, list[dict]] = {}
    for project in projects:
        candidates = json.loads(
            (readjudication_root / f"qa_candidates_{project}.json").read_text(encoding="utf-8")
        )
        collected = []
        for item in candidates:
            if item["candidate_state"] not in USABLE_STATES:
                continue
            equivalence = item.get("evidence_equivalence", {})
            collected.append(
                {
                    "qa_key": item["qa_key"],
                    "project_id": project,
                    "candidate_state": item["candidate_state"],
                    "question_type": item.get("question_type", ""),
                    "question": item.get("question", ""),
                    "reference_answer": item.get("reference_answer", ""),
                    "gold_evidence_span": item.get("gold_evidence_span", ""),
                    "evidence_occurrences": equivalence.get("occurrence_count", 1),
                    "evidence_documents": text_list(equivalence.get("documents", [])),
                    "gold_evidence_unit_ids": text_list(
                        item.get("gold_evidence_unit_ids") or [item.get("gold_evidence_unit_id", "")]
                    ),
                    "answer_grounding": item.get("proposed_rewrite", {}).get("answer_grounding", ""),
                    "needs_role_check": "",
                    "prereview_notes": item.get("prereview_notes", ""),
                    "suggested_privacy_flags": ", ".join(item.get("suggested_privacy_flags", [])),
                    "suggested_difficulty": item.get("suggested_difficulty", ""),
                }
            )

        supplement_path = supplement_root / f"qa_supplement_{project}.json"
        if supplement_path.is_file():
            for item in json.loads(supplement_path.read_text(encoding="utf-8")):
                equivalence = item.get("evidence_equivalence", {})
                collected.append(
                    {
                        "qa_key": item["qa_key"],
                        "project_id": project,
                        "candidate_state": item["candidate_state"],
                        "question_type": item.get("question_type", ""),
                        "question": item.get("question", ""),
                        "reference_answer": item.get("reference_answer", ""),
                        "gold_evidence_span": item.get("gold_evidence_span", ""),
                        "evidence_occurrences": equivalence.get("occurrence_count", 1),
                        "evidence_documents": text_list(equivalence.get("documents", [])),
                        "gold_evidence_unit_ids": text_list(item.get("gold_evidence_unit_ids", [])),
                        "answer_grounding": item.get("answer_grounding", ""),
                        "needs_role_check": "YES" if item.get("needs_role_check") else "",
                        "prereview_notes": "",
                        "suggested_privacy_flags": "",
                        "suggested_difficulty": "",
                    }
                )
        rows[project] = collected
    return rows


def write_guide(workbook: Workbook) -> None:
    sheet = workbook.create_sheet("Guide")
    entries = [
        ("SEAC 检索 QA 人工终审工作簿", ""),
        ("", ""),
        ("用途", "对多证据候选集做人工内容终审与隐私放行；自动流程不得填写这两列。"),
        ("只填四列", "approval_status、privacy_review_status、difficulty、review_notes。"),
        ("approval_status", "APPROVED 或 REJECTED，不得留空。"),
        ("privacy_review_status", "NO_RISK / ANONYMIZED / CLEARED / REJECTED，需由获授权人员判定。"),
        ("difficulty", "Easy / Medium / Hard。suggested_difficulty 仅为预审建议。"),
        ("多证据模型", "evidence_occurrences 大于 1 表示同一原文在语料多处出现，检索命中任一处即算正确；请确认这些位置的答案确实一致。"),
        ("candidate_state", "APPROVED_SINGLE_ROUND 预审通过；CANDIDATE_MULTI_EVIDENCE 按多证据模型放行；CANDIDATE_REWRITE 题干或答案经改写并通过机械校验；CANDIDATE_SUPPLEMENT 为补写草稿。"),
        ("needs_role_check", "YES 表示证据所在表格的表头有重复或占位列名，答案对列语义的归属需人工确认。"),
        ("answer_grounding", "LITERAL 答案是证据子串；PARAPHRASE_GROUNDED 答案改述自证据，各实质片段可回溯。"),
        ("拒绝原则", "证据不唯一、答案需外部推断、题干无法自足时一律 REJECTED，不得为凑题数放行。"),
        ("保密要求", "含企业文档片段，仅限受控环境内部使用，不得作为论文附件或上传公开平台。"),
    ]
    for index, (key, value) in enumerate(entries, 1):
        sheet.cell(row=index, column=1, value=key).font = Font(bold=index == 1, size=13 if index == 1 else 11)
        sheet.cell(row=index, column=2, value=value).alignment = Alignment(wrap_text=True, vertical="top")
    sheet.column_dimensions["A"].width = 22
    sheet.column_dimensions["B"].width = 104


def write_project_sheet(workbook: Workbook, project: str, rows: list[dict]) -> None:
    sheet = workbook.create_sheet(project)
    header_fill = PatternFill("solid", fgColor="DDEBF7")
    input_fill = PatternFill("solid", fgColor="FFF2CC")
    for column, header in enumerate(HEADERS, 1):
        cell = sheet.cell(row=1, column=column, value=header)
        cell.font = Font(bold=True)
        cell.fill = header_fill
        sheet.column_dimensions[get_column_letter(column)].width = WIDTHS[column - 1]

    for index, row in enumerate(rows, 2):
        for column, header in enumerate(HEADERS, 1):
            value = row.get(header, "")
            cell = sheet.cell(row=index, column=column, value=value)
            cell.alignment = Alignment(wrap_text=True, vertical="top")
            if column >= INPUT_START:
                cell.fill = input_fill

    approval = DataValidation(type="list", formula1='"APPROVED,REJECTED"', allow_blank=True)
    privacy = DataValidation(
        type="list", formula1='"NO_RISK,ANONYMIZED,CLEARED,REJECTED"', allow_blank=True
    )
    difficulty = DataValidation(type="list", formula1='"Easy,Medium,Hard"', allow_blank=True)
    for rule in (approval, privacy, difficulty):
        sheet.add_data_validation(rule)
    last = len(rows) + 1
    approval.add(f"P2:P{last}")
    privacy.add(f"Q2:Q{last}")
    difficulty.add(f"R2:R{last}")
    sheet.freeze_panes = "A2"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--readjudication-root", default="outputs/qa_readjudication_v1")
    parser.add_argument("--supplement-root", default="outputs/qa_supplement_v1")
    parser.add_argument("--output-root", default="outputs/qa_approval_v1")
    parser.add_argument("--manifest", default="configs/formal_run_manifest.json")
    parser.add_argument(
        "--projects", nargs="*", default=["P02", "P03", "P04", "P05", "P06", "P07", "P08", "P09", "P10"]
    )
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    output_root = root / args.output_root
    output_root.mkdir(parents=True, exist_ok=True)
    minimums = {
        project["project_id"]: project.get("minimum_qa_count", 0)
        for project in json.loads((root / args.manifest).read_text(encoding="utf-8"))["projects"]
    }

    rows = collect(
        root, root / args.readjudication_root, root / args.supplement_root, args.projects
    )
    workbook = Workbook()
    workbook.remove(workbook.active)
    write_guide(workbook)
    manifest = {
        "protocol_version": PROTOCOL_VERSION,
        "evidence_model": "multi_evidence_any_hit",
        "approval_decisions_included": False,
        "privacy_decisions_included": False,
        "projects": {},
    }
    for project in args.projects:
        write_project_sheet(workbook, project, rows[project])
        states = Counter(row["candidate_state"] for row in rows[project])
        manifest["projects"][project] = {
            "candidate_count": len(rows[project]),
            "states": dict(sorted(states.items())),
            "multi_evidence_records": sum(1 for row in rows[project] if row["evidence_occurrences"] > 1),
            "needs_role_check": sum(1 for row in rows[project] if row["needs_role_check"] == "YES"),
            "minimum_qa_count": minimums.get(project, 0),
            "meets_minimum": len(rows[project]) >= minimums.get(project, 0),
        }

    target = output_root / "SEAC_QA_approval.xlsx"
    workbook.save(target)
    manifest["workbook_sha256"] = hashlib.sha256(target.read_bytes()).hexdigest()
    manifest["candidate_total"] = sum(len(items) for items in rows.values())
    (output_root / "approval_manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(
        json.dumps(
            {
                "workbook": display(target, root),
                "candidate_total": manifest["candidate_total"],
                "projects": {
                    project: {
                        "candidates": info["candidate_count"],
                        "minimum": info["minimum_qa_count"],
                        "meets_minimum": info["meets_minimum"],
                    }
                    for project, info in manifest["projects"].items()
                },
            },
            ensure_ascii=False,
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
