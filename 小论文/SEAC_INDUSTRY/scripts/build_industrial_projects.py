from __future__ import annotations

import argparse
import hashlib
import json
import re
from pathlib import Path

import extract_project_corpus as extractor


SUPPORTED_SUFFIXES = {".docx", ".xlsx"}


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def safe_preview_name(relative_path: Path) -> str:
    stem = "__".join(relative_path.with_suffix("").parts)
    return re.sub(r"[^\w.-]+", "_", stem) + ".txt"


def load_converted_sources(project_id: str, conversion_root: Path) -> dict[str, dict]:
    manifest_path = conversion_root / project_id / "conversion_manifest.json"
    if not manifest_path.is_file():
        return {}
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    converted = {}
    for record in manifest.get("records", []):
        if record.get("status") != "converted":
            continue
        output_path = conversion_root / project_id / record["output_relative"]
        if output_path.is_file():
            converted[record["source_relative"]] = {**record, "path": output_path}
    return converted


def extract_project(project, source_root: Path, output_root: Path, conversion_root: Path):
    project_id = project["project_id"]
    source_dir = source_root / project["source_dir"]
    if not source_dir.is_dir():
        raise FileNotFoundError(f"Missing project source directory: {source_dir}")

    output_dir = output_root / project_id
    output_dir.mkdir(parents=True, exist_ok=True)
    extractor.PROJECT_ID = project_id
    documents = []
    processed = []
    skipped = []
    converted_sources = load_converted_sources(project_id, conversion_root)

    for path in sorted(item for item in source_dir.rglob("*") if item.is_file()):
        relative = path.relative_to(source_dir)
        suffix = path.suffix.lower()
        if path.name.startswith(".~") or path.name == ".DS_Store":
            skipped.append({"file": str(relative), "reason": "temporary_or_system_file"})
            continue
        converted = converted_sources.get(str(relative))
        extraction_path = converted["path"] if converted else path
        extraction_suffix = extraction_path.suffix.lower()
        if suffix not in SUPPORTED_SUFFIXES and not converted:
            skipped.append({"file": str(relative), "reason": f"unsupported_{suffix.lstrip('.') or 'no_suffix'}"})
            continue

        if converted and converted["source_sha256"] != sha256(path):
            skipped.append({"file": str(relative), "reason": "stale_converted_source"})
            continue

        try:
            document = (
                extractor.extract_docx(extraction_path)
                if extraction_suffix == ".docx"
                else extractor.extract_xlsx(extraction_path)
            )
        except Exception as exc:
            skipped.append(
                {
                    "file": str(relative),
                    "reason": "parse_error",
                    "error_type": type(exc).__name__,
                    "error": str(exc),
                    "sha256": sha256(path),
                    "bytes": path.stat().st_size,
                }
            )
            continue
        document["file_name"] = relative.name
        document["document_type"] = extractor.classify_document(relative.name)
        document["file_path"] = str(Path(project["source_dir"]) / relative)
        documents.append(document)
        processed_record = {
            "file": str(relative),
            "suffix": suffix,
            "sha256": sha256(path),
            "bytes": path.stat().st_size,
            "document_type": document["document_type"],
            "source_mode": "converted_copy" if converted else "original",
        }
        if converted:
            processed_record.update(
                {
                    "converted_file": str(converted["path"].relative_to(conversion_root)),
                    "converted_sha256": converted["output_sha256"],
                    "conversion_reason": converted["conversion_reason"],
                }
            )
        processed.append(processed_record)

        preview_path = output_dir / safe_preview_name(relative)
        with preview_path.open("w", encoding="utf-8") as handle:
            for block in document["blocks"]:
                handle.write(
                    f'[{block["block_id"]}] [{block["block_type"]}] '
                    f'[{block.get("heading_path", "")}]\n{block["text"]}\n\n'
                )

    if not documents:
        raise ValueError(f"No supported documents found for {project_id}: {source_dir}")

    stats = [{key: value for key, value in document.items() if key != "blocks"} for document in documents]
    (output_dir / "corpus.json").write_text(
        json.dumps(documents, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    (output_dir / "document_statistics.json").write_text(
        json.dumps(stats, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    extraction_manifest = {
        "protocol_version": "industrial-seac-v1.0",
        "project_id": project_id,
        "split": project["split"],
        "source_dir": project["source_dir"],
        "processed": processed,
        "skipped": skipped,
        "document_count": len(documents),
        "block_count": sum(len(document["blocks"]) for document in documents),
        "approx_token_count": sum(document["tokens_approx"] for document in documents),
    }
    (output_dir / "extraction_manifest.json").write_text(
        json.dumps(extraction_manifest, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    return extraction_manifest


def main():
    parser = argparse.ArgumentParser(description="Build reproducible P01-P10 industrial SEAC corpora.")
    parser.add_argument("--config", default="configs/industrial_projects.json")
    parser.add_argument("--source-root")
    parser.add_argument("--output-root", default="workbench")
    parser.add_argument("--conversion-root", default="workbench/converted_sources")
    parser.add_argument("--projects", nargs="*")
    args = parser.parse_args()

    config_path = Path(args.config)
    config = json.loads(config_path.read_text(encoding="utf-8"))
    source_root = Path(args.source_root or config["source_root"])
    if not source_root.is_absolute():
        source_root = (config_path.parent / source_root).resolve()
    output_root = Path(args.output_root)
    conversion_root = Path(args.conversion_root)
    requested = set(args.projects or [])
    projects = [
        project for project in config["projects"]
        if not requested or project["project_id"] in requested
    ]
    if requested - {project["project_id"] for project in projects}:
        unknown = ", ".join(sorted(requested - {project["project_id"] for project in projects}))
        raise ValueError(f"Unknown project ids: {unknown}")

    summaries = []
    for project in projects:
        summary = extract_project(project, source_root, output_root, conversion_root)
        summaries.append(summary)
        print(
            f'{summary["project_id"]}: documents={summary["document_count"]} '
            f'blocks={summary["block_count"]} tokens~={summary["approx_token_count"]} '
            f'skipped={len(summary["skipped"])}',
            flush=True,
        )
    print(json.dumps(summaries, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
