"""Deterministic §6.1 self-containment preconditions for QA questions.

§6.1 requires a question to stand on its own, naming neither the file it came from
nor the block or section number it was drafted against. The first human approval
round applied that clause unevenly on one family: a question whose subject was a
section *number* was rejected, while a question that merely carried a leading
section number in front of an otherwise self-contained subject was approved. Ten
approved rows carried such a prefix.

This rule encodes the part of §6.1 that needs no judgement: a question must not
begin with a section number. It is deliberately narrow. It fires only on a leading
dotted numeral group whose first component is one or two digits, which is what a
section number looks like in these documents, and never on a date — a four-digit
year fails the first component, and `2025年11月10日` carries no dots at all. A
trailing measure word is excluded so a genuine quantity ("1.5 秒") is left alone.

The rule never rewrites what a reviewer wrote in the workbook. It is applied at
import time, the affected rows are reported separately, and the excluded records
are recorded as `RULE_6_1` so the provenance of the final question set stays
auditable.
"""

from __future__ import annotations

import re

# A section number in these documents: one or two digits, then at least one more
# dotted component. `2.1.2.1`, `2.1.2.2` and `3.2` match; `2025.7.22` does not,
# because a four-digit year cannot be the first component.
SECTION_NUMBER = re.compile(r"^\s*\d{1,2}(?:\.\d{1,2})+")

# Characters that turn a leading decimal into a quantity rather than a section
# number, so "1.5 秒" and "2.5 万" stay out of scope.
MEASURE_WORDS = set("秒分时倍个米级层年月日万亿元吨度%")

RULE_ID = "RULE_6_1"
REASON = "question_begins_with_section_number"


def section_number_prefix(question: str) -> str:
    """Return the leading section number of a question, empty when there is none."""

    match = SECTION_NUMBER.match(question or "")
    if not match:
        return ""
    tail = (question or "")[match.end() :]
    if tail[:1] in MEASURE_WORDS:
        return ""
    return match.group(0).strip()


def violates_self_containment(question: str) -> bool:
    """Report whether §6.1 mechanically rules this question out."""

    return bool(section_number_prefix(question))


def classify(question: str) -> dict:
    """Describe the §6.1 verdict for one question, for reporting and provenance."""

    prefix = section_number_prefix(question)
    if not prefix:
        return {"excluded": False, "rule": "", "reason": "", "prefix": ""}
    return {"excluded": True, "rule": RULE_ID, "reason": REASON, "prefix": prefix}
