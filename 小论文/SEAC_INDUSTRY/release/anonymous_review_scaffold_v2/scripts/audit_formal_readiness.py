from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path


LABELS = {"BREAK", "NO_BREAK", "NOISE"}
APPROVED = {"KEEP", "APPROVED", "VERIFIED", "已复核", "已确认", "已仲裁"}
PRIVACY_CLEARED = {"CLEARED", "NO_RISK", "ANONYMIZED"}


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def read_json(path: Path):
    return json.loads(path.read_text(encoding="utf-8"))


def resolve(root: Path, value: str) -> Path:
    path = Path(value)
    return path if path.is_absolute() else root / path


def audit_project(project: dict, root: Path) -> dict:
    blockers = []
    # A project whose boundary verification failed carries no boundary gold standard.
    # The manifest has to declare that explicitly, so a genuinely missing file is
    # still a blocker and the omission has to be a recorded decision with a reason.
    boundary_withheld = bool(project.get("boundary_withheld"))
    if boundary_withheld and not str(project.get("boundary_withheld_reason", "")).strip():
        blockers.append("boundary_withheld is set without boundary_withheld_reason")
    # A project whose approved question set stayed below the per-project minimum is
    # withheld from retrieval reporting. Its questions are individually valid, so the
    # file is kept; only the minimum-count blocker is waived, and 4.1bis's rule
    # applies here too: the flag may not be used on a project that actually passes.
    qa_withheld = bool(project.get("qa_withheld"))
    if qa_withheld and not str(project.get("qa_withheld_reason", "")).strip():
        blockers.append("qa_withheld is set without qa_withheld_reason")
    required = ("corpus", "qa") if boundary_withheld else ("corpus", "breakpoints", "qa")
    paths = {key: resolve(root, project[key]) for key in ("corpus", "breakpoints", "qa")}
    for key in required:
        if not paths[key].is_file():
            blockers.append(f"missing {key}: {paths[key]}")
    if boundary_withheld and paths["breakpoints"].is_file():
        blockers.append(
            f'boundary_withheld is set but {paths["breakpoints"]} exists; '
            "archive the withheld gold standard or clear the flag"
        )
    counts = {"documents": 0, "boundaries": 0, "qa": 0}
    hashes = {}
    if paths["corpus"].is_file():
        corpus = read_json(paths["corpus"])
        counts["documents"] = len(corpus)
        hashes["corpus_sha256"] = sha256(paths["corpus"])
        if not corpus:
            blockers.append("corpus is empty")
    if not boundary_withheld and paths["breakpoints"].is_file():
        boundaries = read_json(paths["breakpoints"])
        counts["boundaries"] = len(boundaries)
        hashes["breakpoints_sha256"] = sha256(paths["breakpoints"])
        invalid = sum(str(item.get("human_label", "")).strip().upper() not in LABELS for item in boundaries)
        expected = project["expected_boundary_count"]
        if len(boundaries) != expected:
            blockers.append(f"boundary count is {len(boundaries)}, expected {expected}")
        if invalid:
            blockers.append(f"{invalid} boundaries lack an adjudicated human label")
    if paths["qa"].is_file():
        questions = read_json(paths["qa"])
        counts["qa"] = len(questions)
        hashes["qa_sha256"] = sha256(paths["qa"])
        approval_pending = 0
        privacy_pending = 0
        exact_evidence_missing = 0
        wrong_project = 0
        for item in questions:
            statuses = {
                str(item.get("audit_status", "")).strip().upper(),
                str(item.get("review_status", "")).strip().upper(),
            }
            approval_pending += not bool(statuses.intersection(APPROVED))
            privacy_pending += str(item.get("privacy_review_status", "")).strip().upper() not in PRIVACY_CLEARED
            exact_evidence_missing += not bool(
                (item.get("gold_evidence_unit_id") or item.get("gold_evidence_unit_ids"))
                and (item.get("gold_evidence_span") or item.get("gold_evidence_spans"))
            )
            wrong_project += item.get("project_id") != project["project_id"]
        below_minimum = len(questions) < project["minimum_qa_count"]
        if below_minimum and not qa_withheld:
            blockers.append(f'QA count is {len(questions)}, below minimum {project["minimum_qa_count"]}')
        if qa_withheld and not below_minimum:
            blockers.append(
                f'qa_withheld is set but the QA count {len(questions)} meets the minimum '
                f'{project["minimum_qa_count"]}; clear the flag'
            )
        if approval_pending:
            blockers.append(f"{approval_pending} QA records are not approved")
        if privacy_pending:
            blockers.append(f"{privacy_pending} QA records lack privacy clearance")
        if exact_evidence_missing:
            blockers.append(f"{exact_evidence_missing} QA records lack exact unit/span evidence")
        if wrong_project:
            blockers.append(f"{wrong_project} QA records have the wrong project_id")
    return {
        "project_id": project["project_id"],
        "split": project["split"],
        "formal_ready": not blockers,
        "boundary_withheld": boundary_withheld,
        "boundary_withheld_reason": project.get("boundary_withheld_reason", ""),
        "qa_withheld": qa_withheld,
        "qa_withheld_reason": project.get("qa_withheld_reason", ""),
        "counts": counts,
        "hashes": hashes,
        "blockers": blockers,
        "paths": {key: str(path) for key, path in paths.items()},
    }


def audit_manifest(manifest: dict, root: Path) -> dict:
    projects = [audit_project(project, root) for project in manifest["projects"]]
    locked = [item for item in projects if item["split"] == "locked_test"]
    return {
        "protocol_version": manifest["protocol_version"],
        "formal_ready": all(item["formal_ready"] for item in projects),
        "locked_test_release_ready": bool(locked) and all(item["formal_ready"] for item in locked),
        "ready_project_count": sum(item["formal_ready"] for item in projects),
        "project_count": len(projects),
        "projects": projects,
    }


def write_markdown(report: dict, path: Path) -> None:
    lines = [
        "# SEAC formal experiment readiness",
        "",
        f'- Ready projects: {report["ready_project_count"]}/{report["project_count"]}',
        f'- Locked P04–P10 release: {"READY" if report["locked_test_release_ready"] else "BLOCKED"}',
        "",
        "| Project | Split | Documents | Boundaries | QA | Status | Blockers |",
        "|---|---|---:|---:|---:|---|---|",
    ]
    for item in report["projects"]:
        blockers = "; ".join(item["blockers"]) or "—"
        lines.append(
            f'| {item["project_id"]} | {item["split"]} | {item["counts"]["documents"]} | '
            f'{item["counts"]["boundaries"]} | {item["counts"]["qa"]} | '
            f'{"READY" if item["formal_ready"] else "BLOCKED"} | {blockers} |'
        )
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser(description="Audit all gates before formal SEAC experiments.")
    parser.add_argument("--manifest", default="configs/formal_run_manifest.json")
    parser.add_argument("--json-output", default="experiments/formal_readiness.json")
    parser.add_argument("--markdown-output", default="experiments/formal_readiness.md")
    parser.add_argument("--require-ready", action="store_true")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    manifest = read_json(resolve(root, args.manifest))
    report = audit_manifest(manifest, root)
    json_output = resolve(root, args.json_output)
    markdown_output = resolve(root, args.markdown_output)
    json_output.parent.mkdir(parents=True, exist_ok=True)
    markdown_output.parent.mkdir(parents=True, exist_ok=True)
    json_output.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    write_markdown(report, markdown_output)
    print(json.dumps({key: report[key] for key in ("formal_ready", "locked_test_release_ready", "ready_project_count", "project_count")}, ensure_ascii=False, indent=2))
    if args.require_ready and not report["formal_ready"]:
        raise ValueError("Formal experiment gates are not complete; inspect the readiness report")


if __name__ == "__main__":
    main()
