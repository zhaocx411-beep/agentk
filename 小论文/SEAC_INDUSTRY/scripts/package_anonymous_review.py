from __future__ import annotations

import argparse
import hashlib
import json
import re
import shutil
import zipfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]

SCRIPT_FILES = (
    "adjudicate_boundary_annotations.py",
    "align_qa_evidence_units.py",
    "analyze_formal_results.py",
    "audit_formal_readiness.py",
    "audit_review_progress.py",
    "analyze_formal_results.py",
    "audit_formal_readiness.py",
    "audit_prelabel_consistency.py",
    "boundary_candidate_utils.py",
    "boundary_noise_rules.py",
    "boundary_verification_store.py",
    "build_generic_review_dataset.py",
    "build_prelabel_verification_sample.py",
    "build_qa_approval_workbook.py",
    "build_submission_docx.py",
    "convert_legacy_sources.py",
    "fill_manuscript_results.py",
    "finalize_qa_review.py",
    "import_qa_approval_workbook.py",
    "import_review_workbooks.py",
    "lint_manuscript.py",
    "merge_llm_prelabels.py",
    "merge_qa_prereview.py",
    "merge_qa_readjudication.py",
    "prepare_annotation_pack.py",
    "prepare_calibration_sample.py",
    "prepare_jss_manuscript.py",
    "prepare_llm_prelabel_batches.py",
    "prepare_qa_privacy_review.py",
    "privacy_review_store.py",
    "promote_llm_prelabels.py",
    "qa_approval_store.py",
    "qa_question_rules.py",
    "resolve_qa_evidence_equivalents.py",
    "run_chunking_experiment.py",
    "run_formal_suite.py",
    "serve_boundary_verification.py",
    "serve_privacy_review.py",
    "serve_qa_approval.py",
    "verify_supplementary_qa_drafts.py",
)

# The three review views are served as static assets by the scripts above. Their
# tests start a real server and assert on the rendered page, so omitting the
# assets would ship a package whose own test suite cannot run.
UI_DIRECTORIES = (
    "boundary_verification_ui",
    "privacy_review_ui",
    "qa_approval_ui",
)

TOP_LEVEL_FILES = ("requirements-paper.txt",)

CONFIG_FILES = (
    "formal_run_manifest.json",
    "industrial_seac_frozen.json",
    "synthetic_run_manifest.json",
)

DOC_FILES = (
    "annotation_and_privacy_protocol.md",
    "industrial_seac_protocol.md",
    "submission_strategy.md",
)

MANUSCRIPT_FILES = (
    "README.md",
    "references.bib",
    "seac_industry_manuscript.md",
)

ALLOWED_SUFFIXES = {".bib", ".css", ".html", ".js", ".json", ".md", ".py", ".txt"}
BANNED_SCOPE_TERMS = ("docfinqa", "tat-qa", "financebench", "autoseac-fin", "personal-finkb")
BANNED_ENTERPRISE_LITERALS = (
    "镔鑫",
    # An affiliate named inside P01 body text rather than in a file name; found
    # during the P01 privacy round, where no derived identifier covered it.
    "华乐合金",
    "晋钢",
    "天津钢管",
    "津西",
    "烟台华新",
    "巨能特钢",
)
USER_PATH = re.compile(r"/Users/[^/\s]+/")
PRIVATE_IPV4 = re.compile(
    r"\b(?:10(?:\.\d{1,3}){3}|192\.168(?:\.\d{1,3}){2}|172\.(?:1[6-9]|2\d|3[01])(?:\.\d{1,3}){2})\b"
)
CREDENTIAL_VALUE = re.compile(
    r"(?i)(?:password|passwd|api[_-]?key|access[_-]?token|secret)"
    r"\s*[\"']?\s*[:=]\s*[\"']([A-Za-z0-9+/=_-]{16,})"
)


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def source_files() -> list[Path]:
    files = [ROOT / name for name in TOP_LEVEL_FILES]
    files.extend(ROOT / "configs" / name for name in CONFIG_FILES)
    files.extend(ROOT / "docs" / name for name in DOC_FILES)
    files.extend(ROOT / "manuscript" / name for name in MANUSCRIPT_FILES)
    files.extend(ROOT / "scripts" / name for name in SCRIPT_FILES)
    for directory, pattern in (
        (ROOT / "seac_chunking", "*.py"),
        (ROOT / "tests", "test_*.py"),
        (ROOT / "schemas", "*"),
        (ROOT / "examples/synthetic", "*"),
        *((ROOT / "scripts" / name, "*") for name in UI_DIRECTORIES),
    ):
        files.extend(path for path in directory.glob(pattern) if path.is_file())
    unique = sorted(set(files), key=lambda path: str(path.relative_to(ROOT)))
    missing = [path for path in unique if not path.is_file()]
    if missing:
        raise FileNotFoundError(f"Release whitelist contains missing files: {missing}")
    return unique


def write_text(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def write_json(path: Path, value: object) -> None:
    write_text(path, json.dumps(value, ensure_ascii=False, indent=2) + "\n")


def add_generated_files(output: Path) -> None:
    write_text(
        output / "README.md",
        """# SEAC anonymous-review artifact scaffold

This package contains release-safe code, frozen configurations, annotation
schemas, tests, and invented smoke-test data for the SEAC paper. It deliberately
excludes raw enterprise documents, document-derived annotation workbooks, real
questions and gold evidence, project source names, and experiment outputs.

This build is a scaffold, not the final submission artifact: human boundary,
QA, and privacy review is still incomplete, and no formal locked-test result is
included. `BUILD_STATUS.json` records those gates explicitly.

Run the unit tests:

```bash
python3 -m unittest discover -s tests
```

Run the synthetic CPU smoke test using the command in
`examples/synthetic/README.md`. Formal reproduction requires controlled-access
inputs whose hashes must match the run manifest; those inputs are not present.
""",
    )
    write_text(
        output / "REVIEW_ONLY_NOTICE.md",
        """# Review-only notice

This scaffold is supplied for confidential scholarly review and reproducibility
inspection. It does not grant a public software or data license. A repository
license and persistent identifier must be chosen and approved by the authors and
data owner before archival public release.
""",
    )
    write_json(
        output / "BUILD_STATUS.json",
        {
            "artifact_version": "industrial-seac-anonymous-review-scaffold-v1.0",
            "synthetic_examples_only": True,
            "human_review_complete": False,
            "formal_locked_test_results_included": False,
            "submission_ready": False,
        },
    )


def scan_package(output: Path) -> dict:
    findings = []
    scanned = 0
    for path in sorted(item for item in output.rglob("*") if item.is_file()):
        relative = path.relative_to(output).as_posix()
        scanned += 1
        if path.suffix.lower() not in ALLOWED_SUFFIXES:
            findings.append({"file": relative, "rule": "unexpected_file_type"})
            continue
        text = path.read_text(encoding="utf-8")
        for literal in BANNED_ENTERPRISE_LITERALS:
            if literal in text or literal in relative:
                findings.append({"file": relative, "rule": "enterprise_identifier"})
        lowered = text.casefold()
        for term in BANNED_SCOPE_TERMS:
            if term in lowered or term in relative.casefold():
                findings.append({"file": relative, "rule": "out_of_scope_dataset"})
        if USER_PATH.search(text):
            findings.append({"file": relative, "rule": "absolute_user_path"})
        if PRIVATE_IPV4.search(text):
            findings.append({"file": relative, "rule": "private_ipv4"})
        if path.suffix.lower() in {".json", ".md", ".txt", ".bib"} and CREDENTIAL_VALUE.search(text):
            findings.append({"file": relative, "rule": "credential_like_value"})
    return {
        "protocol_version": "industrial-seac-release-scan-v1.0",
        "scanned_file_count": scanned,
        "finding_count": len(findings),
        "clean": not findings,
        "findings": findings,
        "excluded_categories": [
            "raw enterprise documents",
            "document-derived annotation workbooks",
            "real QA and boundary labels",
            "experiment outputs",
            "financial datasets and scripts",
        ],
    }


def build(output: Path, archive: Path) -> dict:
    if output.exists() or archive.exists():
        raise FileExistsError("Release output already exists; choose a new versioned path")
    output.mkdir(parents=True)
    copied = []
    for source in source_files():
        relative = source.relative_to(ROOT)
        destination = output / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, destination)
        copied.append(relative.as_posix())
    add_generated_files(output)
    payload = []
    for path in sorted(item for item in output.rglob("*") if item.is_file()):
        payload.append(
            {
                "path": path.relative_to(output).as_posix(),
                "bytes": path.stat().st_size,
                "sha256": sha256(path),
            }
        )
    write_json(
        output / "PACKAGE_MANIFEST.json",
        {
            "protocol_version": "industrial-seac-release-manifest-v1.0",
            "source_file_count": len(copied),
            "payload_file_count": len(payload),
            "files": payload,
        },
    )
    scan = scan_package(output)
    write_json(output / "PRIVACY_SCAN_REPORT.json", scan)
    if not scan["clean"]:
        raise ValueError(f"Anonymous package scan failed: {scan['findings']}")
    archive.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(archive, "x", compression=zipfile.ZIP_DEFLATED) as handle:
        for path in sorted(item for item in output.rglob("*") if item.is_file()):
            handle.write(path, arcname=(output.name + "/" + path.relative_to(output).as_posix()))
    return {
        "output": str(output),
        "archive": str(archive),
        "archive_sha256": sha256(archive),
        "source_file_count": len(copied),
        "scan": scan,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description="Build a whitelist-only SEAC review artifact scaffold.")
    # Points at the current version, which already exists, so a default run stops
    # rather than silently minting a stale duplicate; bump both when repackaging.
    parser.add_argument("--output", default="release/anonymous_review_scaffold_v2")
    parser.add_argument("--archive", default="release/SEAC_anonymous_review_scaffold_v2.zip")
    args = parser.parse_args()
    output = Path(args.output) if Path(args.output).is_absolute() else ROOT / args.output
    archive = Path(args.archive) if Path(args.archive).is_absolute() else ROOT / args.archive
    print(json.dumps(build(output, archive), ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
