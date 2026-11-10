"""Audit LLM pre-labels for rule conformance and cross-project consistency.

The audit is mechanical: it reports where labels contradict the deterministic
parts of `outputs/llm_prelabel_v1/labeling_rules_applied.md` and where the
label distribution of one project departs from the pooled distribution of the
same stratum. It never edits labels.
"""

from __future__ import annotations

import argparse
import json
import re
from collections import Counter, defaultdict
from pathlib import Path

BARE_TEMPLATE_LABELS = {
    "输入",
    "输出",
    "处理",
    "处理逻辑",
    "返回",
    "返回信息",
    "请求信息",
    "目录",
}
PAGE_NUMBER_TAIL = re.compile(r"\s\d{1,4}$")


def normalize(text: str) -> str:
    return re.sub(r"\s+", "", text or "")


def is_bare_template(text: str) -> bool:
    return normalize(text) in BARE_TEMPLATE_LABELS


def has_page_number_tail(record: dict, side: str) -> bool:
    if record[f"{side}_block_type"] != "heading":
        return False
    return bool(PAGE_NUMBER_TAIL.search((record[f"{side}_evidence"] or "").strip()))


def is_parent_child_heading(record: dict) -> bool:
    if record["left_block_type"] != "heading" or record["right_block_type"] != "heading":
        return False
    left, right = record["left_section"] or "", record["right_section"] or ""
    return bool(left) and right.startswith(left) and right != left


def rule_checks(record: dict) -> list[str]:
    label = record["prelabel"]
    findings = []
    bare = is_bare_template(record["left_evidence"]) or is_bare_template(record["right_evidence"])
    if bare and label != "NOISE":
        findings.append("bare_template_label_not_noise")
    if record["boundary_stratum"] == "inside_table" and label == "BREAK" and not bare:
        findings.append("inside_table_marked_break")
    if (has_page_number_tail(record, "left") or has_page_number_tail(record, "right")) and label != "NOISE":
        findings.append("page_numbered_heading_not_noise")
    if is_parent_child_heading(record) and label == "BREAK" and not bare:
        findings.append("parent_child_heading_marked_break")
    return findings


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--prelabel-root", default="outputs/llm_prelabel_v1")
    parser.add_argument("--report-json", default="outputs/llm_prelabel_v1/prelabel_consistency.json")
    parser.add_argument("--report-markdown", default="outputs/llm_prelabel_v1/prelabel_consistency.md")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    prelabel_root = root / args.prelabel_root
    manifest = json.loads((prelabel_root / "batch_manifest.json").read_text(encoding="utf-8"))

    findings: dict[str, list[str]] = defaultdict(list)
    unflagged_findings: dict[str, list[str]] = defaultdict(list)
    stratum_totals: dict[str, Counter[str]] = defaultdict(Counter)
    project_stratum: dict[str, dict[str, Counter[str]]] = defaultdict(lambda: defaultdict(Counter))
    project_totals: dict[str, Counter[str]] = defaultdict(Counter)
    flagged: dict[str, int] = {}

    for project in manifest["projects"]:
        records = json.loads((prelabel_root / f"boundary_llm_prelabel_{project}.json").read_text(encoding="utf-8"))
        flagged[project] = sum(item["prelabel_flag"] == "NEEDS_HUMAN_REVIEW" for item in records)
        for record in records:
            label = record["prelabel"]
            stratum = record["boundary_stratum"]
            stratum_totals[stratum][label] += 1
            project_stratum[project][stratum][label] += 1
            project_totals[project][label] += 1
            for finding in rule_checks(record):
                findings[finding].append(record["candidate_id"])
                if record["prelabel_flag"] != "NEEDS_HUMAN_REVIEW":
                    unflagged_findings[finding].append(record["candidate_id"])

    outliers = []
    for stratum, pooled in stratum_totals.items():
        pooled_total = sum(pooled.values())
        for label in ("BREAK", "NO_BREAK", "NOISE"):
            pooled_rate = pooled[label] / pooled_total if pooled_total else 0.0
            for project, strata in project_stratum.items():
                counts = strata.get(stratum)
                if not counts:
                    continue
                total = sum(counts.values())
                if total < 20:
                    continue
                rate = counts[label] / total
                if abs(rate - pooled_rate) >= 0.30:
                    outliers.append(
                        {
                            "project_id": project,
                            "stratum": stratum,
                            "label": label,
                            "project_rate": round(rate, 3),
                            "pooled_rate": round(pooled_rate, 3),
                            "project_count": total,
                        }
                    )

    report = {
        "protocol_version": "industrial-seac-llm-prelabel-consistency-v1.0",
        "candidate_total": sum(sum(counts.values()) for counts in project_totals.values()),
        "rule_findings": {name: {"count": len(ids), "examples": ids[:10]} for name, ids in sorted(findings.items())},
        "rule_finding_total": sum(len(ids) for ids in findings.values()),
        "unflagged_rule_findings": {
            name: {"count": len(ids), "examples": ids[:10]} for name, ids in sorted(unflagged_findings.items())
        },
        "unflagged_rule_finding_total": sum(len(ids) for ids in unflagged_findings.values()),
        "labels_by_stratum": {
            stratum: dict(sorted(counts.items())) for stratum, counts in sorted(stratum_totals.items())
        },
        "labels_by_project": {
            project: dict(sorted(counts.items())) for project, counts in sorted(project_totals.items())
        },
        "needs_human_review_by_project": dict(sorted(flagged.items())),
        "distribution_outliers": sorted(outliers, key=lambda item: -abs(item["project_rate"] - item["pooled_rate"])),
    }

    lines = [
        "# LLM 预标一致性审计",
        "",
        f'- 候选总数：{report["candidate_total"]}',
        f'- 规则冲突条数：{report["rule_finding_total"]}'
        f'（其中未标复核 {report["unflagged_rule_finding_total"]} 条）',
        f'- 分层分布离群项：{len(outliers)}',
        "",
        "## 各项目标签分布",
        "",
        "| 项目 | BREAK | NO_BREAK | NOISE | 待人工复核 |",
        "|---|---:|---:|---:|---:|",
    ]
    for project, counts in report["labels_by_project"].items():
        lines.append(
            f'| {project} | {counts.get("BREAK", 0)} | {counts.get("NO_BREAK", 0)} | '
            f'{counts.get("NOISE", 0)} | {flagged[project]} |'
        )
    lines += ["", "## 各分层标签分布", "", "| 分层 | BREAK | NO_BREAK | NOISE |", "|---|---:|---:|---:|"]
    for stratum, counts in report["labels_by_stratum"].items():
        lines.append(
            f'| {stratum} | {counts.get("BREAK", 0)} | {counts.get("NO_BREAK", 0)} | {counts.get("NOISE", 0)} |'
        )
    if report["rule_findings"]:
        lines += ["", "## 规则冲突", "", "| 类型 | 条数 | 示例 |", "|---|---:|---|"]
        for name, info in report["rule_findings"].items():
            lines.append(f'| {name} | {info["count"]} | {", ".join(info["examples"][:5])} |')
    if outliers:
        lines += ["", "## 分层分布离群项", "", "| 项目 | 分层 | 标签 | 项目占比 | 汇总占比 |", "|---|---|---|---:|---:|"]
        for item in report["distribution_outliers"]:
            lines.append(
                f'| {item["project_id"]} | {item["stratum"]} | {item["label"]} | '
                f'{item["project_rate"]:.3f} | {item["pooled_rate"]:.3f} |'
            )

    (root / args.report_json).write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    (root / args.report_markdown).write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(
        json.dumps(
            {
                "candidate_total": report["candidate_total"],
                "rule_finding_total": report["rule_finding_total"],
                "rule_findings": {name: info["count"] for name, info in report["rule_findings"].items()},
                "unflagged_rule_finding_total": report["unflagged_rule_finding_total"],
                "outlier_count": len(outliers),
            },
            ensure_ascii=False,
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
