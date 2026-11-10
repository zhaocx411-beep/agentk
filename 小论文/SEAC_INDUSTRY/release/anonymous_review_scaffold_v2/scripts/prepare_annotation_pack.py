from __future__ import annotations

import argparse
import hashlib
import json
import sys
from collections import Counter, defaultdict
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from build_generic_review_dataset import questions
from boundary_candidate_utils import shorten
from seac_chunking.text import is_noise_text
from seac_chunking.units import document_units


STRATUM_WEIGHTS = {
    "heading_transition": 0.18,
    "section_transition": 0.14,
    "same_section_continuity": 0.23,
    "inside_table": 0.23,
    "table_transition": 0.12,
    "noise_candidate": 0.10,
}


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def stable_key(seed: int, value: str) -> str:
    return hashlib.sha256(f"{seed}:{value}".encode("utf-8")).hexdigest()


def boundary_stratum(left, right) -> str:
    if is_noise_text(left.text) or is_noise_text(right.text):
        return "noise_candidate"
    if left.protected_group and left.protected_group == right.protected_group:
        return "inside_table"
    if left.block_type == "table_row" or right.block_type == "table_row":
        return "table_transition"
    if right.block_type == "heading":
        return "heading_transition"
    if left.heading_path != right.heading_path:
        return "section_transition"
    return "same_section_continuity"


def enumerate_boundaries(documents: list[dict], project_id: str) -> list[dict]:
    pool = []
    for document in documents:
        units = document_units(document)
        for position in range(1, len(units)):
            left, right = units[position - 1], units[position]
            identity = f"{document['file_name']}::{left.unit_id}::{right.unit_id}"
            pool.append(
                {
                    "sampling_identity": identity,
                    "project_id": project_id,
                    "source_document": document["file_name"],
                    "document_type": document["document_type"],
                    "boundary_position": position,
                    "boundary_stratum": boundary_stratum(left, right),
                    "left_unit_id": left.unit_id,
                    "right_unit_id": right.unit_id,
                    "left_block_type": left.block_type,
                    "right_block_type": right.block_type,
                    "left_section": left.heading_path,
                    "right_section": right.heading_path,
                    "left_evidence": shorten(left.text),
                    "right_evidence": shorten(right.text),
                    "human_label": "",
                    "review_status": "UNLABELED",
                    "review_notes": "",
                }
            )
    return pool


def round_robin_sample(items: list[dict], limit: int, seed: int) -> list[dict]:
    by_document = defaultdict(list)
    for item in items:
        by_document[item["source_document"]].append(item)
    for document, rows in by_document.items():
        rows.sort(key=lambda row: stable_key(seed, row["sampling_identity"]))
    documents = sorted(by_document, key=lambda name: stable_key(seed, name))
    selected = []
    offsets = Counter()
    while len(selected) < limit:
        progressed = False
        for document in documents:
            offset = offsets[document]
            if offset >= len(by_document[document]):
                continue
            selected.append(by_document[document][offset])
            offsets[document] += 1
            progressed = True
            if len(selected) >= limit:
                break
        if not progressed:
            break
    return selected


def stratified_sample(pool: list[dict], target: int, seed: int) -> list[dict]:
    selected = []
    selected_ids = set()
    for stratum, weight in STRATUM_WEIGHTS.items():
        candidates = [item for item in pool if item["boundary_stratum"] == stratum]
        quota = round(target * weight)
        for item in round_robin_sample(candidates, quota, seed):
            selected.append(item)
            selected_ids.add(item["sampling_identity"])
    if len(selected) < target:
        remaining = [item for item in pool if item["sampling_identity"] not in selected_ids]
        selected.extend(round_robin_sample(remaining, target - len(selected), seed + 1))
    selected.sort(key=lambda item: stable_key(seed + 2, item["sampling_identity"]))
    for index, item in enumerate(selected, 1):
        item["candidate_id"] = f'{item["project_id"]}-BP{index:04d}'
        item.pop("sampling_identity")
    return selected


def annotator_copy(sample: list[dict], slot: str, seed: int) -> list[dict]:
    copied = [{**item, "annotator_slot": slot} for item in sample]
    return sorted(copied, key=lambda item: stable_key(seed, item["candidate_id"]))


def annotation_readme(project_id: str, boundary_count: int, qa_count: int) -> str:
    return f"""# {project_id} SEAC blind annotation pack

This pack is a paper benchmark input, not gold data yet.

Read `docs/annotation_and_privacy_protocol.md` before editing this pack. It is
the normative decision, adjudication, QA, and privacy-review guide.

## Boundary labels

- `BREAK`: the right unit should start a new semantically self-contained chunk.
- `NO_BREAK`: the two units should remain together when length constraints permit.
- `NOISE`: at least one side is navigation, a footer, an empty template, or otherwise not meaningful content.

Annotator A and B must work independently. Do not consult method outputs, model
predictions, or the other annotator. Fill `human_label`, `review_status`, and
`review_notes`; use `NEEDS_DISCUSSION` only when the evidence is genuinely
ambiguous. The {boundary_count} boundaries were selected deterministically with
document- and structure-stratified sampling.

## QA review

The {qa_count} QA records are rule-generated drafts. A reviewer must verify that
the question is natural, the answer is unique, the evidence span is sufficient,
and confidential details are removed. Table questions require a precise
`gold_evidence_unit_id`. Only records later marked `APPROVED` or `KEEP` may enter
formal retrieval evaluation. Set `privacy_review_status` to `NO_RISK`,
`ANONYMIZED`, or `CLEARED` for approved records; otherwise reject the record.
"""


def prepare_project(
    project: dict,
    workbench_root: Path,
    boundary_target: int,
    qa_limit: int,
    seed: int,
) -> dict:
    project_id = project["project_id"]
    project_dir = workbench_root / project_id
    corpus_path = project_dir / "corpus.json"
    if not corpus_path.is_file():
        raise FileNotFoundError(f"Missing corpus: {corpus_path}")
    documents = json.loads(corpus_path.read_text(encoding="utf-8"))
    pool = enumerate_boundaries(documents, project_id)
    sample = stratified_sample(pool, min(boundary_target, len(pool)), seed)
    qa_drafts = questions(documents, project_id, qa_limit)
    for item in qa_drafts:
        item.setdefault("privacy_review_status", "PENDING")
    pack_dir = project_dir / "annotation_pack"
    pack_dir.mkdir(parents=True, exist_ok=True)

    master_path = pack_dir / "boundary_blind_master.json"
    annotator_a_path = pack_dir / "boundary_blind_annotator_a.json"
    annotator_b_path = pack_dir / "boundary_blind_annotator_b.json"
    qa_path = pack_dir / "qa_draft.json"
    master_path.write_text(json.dumps(sample, ensure_ascii=False, indent=2), encoding="utf-8")
    annotator_a_path.write_text(
        json.dumps(annotator_copy(sample, "A", seed + 11), ensure_ascii=False, indent=2),
        encoding="utf-8",
    )
    annotator_b_path.write_text(
        json.dumps(annotator_copy(sample, "B", seed + 29), ensure_ascii=False, indent=2),
        encoding="utf-8",
    )
    qa_path.write_text(json.dumps(qa_drafts, ensure_ascii=False, indent=2), encoding="utf-8")
    (pack_dir / "README.md").write_text(
        annotation_readme(project_id, len(sample), len(qa_drafts)), encoding="utf-8"
    )

    summary = {
        "protocol_version": "industrial-seac-annotation-v1.0",
        "project_id": project_id,
        "split": project["split"],
        "sampling_seed": seed,
        "corpus_sha256": sha256(corpus_path),
        "boundary_pool_count": len(pool),
        "boundary_sample_count": len(sample),
        "pool_strata": dict(sorted(Counter(item["boundary_stratum"] for item in pool).items())),
        "sample_strata": dict(sorted(Counter(item["boundary_stratum"] for item in sample).items())),
        "sample_documents": dict(sorted(Counter(item["source_document"] for item in sample).items())),
        "qa_draft_count": len(qa_drafts),
        "qa_types": dict(sorted(Counter(item["question_type"] for item in qa_drafts).items())),
        "qa_unit_aligned_count": sum(bool(item.get("gold_evidence_unit_id")) for item in qa_drafts),
        "formal_status": "ANNOTATION_PENDING",
        "files": {
            path.name: sha256(path)
            for path in (master_path, annotator_a_path, annotator_b_path, qa_path)
        },
    }
    (pack_dir / "annotation_summary.json").write_text(
        json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    return summary


def main() -> None:
    parser = argparse.ArgumentParser(description="Prepare blind boundary and QA review packs.")
    parser.add_argument("--config", default="configs/industrial_projects.json")
    parser.add_argument("--workbench-root", default="workbench")
    parser.add_argument("--projects", nargs="*")
    parser.add_argument("--boundary-target", type=int, default=300)
    parser.add_argument("--qa-limit", type=int, default=36)
    parser.add_argument("--seed", type=int, default=20260804)
    args = parser.parse_args()

    config = json.loads(Path(args.config).read_text(encoding="utf-8"))
    requested = set(args.projects or [])
    projects = [
        project for project in config["projects"]
        if not requested or project["project_id"] in requested
    ]
    unknown = requested - {project["project_id"] for project in projects}
    if unknown:
        raise ValueError(f'Unknown project ids: {", ".join(sorted(unknown))}')
    summaries = [
        prepare_project(
            project,
            Path(args.workbench_root),
            args.boundary_target,
            args.qa_limit,
            args.seed,
        )
        for project in projects
    ]
    print(
        json.dumps(
            [
                {
                    "project_id": summary["project_id"],
                    "boundary_pool_count": summary["boundary_pool_count"],
                    "boundary_sample_count": summary["boundary_sample_count"],
                    "qa_draft_count": summary["qa_draft_count"],
                    "qa_unit_aligned_count": summary["qa_unit_aligned_count"],
                }
                for summary in summaries
            ],
            ensure_ascii=False,
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
