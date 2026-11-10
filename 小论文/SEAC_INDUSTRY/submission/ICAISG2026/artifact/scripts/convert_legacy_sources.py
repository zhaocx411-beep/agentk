from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import tempfile
import zipfile
from pathlib import Path

from docx import Document


OLE_MAGIC = bytes.fromhex("D0CF11E0A1B11AE1")


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def container_kind(path: Path) -> str:
    with path.open("rb") as handle:
        signature = handle.read(8)
    if signature == OLE_MAGIC:
        return "ole"
    if signature.startswith(b"PK"):
        return "zip"
    return "unknown"


def docx_content_stats(path: Path) -> dict[str, int]:
    document = Document(path)
    paragraph_characters = sum(len(paragraph.text.strip()) for paragraph in document.paragraphs)
    table_characters = sum(
        len(cell.text.strip())
        for table in document.tables
        for row in table.rows
        for cell in row.cells
    )
    return {
        "paragraphs": sum(bool(paragraph.text.strip()) for paragraph in document.paragraphs),
        "tables": len(document.tables),
        "characters": paragraph_characters + table_characters,
    }


def needs_conversion(path: Path) -> tuple[bool, str]:
    suffix = path.suffix.lower()
    if suffix == ".doc":
        return True, "legacy_doc"
    if suffix != ".docx":
        return False, "unsupported"
    kind = container_kind(path)
    if kind == "ole":
        return True, "legacy_doc_mislabeled_docx"
    if kind != "zip":
        return True, f"invalid_docx_container_{kind}"
    try:
        docx_content_stats(path)
    except Exception as exc:
        return True, f"docx_parse_error:{type(exc).__name__}"
    return False, "valid_docx"


def output_relative_path(source_relative: Path) -> Path:
    return source_relative.with_suffix(".docx")


def find_soffice(explicit: str | None) -> str:
    candidate = explicit or shutil.which("soffice") or shutil.which("libreoffice")
    if not candidate:
        raise FileNotFoundError("LibreOffice soffice executable was not found")
    return candidate


def convert_one(source: Path, destination: Path, soffice: str) -> dict:
    source_kind = container_kind(source)
    input_suffix = ".doc" if source_kind == "ole" else ".docx"
    with tempfile.TemporaryDirectory(prefix="seac_doc_convert_", dir="/private/tmp") as temp_name:
        temp_root = Path(temp_name)
        input_dir = temp_root / "input"
        output_dir = temp_root / "output"
        profile_dir = temp_root / "profile"
        input_dir.mkdir()
        output_dir.mkdir()
        profile_dir.mkdir()
        temp_input = input_dir / f"source{input_suffix}"
        shutil.copy2(source, temp_input)
        environment = os.environ.copy()
        environment.update(
            {
                "HOME": str(profile_dir),
                "XDG_CONFIG_HOME": str(profile_dir / "config"),
                "XDG_CACHE_HOME": str(profile_dir / "cache"),
                "TMPDIR": "/private/tmp",
            }
        )
        command = [
            soffice,
            "--headless",
            f"-env:UserInstallation=file://{profile_dir}",
            "--convert-to",
            "docx:Office Open XML Text",
            "--outdir",
            str(output_dir),
            str(temp_input),
        ]
        completed = subprocess.run(
            command,
            check=False,
            capture_output=True,
            text=True,
            timeout=180,
            env=environment,
        )
        converted = output_dir / "source.docx"
        if completed.returncode != 0 or not converted.is_file():
            raise RuntimeError(
                "LibreOffice conversion failed "
                f"(exit={completed.returncode}): {completed.stdout.strip()} {completed.stderr.strip()}"
            )
        if not zipfile.is_zipfile(converted):
            raise ValueError("LibreOffice output is not a valid OOXML ZIP container")
        stats = docx_content_stats(converted)
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(converted, destination)
    return stats


def convert_project(project: dict, source_root: Path, output_root: Path, soffice: str) -> dict:
    project_id = project["project_id"]
    source_dir = source_root / project["source_dir"]
    project_output = output_root / project_id
    records = []
    for source in sorted(item for item in source_dir.rglob("*") if item.is_file()):
        if source.name.startswith(".~") or source.name == ".DS_Store":
            continue
        required, reason = needs_conversion(source)
        if not required:
            continue
        if source.suffix.lower() not in {".doc", ".docx"}:
            continue
        relative = source.relative_to(source_dir)
        destination_relative = output_relative_path(relative)
        destination = project_output / destination_relative
        record = {
            "source_relative": str(relative),
            "source_sha256": sha256(source),
            "source_bytes": source.stat().st_size,
            "source_container": container_kind(source),
            "conversion_reason": reason,
            "output_relative": str(destination_relative),
        }
        try:
            stats = convert_one(source, destination, soffice)
            record.update(
                {
                    "status": "converted",
                    "output_sha256": sha256(destination),
                    "output_bytes": destination.stat().st_size,
                    "content_stats": stats,
                }
            )
        except Exception as exc:
            record.update(
                {
                    "status": "failed",
                    "error_type": type(exc).__name__,
                    "error": str(exc),
                }
            )
        records.append(record)
        print(f'{project_id}: {relative} -> {record["status"]}', flush=True)

    manifest = {
        "protocol_version": "industrial-seac-source-conversion-v1.0",
        "project_id": project_id,
        "source_dir": project["source_dir"],
        "soffice": soffice,
        "converted_count": sum(record["status"] == "converted" for record in records),
        "failed_count": sum(record["status"] == "failed" for record in records),
        "records": records,
    }
    project_output.mkdir(parents=True, exist_ok=True)
    (project_output / "conversion_manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    return manifest


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Convert legacy or malformed industrial Word sources into traceable DOCX copies."
    )
    parser.add_argument("--config", default="configs/industrial_projects.json")
    parser.add_argument("--source-root")
    parser.add_argument("--output-root", default="workbench/converted_sources")
    parser.add_argument("--projects", nargs="*")
    parser.add_argument("--soffice")
    args = parser.parse_args()

    config_path = Path(args.config)
    config = json.loads(config_path.read_text(encoding="utf-8"))
    source_root = Path(args.source_root or config["source_root"])
    if not source_root.is_absolute():
        source_root = (config_path.parent / source_root).resolve()
    output_root = Path(args.output_root)
    requested = set(args.projects or [])
    projects = [
        project for project in config["projects"]
        if not requested or project["project_id"] in requested
    ]
    unknown = requested - {project["project_id"] for project in projects}
    if unknown:
        raise ValueError(f'Unknown project ids: {", ".join(sorted(unknown))}')

    soffice = find_soffice(args.soffice)
    manifests = [convert_project(project, source_root, output_root, soffice) for project in projects]
    print(
        json.dumps(
            [
                {
                    "project_id": manifest["project_id"],
                    "converted_count": manifest["converted_count"],
                    "failed_count": manifest["failed_count"],
                }
                for manifest in manifests
            ],
            ensure_ascii=False,
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
