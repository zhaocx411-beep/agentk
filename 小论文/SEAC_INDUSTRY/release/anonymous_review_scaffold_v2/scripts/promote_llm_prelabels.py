"""Promote LLM pre-labels to the formal boundary gold standard after human verification.

The gate refuses to promote unless the human verification workbook is complete
and per-project human-LLM agreement reaches the configured Cohen's kappa
threshold. Promoted records keep per-row provenance: rows a human verified are
`HUMAN_VERIFIED`, the rest remain `LLM_PRELABEL` and must be disclosed as such
in the manuscript.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from collections import Counter, defaultdict
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from adjudicate_boundary_annotations import cohen_kappa  # noqa: E402
from boundary_noise_rules import classify as classify_noise  # noqa: E402

from openpyxl import load_workbook  # noqa: E402

PROTOCOL_VERSION = "industrial-seac-prelabel-promotion-v1.0"
LABELS = {"BREAK", "NO_BREAK", "NOISE"}
# Section 4.4 of the protocol tells an annotator to record a best judgment and
# flag the ambiguity rather than leave the row blank, so a flagged row is decided.
# It stays in the agreement statistic: dropping the rows an annotator found hard
# would systematically flatter the human-model agreement.
REVIEW_STATUSES = {"COMPLETE", "NEEDS_DISCUSSION"}


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def text(value: object) -> str:
    return "" if value is None else str(value).strip()


def read_verification(path: Path, projects: list[str]) -> dict[str, dict[str, dict]]:
    workbook = load_workbook(path, read_only=True, data_only=True)
    try:
        rows_by_project: dict[str, dict[str, dict]] = {}
        for project in projects:
            if project not in workbook.sheetnames:
                raise ValueError(f"verification workbook is missing sheet {project}")
            sheet = workbook[project]
            iterator = sheet.iter_rows(values_only=True)
            headers = [text(value) for value in next(iterator)]
            index = {header: position for position, header in enumerate(headers)}
            for field in ("candidate_id", "row_group", "human_label", "review_status", "review_notes"):
                if field not in index:
                    raise ValueError(f"{project}: verification sheet is missing column {field!r}")
            records: dict[str, dict] = {}
            for row in iterator:
                candidate_id = text(row[index["candidate_id"]])
                if not candidate_id:
                    continue
                if candidate_id in records:
                    raise ValueError(f"{project}: duplicate verification row {candidate_id}")
                records[candidate_id] = {
                    "row_group": text(row[index["row_group"]]),
                    "human_label": text(row[index["human_label"]]).upper(),
                    "review_status": text(row[index["review_status"]]).upper(),
                    "review_notes": text(row[index["review_notes"]]),
                }
            rows_by_project[project] = records
        return rows_by_project
    finally:
        workbook.close()


def audit_project(project: str, records: list[dict], verified: dict[str, dict], expected: dict) -> dict:
    blockers: list[str] = []
    expected_ids = set(expected["agreement_sample_ids"]) | set(expected["flagged_review_ids"])
    if set(verified) != expected_ids:
        missing = sorted(expected_ids - set(verified))
        extra = sorted(set(verified) - expected_ids)
        if missing:
            blockers.append(f"{len(missing)} verification rows are missing (examples: {', '.join(missing[:5])})")
        if extra:
            blockers.append(f"{len(extra)} verification rows are not part of the sample (examples: {', '.join(extra[:5])})")

    incomplete = [
        candidate_id
        for candidate_id, row in sorted(verified.items())
        if row["human_label"] not in LABELS or row["review_status"] not in REVIEW_STATUSES
    ]
    if incomplete:
        blockers.append(
            f"{len(incomplete)} verification rows lack a valid label and review status "
            f"(examples: {', '.join(incomplete[:5])})"
        )
    needs_discussion = sorted(
        candidate_id
        for candidate_id, row in verified.items()
        if row["review_status"] == "NEEDS_DISCUSSION" and candidate_id not in set(incomplete)
    )
    unexplained = [
        candidate_id for candidate_id in needs_discussion if not verified[candidate_id]["review_notes"]
    ]
    if unexplained:
        blockers.append(
            f"{len(unexplained)} rows flagged NEEDS_DISCUSSION give no reason in review_notes "
            f"(examples: {', '.join(unexplained[:5])})"
        )

    prelabel_by_id = {record["candidate_id"]: record for record in records}
    sample_ids = [
        candidate_id for candidate_id in expected["agreement_sample_ids"] if candidate_id in verified and candidate_id not in set(incomplete)
    ]
    # §4.1.1-4.1.3 settle a boundary whenever either side carries no retrievable
    # evidence. Those rows measure nothing about human-model agreement, so they are
    # reported separately instead of counted as agreement in either direction.
    rule_determined = {
        candidate_id: classify_noise(
            prelabel_by_id[candidate_id].get("left_evidence", ""),
            prelabel_by_id[candidate_id].get("right_evidence", ""),
        )[1]
        for candidate_id in sample_ids
        if classify_noise(
            prelabel_by_id[candidate_id].get("left_evidence", ""),
            prelabel_by_id[candidate_id].get("right_evidence", ""),
        )[0]
    }
    judgment_ids = [candidate_id for candidate_id in sample_ids if candidate_id not in rule_determined]
    human_labels = [verified[candidate_id]["human_label"] for candidate_id in judgment_ids]
    model_labels = [prelabel_by_id[candidate_id]["prelabel"] for candidate_id in judgment_ids]
    agreements = sum(left == right for left, right in zip(human_labels, model_labels))
    confusion: dict[str, Counter[str]] = defaultdict(Counter)
    for human, model in zip(human_labels, model_labels):
        confusion[model][human] += 1

    kappa = cohen_kappa(human_labels, model_labels) if human_labels else 0.0
    # The rule families were specified after inspecting round-one disagreements, so
    # the pre-amendment figure is kept and reported alongside: a reader must be able
    # to see how much of the agreement is owed to the amendment.
    all_human = [verified[candidate_id]["human_label"] for candidate_id in sample_ids]
    all_model = [prelabel_by_id[candidate_id]["prelabel"] for candidate_id in sample_ids]
    kappa_before = cohen_kappa(all_human, all_model) if all_human else 0.0
    corrected_flagged = sum(
        verified[candidate_id]["human_label"] != prelabel_by_id[candidate_id]["prelabel"]
        for candidate_id in expected["flagged_review_ids"]
        if candidate_id in verified and verified[candidate_id]["human_label"] in LABELS
    )
    return {
        "project_id": project,
        "candidate_count": len(records),
        "verification_sample_size": len(sample_ids),
        "rule_determined_count": len(rule_determined),
        "rule_determined_reasons": dict(sorted(Counter(rule_determined.values()).items())),
        "agreement_sample_size": len(judgment_ids),
        "agreement_count": agreements,
        "disagreement_count": len(judgment_ids) - agreements,
        "raw_agreement": agreements / len(judgment_ids) if judgment_ids else 0.0,
        "cohen_kappa": kappa,
        "cohen_kappa_before_rule_amendment": kappa_before,
        "flagged_review_size": len(expected["flagged_review_ids"]),
        "flagged_corrected_count": corrected_flagged,
        "needs_discussion_count": len(needs_discussion),
        "needs_discussion_ids": needs_discussion,
        "human_label_distribution": dict(sorted(Counter(human_labels).items())),
        "prelabel_distribution_on_sample": dict(sorted(Counter(model_labels).items())),
        "confusion_prelabel_to_human": {
            model: dict(sorted(counts.items())) for model, counts in sorted(confusion.items())
        },
        "blockers": blockers,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--prelabel-root", default="outputs/llm_prelabel_v1")
    parser.add_argument("--verification-root", default="outputs/prelabel_verification_v1")
    parser.add_argument("--workbench-root", default="workbench")
    parser.add_argument("--kappa-threshold", type=float, default=0.70)
    parser.add_argument("--summary-output", default="outputs/prelabel_verification_v1/verification_summary.json")
    parser.add_argument("--write", action="store_true", help="write the promoted gold standard")
    parser.add_argument("--force", action="store_true", help="overwrite an existing gold standard")
    parser.add_argument(
        "--withhold",
        nargs="*",
        default=[],
        metavar="PROJECT",
        help=(
            "projects to leave unpromoted because verification did not pass. Naming a "
            "project here withholds its boundary gold standard; it does not relax any "
            "threshold, and downstream boundary metrics must omit the project."
        ),
    )
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    prelabel_root = root / args.prelabel_root
    verification_root = root / args.verification_root
    verification_manifest = json.loads((verification_root / "verification_manifest.json").read_text(encoding="utf-8"))
    workbook_path = verification_root / "SEAC_boundary_prelabel_verification.xlsx"
    if not workbook_path.is_file():
        raise SystemExit(f"verification workbook not found: {workbook_path}")
    if sha256_file(workbook_path) == verification_manifest["workbook_sha256"]:
        raise SystemExit(
            "verification workbook is unchanged from the generated template; human verification has not started"
        )

    projects = list(verification_manifest["projects"])
    verified_by_project = read_verification(workbook_path, projects)

    report = {
        "protocol_version": PROTOCOL_VERSION,
        "kappa_threshold": args.kappa_threshold,
        "verification_workbook_sha256": sha256_file(workbook_path),
        "projects": {},
    }
    audits = {}
    for project in projects:
        records = json.loads((prelabel_root / f"boundary_llm_prelabel_{project}.json").read_text(encoding="utf-8"))
        audit = audit_project(project, records, verified_by_project[project], verification_manifest["projects"][project])
        if audit["agreement_sample_size"] and audit["cohen_kappa"] < args.kappa_threshold:
            audit["blockers"].append(
                f'human-LLM kappa {audit["cohen_kappa"]:.3f} is below the {args.kappa_threshold:.2f} threshold'
            )
        audits[project] = (records, audit)
        report["projects"][project] = {key: value for key, value in audit.items() if key != "blockers"}
        report["projects"][project]["blockers"] = audit["blockers"]

    withheld = sorted(set(args.withhold))
    unknown = [project for project in withheld if project not in audits]
    if unknown:
        raise SystemExit(f"--withhold names unknown project(s): {', '.join(unknown)}")
    # Withholding is only honest when the project actually failed: it must never be a
    # way to drop a project whose numbers are inconvenient for some other reason.
    passing_withheld = [project for project in withheld if not audits[project][1]["blockers"]]
    if passing_withheld:
        raise SystemExit(
            f"--withhold names project(s) that passed verification: {', '.join(passing_withheld)}; "
            "withholding is reserved for projects that failed"
        )
    blocked = sorted(project for project, (_, audit) in audits.items() if audit["blockers"])
    report["withheld_projects"] = withheld
    report["blocked_projects"] = blocked
    report["promotable"] = not [project for project in blocked if project not in withheld]
    summary_path = root / args.summary_output
    summary_path.parent.mkdir(parents=True, exist_ok=True)
    summary_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    if args.write:
        if not report["promotable"]:
            raise SystemExit(
                "verification gates are not satisfied for "
                f"{', '.join(project for project in blocked if project not in withheld)}; "
                "inspect the verification summary"
            )
        written = []
        for project, (records, _) in audits.items():
            if project in withheld:
                continue
            verified = verified_by_project[project]
            target = root / args.workbench_root / project / "candidate_breakpoints_adjudicated.json"
            if target.exists() and not args.force:
                raise SystemExit(f"{target} already exists; archive it first or pass --force")
            promoted = []
            for record in records:
                candidate_id = record["candidate_id"]
                row = verified.get(candidate_id)
                determined, reason = classify_noise(
                    record.get("left_evidence", ""), record.get("right_evidence", "")
                )
                if determined:
                    # §4.1 decides these outright, so neither the annotator's nor the
                    # model's label is used. The workbook keeps whatever the annotator
                    # wrote; only the promoted record is normalised, and it says so.
                    label, source, status = "NOISE", "RULE_4_1", "RULE_DETERMINED"
                    notes = f"§4.1 判定：{reason}"
                    ambiguous = False
                elif row and row["human_label"] in LABELS:
                    label, source, status = row["human_label"], "HUMAN_VERIFIED", "HUMAN_VERIFIED"
                    notes = row["review_notes"]
                    ambiguous = row["review_status"] == "NEEDS_DISCUSSION"
                else:
                    label, source, status = record["prelabel"], "LLM_PRELABEL", "LLM_PRELABEL_ACCEPTED"
                    notes = record["prelabel_notes"]
                    ambiguous = False
                promoted.append(
                    {
                        **{
                            key: value
                            for key, value in record.items()
                            if key not in {"human_label", "review_status", "review_notes"}
                        },
                        "human_label": label,
                        "label_source": source,
                        "human_verified": source == "HUMAN_VERIFIED",
                        "rule_determined": determined,
                        "rule_reason": reason,
                        "annotator_flagged_ambiguous": ambiguous,
                        "review_status": status,
                        "review_notes": notes,
                    }
                )
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(json.dumps(promoted, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
            written.append(str(target.relative_to(root)))
        print(json.dumps({"promoted": written}, ensure_ascii=False, indent=2))
        return

    print(
        json.dumps(
            {
                "promotable": report["promotable"],
                "projects": {
                    project: {
                        "kappa": round(info["cohen_kappa"], 3),
                        "raw_agreement": round(info["raw_agreement"], 3),
                        "blockers": info["blockers"],
                    }
                    for project, info in report["projects"].items()
                },
            },
            ensure_ascii=False,
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
