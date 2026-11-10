from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

from audit_formal_readiness import audit_manifest, read_json, resolve


def command_for(
    project: dict,
    manifest: dict,
    frozen: dict,
    frozen_path: Path,
    root: Path,
    output_root: Path,
    device: str,
    batch_size: int,
) -> list[str]:
    # A withheld axis is passed as "none" rather than as a path that does not
    # exist, so the runner reports the axis as absent instead of crashing.
    qa = "none" if project.get("qa_withheld") else str(resolve(root, project["qa"]))
    breakpoints = (
        "none" if project.get("boundary_withheld") else str(resolve(root, project["breakpoints"]))
    )
    command = [
        sys.executable,
        str(root / "scripts/run_chunking_experiment.py"),
        "--project-id",
        project["project_id"],
        "--corpus",
        str(resolve(root, project["corpus"])),
        "--qa",
        qa,
        "--breakpoints",
        breakpoints,
        "--methods",
        *manifest["methods"],
        "--encoder",
        manifest["encoder"],
        "--evidence-mode",
        manifest["evidence_mode"],
        "--model-batch-size",
        str(batch_size),
        "--device",
        device,
        "--protocol-config",
        str(frozen_path),
        "--max-tokens",
        str(frozen["max_tokens"]),
        "--overlap",
        str(frozen["overlap"]),
        "--semantic-percentile",
        str(frozen["semantic_percentile"]),
        "--semantic-weight",
        str(frozen["semantic_weight"]),
        "--structure-weight",
        str(frozen["structure_weight"]),
        "--density-weight",
        str(frozen["key_density_change_weight"]),
        "--continuity-penalty",
        str(frozen["same_heading_continuity_penalty"]),
        "--protected-group-penalty",
        str(frozen["same_protected_group_penalty"]),
        "--structure-heading-signal",
        str(frozen["structure_heading_signal"]),
        "--structure-path-change-signal",
        str(frozen["structure_path_change_signal"]),
        "--structure-group-change-signal",
        str(frozen["structure_group_change_signal"]),
        "--density-change-scale",
        str(frozen["density_change_scale"]),
        "--table-ratio-threshold-adjustment",
        str(frozen["table_ratio_threshold_adjustment"]),
        "--heading-ratio-threshold-adjustment",
        str(frozen["heading_ratio_threshold_adjustment"]),
        "--key-density-cutoff",
        str(frozen["key_density_cutoff"]),
        "--proposed-soft-target",
        str(frozen["parent_soft_target_tokens"]),
        "--proposed-dense-target",
        str(frozen["parent_dense_target_tokens"]),
        "--proposed-min-chunk",
        str(frozen["parent_min_tokens"]),
        "--v6-child-target",
        str(frozen["child_target_tokens"]),
        "--v6-child-max",
        str(frozen["child_max_tokens"]),
        "--hierarchy-parent-weight",
        str(frozen["hierarchy_parent_weight"]),
        "--output-dir",
        str(output_root / project["project_id"]),
    ]
    command.append("--fair-context" if frozen["fair_context"] else "--no-fair-context")
    return command


def main() -> None:
    parser = argparse.ArgumentParser(description="Run the frozen SEAC formal matrix after all annotation gates pass.")
    parser.add_argument("--manifest", default="configs/formal_run_manifest.json")
    parser.add_argument("--frozen-config", default="configs/industrial_seac_frozen.json")
    parser.add_argument("--projects", nargs="*")
    parser.add_argument("--device", default="cuda")
    parser.add_argument("--model-batch-size", type=int, default=16)
    parser.add_argument("--output-root", default="experiments/formal_v1")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    manifest = read_json(resolve(root, args.manifest))
    frozen_path = resolve(root, args.frozen_config)
    frozen = read_json(frozen_path)
    audit = audit_manifest(manifest, root)
    requested = set(args.projects or [])
    selected = [project for project in manifest["projects"] if not requested or project["project_id"] in requested]
    unknown = requested - {project["project_id"] for project in selected}
    if unknown:
        raise ValueError(f"Unknown project IDs: {sorted(unknown)}")
    readiness = {item["project_id"]: item for item in audit["projects"]}
    blocked = [project["project_id"] for project in selected if not readiness[project["project_id"]]["formal_ready"]]
    if any(project["split"] == "locked_test" for project in selected) and not audit["locked_test_release_ready"]:
        blocked = sorted(set(blocked + ["P04-P10 collective locked-test release gate"]))
    output_root = resolve(root, args.output_root)
    commands = [
        command_for(
            project,
            manifest,
            frozen,
            frozen_path,
            root,
            output_root,
            args.device,
            args.model_batch_size,
        )
        for project in selected
    ]
    if args.dry_run:
        print(json.dumps({"blocked": blocked, "commands": commands}, ensure_ascii=False, indent=2))
        return
    if blocked:
        raise ValueError(f"Formal suite is blocked: {blocked}")
    for command in commands:
        subprocess.run(command, cwd=root, check=True)


if __name__ == "__main__":
    main()
