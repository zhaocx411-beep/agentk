from __future__ import annotations

import argparse
import json
import re
from collections import Counter
from pathlib import Path


URL_PATTERN = re.compile(r"(?:https?://|www\.|\b(?:10|127)\.\d{1,3}\.\d{1,3}\.\d{1,3}\b)", re.IGNORECASE)
EMAIL_PATTERN = re.compile(r"\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b", re.IGNORECASE)
PHONE_PATTERN = re.compile(r"(?<!\d)1[3-9]\d{9}(?!\d)")
SECRET_PATTERN = re.compile(
    r"(?:password|passwd|secret|access[_-]?token|api[_-]?key|private[_-]?key|口令|密码|密钥|令牌)",
    re.IGNORECASE,
)


def review_text(item: dict) -> str:
    fields = (
        "question",
        "reference_answer",
        "gold_evidence_span",
        "source_document",
        "source_section",
    )
    return "\n".join(str(item.get(field, "")) for field in fields)


def detect_flags(item: dict, sensitive_identifiers: list[str] | None = None) -> list[str]:
    text = review_text(item)
    flags = set(item.get("privacy_flags") or [])
    if URL_PATTERN.search(text):
        flags.add("INTERNAL_NETWORK_OR_URL")
    if EMAIL_PATTERN.search(text) or PHONE_PATTERN.search(text):
        flags.add("PERSON_NAME_OR_CONTACT")
    if SECRET_PATTERN.search(text):
        flags.add("SECRET_OR_CREDENTIAL")
    if any(identifier and identifier in text for identifier in (sensitive_identifiers or [])):
        flags.add("CUSTOMER_OR_VENDOR_IDENTITY")
    return sorted(flags)


def prepare(records: list[dict], sensitive_identifiers: list[str] | None = None) -> tuple[list[dict], dict]:
    output = []
    counts = Counter()
    for item in records:
        prepared = dict(item)
        prepared["privacy_flags"] = detect_flags(prepared, sensitive_identifiers)
        prepared["privacy_review_status"] = "PENDING"
        prepared.setdefault("privacy_review_notes", "")
        output.append(prepared)
        counts.update(prepared["privacy_flags"])
    return output, {
        "protocol_version": "industrial-seac-privacy-prescreen-v1.0",
        "record_count": len(output),
        "privacy_status": "HUMAN_REVIEW_PENDING",
        "automatic_flag_counts": dict(sorted(counts.items())),
        "warning": "Automatic flags are a pre-screen only and never constitute privacy clearance.",
    }


def main() -> None:
    parser = argparse.ArgumentParser(description="Prepare a non-approved QA privacy review copy.")
    parser.add_argument("--input", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--summary-output", required=True)
    parser.add_argument("--sensitive-identifiers", nargs="*", default=[])
    args = parser.parse_args()

    records = json.loads(Path(args.input).read_text(encoding="utf-8"))
    prepared, summary = prepare(records, args.sensitive_identifiers)
    Path(args.output).write_text(json.dumps(prepared, ensure_ascii=False, indent=2), encoding="utf-8")
    Path(args.summary_output).write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
