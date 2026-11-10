from __future__ import annotations

import argparse
import json
import sys
from collections import Counter
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from seac_chunking.units import document_units


def project_summary(project: dict, workbench_root: Path) -> dict:
    project_id = project["project_id"]
    project_dir = workbench_root / project_id
    corpus_path = project_dir / "corpus.json"
    documents = json.loads(corpus_path.read_text(encoding="utf-8"))
    units = sum(len(document_units(document)) for document in documents)
    extraction_path = project_dir / "extraction_manifest.json"
    extraction = (
        json.loads(extraction_path.read_text(encoding="utf-8"))
        if extraction_path.is_file()
        else {}
    )
    annotation_path = project_dir / "annotation_pack" / "annotation_summary.json"
    annotation = (
        json.loads(annotation_path.read_text(encoding="utf-8"))
        if annotation_path.is_file()
        else {}
    )
    substantive_skips = [
        item
        for item in extraction.get("skipped", [])
        if item.get("reason") != "temporary_or_system_file"
    ]
    return {
        "project_id": project_id,
        "source_dir": project["source_dir"],
        "split": project["split"],
        "document_count": len(documents),
        "block_count": sum(len(document["blocks"]) for document in documents),
        "unit_count": units,
        "approx_token_count": sum(document["tokens_approx"] for document in documents),
        "character_count": sum(document["characters"] for document in documents),
        "paragraph_count": sum(document["paragraphs"] for document in documents),
        "table_count": sum(document["tables"] for document in documents),
        "table_row_count": sum(document["table_rows"] for document in documents),
        "heading_count": sum(document["headings"] for document in documents),
        "document_types": dict(sorted(Counter(document["document_type"] for document in documents).items())),
        "converted_source_count": sum(
            item.get("source_mode") == "converted_copy"
            for item in extraction.get("processed", [])
        ),
        "substantive_skips": substantive_skips,
        "boundary_sample_count": annotation.get("boundary_sample_count", 0),
        "qa_draft_count": annotation.get("qa_draft_count", 0),
        "annotation_status": annotation.get(
            "formal_status", "GOLD_READY" if project_id == "P01" else "NOT_PREPARED"
        ),
    }


def markdown_report(inventory: dict) -> str:
    lines = [
        "# Industrial SEAC corpus inventory",
        "",
        f'Protocol: `{inventory["protocol_version"]}`',
        "",
        "Raw enterprise documents remain confidential and are not publication artifacts.",
        "",
        "| Project | Split | Docs | Blocks | Units | Approx. tokens | Boundary sample | QA draft/KEEP | Status |",
        "|---|---:|---:|---:|---:|---:|---:|---:|---|",
    ]
    for project in inventory["projects"]:
        qa_count = project["qa_keep_count"] if "qa_keep_count" in project else project["qa_draft_count"]
        lines.append(
            f'| {project["project_id"]} | {project["split"]} | {project["document_count"]} '
            f'| {project["block_count"]} | {project["unit_count"]} | {project["approx_token_count"]} '
            f'| {project.get("boundary_gold_count", project["boundary_sample_count"])} | {qa_count} '
            f'| {project["annotation_status"]} |'
        )
    total = inventory["totals"]
    lines.extend(
        [
            "",
            f'Across all projects: {total["document_count"]} documents, {total["block_count"]} blocks, '
            f'{total["unit_count"]} retrieval units, and approximately {total["approx_token_count"]} tokens.',
            "",
            f'Prepared blind boundary samples: {total["boundary_sample_count"]}; '
            f'QA drafts pending human review: {total["qa_draft_count"]}.',
            "",
            "P04-P10 retrieval metrics remain locked until annotation and privacy audits pass.",
        ]
    )
    return "\n".join(lines) + "\n"


def main() -> None:
    parser = argparse.ArgumentParser(description="Build a paper-ready industrial corpus inventory.")
    parser.add_argument("--config", default="configs/industrial_projects.json")
    parser.add_argument("--workbench-root", default="workbench")
    parser.add_argument("--json-output", default="experiments/industrial_corpus_inventory.json")
    parser.add_argument("--markdown-output", default="experiments/industrial_corpus_inventory.md")
    args = parser.parse_args()

    config = json.loads(Path(args.config).read_text(encoding="utf-8"))
    projects = [project_summary(project, Path(args.workbench_root)) for project in config["projects"]]
    p01 = projects[0]
    boundary_path = Path(args.workbench_root) / "P01" / "candidate_breakpoints_adjudicated.json"
    qa_path = Path(args.workbench_root) / "P01" / "qa_review_set_keep_unit_aligned.json"
    if boundary_path.is_file():
        gold = json.loads(boundary_path.read_text(encoding="utf-8"))
        p01["boundary_gold_count"] = len(gold)
        p01["boundary_gold_labels"] = dict(sorted(Counter(item["human_label"] for item in gold).items()))
    if qa_path.is_file():
        p01["qa_keep_count"] = len(json.loads(qa_path.read_text(encoding="utf-8")))
    p01["annotation_status"] = "GOLD_READY"

    totals = {
        key: sum(project[key] for project in projects)
        for key in (
            "document_count",
            "block_count",
            "unit_count",
            "approx_token_count",
            "character_count",
            "paragraph_count",
            "table_count",
            "table_row_count",
            "heading_count",
            "converted_source_count",
            "boundary_sample_count",
            "qa_draft_count",
        )
    }
    inventory = {
        "protocol_version": "industrial-seac-v1.0",
        "projects": projects,
        "totals": totals,
    }
    json_path = Path(args.json_output)
    markdown_path = Path(args.markdown_output)
    json_path.write_text(json.dumps(inventory, ensure_ascii=False, indent=2), encoding="utf-8")
    markdown_path.write_text(markdown_report(inventory), encoding="utf-8")
    print(json.dumps(totals, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
