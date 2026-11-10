"""Build the blind human verification workbook for LLM pre-labelled boundaries.

Two disjoint row groups are drawn per project:

- `AGREEMENT_SAMPLE`: a deterministic, stratum-proportional random sample used to
  estimate human-LLM agreement and Cohen's kappa. It is drawn independently of
  the pre-label values, so the estimate is not biased toward easy or hard cases.
- `FLAGGED_REVIEW`: every candidate the pre-labeller marked
  `NEEDS_HUMAN_REVIEW` that the random sample did not already contain. These rows
  must be decided by a human but are reported separately and excluded from the
  agreement estimate.

The workbook deliberately omits the pre-label, its notes, and every method
output, so the verifier stays blind.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import random
from collections import Counter, defaultdict
from pathlib import Path

from openpyxl import Workbook
from openpyxl.styles import Alignment, Font, PatternFill
from openpyxl.utils import get_column_letter
from openpyxl.worksheet.datavalidation import DataValidation

PROTOCOL_VERSION = "industrial-seac-prelabel-verification-v1.0"
SEED = 20260804
HEADERS = [
    "candidate_id",
    "project_id",
    "row_group",
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
WIDTHS = [14, 9, 18, 34, 16, 22, 30, 14, 52, 30, 14, 52, 14, 18, 34]


def stratified_sample(records: list[dict], size: int, seed_key: str) -> list[dict]:
    by_stratum: dict[str, list[dict]] = defaultdict(list)
    for record in records:
        by_stratum[record["boundary_stratum"]].append(record)

    total = len(records)
    quotas = {}
    for stratum, items in sorted(by_stratum.items()):
        quotas[stratum] = min(len(items), int(round(size * len(items) / total)))

    # Repair rounding drift deterministically, largest strata first.
    order = sorted(by_stratum, key=lambda name: (-len(by_stratum[name]), name))
    while sum(quotas.values()) != size:
        drift = size - sum(quotas.values())
        changed = False
        for stratum in order:
            room = len(by_stratum[stratum]) - quotas[stratum]
            if drift > 0 and room > 0:
                quotas[stratum] += 1
                changed = True
            elif drift < 0 and quotas[stratum] > 0:
                quotas[stratum] -= 1
                changed = True
            if sum(quotas.values()) == size:
                break
        if not changed:
            break

    selected = []
    for stratum in sorted(by_stratum):
        items = sorted(by_stratum[stratum], key=lambda record: record["candidate_id"])
        rng = random.Random(f"{seed_key}:{stratum}")
        selected.extend(rng.sample(items, quotas[stratum]))
    return sorted(selected, key=lambda record: record["candidate_id"])


def write_guide(workbook: Workbook, sample_size: int) -> None:
    sheet = workbook.create_sheet("Guide")
    rows = [
        ("SEAC 边界预标人工验证工作簿", ""),
        ("", ""),
        ("用途", "对 LLM 预标结果做独立人工验证，用于测量人机一致率并修正错误标签。"),
        ("判断问题", "长度允许时，右侧单元是否应开始一个新的、语义上可独立检索的父块？"),
        ("允许标签", "BREAK / NO_BREAK / NOISE"),
        ("review_status", "COMPLETE 或 NEEDS_DISCUSSION，不得留空。"),
        ("盲态要求", "本工作簿不含任何 LLM 预标、SEAC 预测或检索结果，请勿在填写前查阅预标文件。"),
        ("row_group", f"AGREEMENT_SAMPLE 为每项目 {sample_size} 条分层随机样本，用于统计一致率；FLAGGED_REVIEW 为预标阶段标记待复核的行，单独报告。"),
        ("填写范围", "只修改 human_label、review_status、review_notes 三列。"),
        ("判定规则", "见 docs/annotation_and_privacy_protocol.md 第 4 节；不得因文本接近 512 token 而判 BREAK。"),
        ("保密要求", "含企业文档片段，仅限受控环境内部使用，不得作为论文附件或上传公开标注平台。"),
    ]
    for index, (key, value) in enumerate(rows, 1):
        sheet.cell(row=index, column=1, value=key).font = Font(bold=index == 1, size=13 if index == 1 else 11)
        sheet.cell(row=index, column=2, value=value).alignment = Alignment(wrap_text=True, vertical="top")
    sheet.column_dimensions["A"].width = 16
    sheet.column_dimensions["B"].width = 96


def write_project_sheet(workbook: Workbook, project: str, rows: list[dict]) -> None:
    sheet = workbook.create_sheet(project)
    header_fill = PatternFill("solid", fgColor="DDEBF7")
    input_fill = PatternFill("solid", fgColor="FFF2CC")
    for column, header in enumerate(HEADERS, 1):
        cell = sheet.cell(row=1, column=column, value=header)
        cell.font = Font(bold=True)
        cell.fill = header_fill
        sheet.column_dimensions[get_column_letter(column)].width = WIDTHS[column - 1]

    for index, record in enumerate(rows, 2):
        values = [
            record["candidate_id"],
            record["project_id"],
            record["row_group"],
            record["source_document"],
            record["document_type"],
            record["boundary_stratum"],
            record["left_section"],
            record["left_block_type"],
            record["left_evidence"],
            record["right_section"],
            record["right_block_type"],
            record["right_evidence"],
            "",
            "UNLABELED",
            "",
        ]
        for column, value in enumerate(values, 1):
            cell = sheet.cell(row=index, column=column, value=value)
            cell.alignment = Alignment(wrap_text=True, vertical="top")
            if column >= 13:
                cell.fill = input_fill

    label_rule = DataValidation(type="list", formula1='"BREAK,NO_BREAK,NOISE"', allow_blank=True)
    status_rule = DataValidation(
        type="list", formula1='"UNLABELED,COMPLETE,NEEDS_DISCUSSION"', allow_blank=False
    )
    sheet.add_data_validation(label_rule)
    sheet.add_data_validation(status_rule)
    last = len(rows) + 1
    label_rule.add(f"M2:M{last}")
    status_rule.add(f"N2:N{last}")
    sheet.freeze_panes = "A2"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--prelabel-root", default="outputs/llm_prelabel_v1")
    parser.add_argument("--output-root", default="outputs/prelabel_verification_v1")
    parser.add_argument("--sample-size", type=int, default=100)
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    prelabel_root = root / args.prelabel_root
    output_root = root / args.output_root
    output_root.mkdir(parents=True, exist_ok=True)
    manifest_source = json.loads((prelabel_root / "batch_manifest.json").read_text(encoding="utf-8"))

    workbook = Workbook()
    workbook.remove(workbook.active)
    write_guide(workbook, args.sample_size)

    manifest = {
        "protocol_version": PROTOCOL_VERSION,
        "seed": SEED,
        "sample_size_per_project": args.sample_size,
        "verifier_is_blind_to_prelabel": True,
        "public_release_allowed": False,
        "projects": {},
    }
    for project in manifest_source["projects"]:
        records = json.loads((prelabel_root / f"boundary_llm_prelabel_{project}.json").read_text(encoding="utf-8"))
        sample = stratified_sample(records, args.sample_size, f"{SEED}:{project}")
        sample_ids = {record["candidate_id"] for record in sample}
        flagged = [
            record
            for record in records
            if record["prelabel_flag"] == "NEEDS_HUMAN_REVIEW" and record["candidate_id"] not in sample_ids
        ]
        rows = [{**record, "row_group": "AGREEMENT_SAMPLE"} for record in sample]
        rows += [{**record, "row_group": "FLAGGED_REVIEW"} for record in flagged]
        write_project_sheet(workbook, project, rows)
        manifest["projects"][project] = {
            "agreement_sample_size": len(sample),
            "flagged_review_size": len(flagged),
            "row_total": len(rows),
            "agreement_sample_by_stratum": dict(sorted(Counter(item["boundary_stratum"] for item in sample).items())),
            "agreement_sample_ids": [record["candidate_id"] for record in sample],
            "flagged_review_ids": [record["candidate_id"] for record in flagged],
        }

    target = output_root / "SEAC_boundary_prelabel_verification.xlsx"
    workbook.save(target)
    digest = hashlib.sha256(target.read_bytes()).hexdigest()
    manifest["workbook_sha256"] = digest
    manifest["row_total"] = sum(info["row_total"] for info in manifest["projects"].values())
    (output_root / "verification_manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(
        json.dumps(
            {
                "workbook": str(target.relative_to(root)),
                "row_total": manifest["row_total"],
                "projects": {
                    project: {"sample": info["agreement_sample_size"], "flagged": info["flagged_review_size"]}
                    for project, info in manifest["projects"].items()
                },
            },
            ensure_ascii=False,
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
