from __future__ import annotations

import argparse
import json
import re
from pathlib import Path


REQUIRED_SECTIONS = (
    "## Abstract",
    "## 1. Introduction",
    "## 2. Background and Related Work",
    "## 3. Task Formulation",
    "## 4. SEAC",
    "## 5. Industrial Study Design",
    "## 6. Results",
    "## 7. Discussion",
    "## 8. Threats to Validity",
    "## 9. Ethics, Privacy, and Data Availability",
    "## 10. Conclusion",
    "## Declaration of generative AI and AI-assisted technologies in the manuscript preparation process",
    "## References",
)

def load_banned_identifiers() -> tuple[str, ...]:
    identifiers = ["PRIVATE_PROJECT_IDENTIFIER"]
    private_path = Path(__file__).resolve().parents[1] / "workbench/private_project_identifiers.json"
    if private_path.is_file():
        values = json.loads(private_path.read_text(encoding="utf-8"))
        if not isinstance(values, list) or not all(isinstance(value, str) and value for value in values):
            raise ValueError("Private project identifier file must be a non-empty string list")
        identifiers.extend(values)
    return tuple(dict.fromkeys(identifiers))


BANNED_IDENTIFIERS = load_banned_identifiers()
VALIDATION_ONLY_NUMBERS = ("0.8722", "0.7352", "0.8062", "0.9259")


def lint(text: str, require_final: bool = False) -> dict:
    errors = []
    warnings = []
    for section in REQUIRED_SECTIONS:
        if section not in text:
            errors.append(f"missing section: {section}")
    placeholders = sorted(set(re.findall(r"\{\{FORMAL_[A-Z0-9_]+\}\}", text)))
    if placeholders:
        message = f"{len(placeholders)} unresolved formal-result placeholders"
        (errors if require_final else warnings).append(message)
    for identifier in BANNED_IDENTIFIERS:
        if identifier in text:
            errors.append(f"non-anonymized project identifier appears: {identifier}")
    for number in VALIDATION_ONLY_NUMBERS:
        if number in text:
            errors.append(f"validation-only P01 metric appears in manuscript: {number}")
    body, _, reference_section = text.partition("## References")
    references = [int(value) for value in re.findall(r"^\[(\d+)\] ", reference_section, flags=re.MULTILINE)]
    if references != list(range(1, len(references) + 1)):
        errors.append("reference list is not sequential from 1")
    cited = {int(value) for value in re.findall(r"\[(\d+)\]", body)}
    missing_references = sorted(cited - set(references))
    if missing_references:
        errors.append(f"citations lack reference entries: {missing_references}")
    uncited_references = sorted(set(references) - cited)
    if uncited_references:
        errors.append(f"reference entries are not cited in the manuscript body: {uncited_references}")
    return {
        "formal_placeholder_count": len(placeholders),
        "reference_count": len(references),
        "cited_reference_count": len(cited),
        "errors": errors,
        "warnings": warnings,
        "clean": not errors and (not require_final or not placeholders),
    }


def main() -> None:
    parser = argparse.ArgumentParser(description="Prevent draft or sensitive material from entering the SEAC submission.")
    parser.add_argument("--manuscript", default="manuscript/seac_industry_manuscript.md")
    parser.add_argument("--report", default="manuscript/manuscript_lint.json")
    parser.add_argument("--require-final", action="store_true")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    manuscript = Path(args.manuscript)
    if not manuscript.is_absolute():
        manuscript = root / manuscript
    report_path = Path(args.report)
    if not report_path.is_absolute():
        report_path = root / report_path
    report = lint(manuscript.read_text(encoding="utf-8"), args.require_final)
    report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False, indent=2))
    if not report["clean"]:
        raise ValueError("Manuscript lint failed")


if __name__ == "__main__":
    main()
