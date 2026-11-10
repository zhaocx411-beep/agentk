"""Deterministic §4.1 noise preconditions for boundary candidates.

The first verification round exposed three families of adjacent units that carry
no retrievable evidence but were not enumerated in §4.1, so the human verifier and
the LLM pre-labeller each applied the noise-priority clause to a different half of
them. The disagreement was fully one-directional in every family, which is the
signature of a definitional gap rather than of careless labelling.

These rules encode the clause §4.1 already states — "左右任一单元 ... 无法形成内容证据
的残片，就标记 NOISE" — for the three families. They are deliberately narrow: each
one fires only on units that are *entirely* a structural label, a punctuation
fragment or a signature line, so a unit carrying any substantive content is left to
human judgment. Rows the rules decide are reported separately and excluded from the
agreement statistic, because a label neither side judged says nothing about
human-model agreement.

The rules never rewrite what an annotator wrote in the workbook. They are applied at
promotion time and recorded as `label_source=RULE_4_1` so the provenance of every
promoted label stays auditable.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from seac_chunking.text import is_noise_text  # noqa: E402

# §4.1.1 Bare structural labels. A unit that is only a form label ("输出", "处理逻辑")
# names a slot rather than filling one, so no query can be answered from it.
STRUCTURAL_LABELS = {
    "无",
    "无。",
    "处理",
    "处理逻辑",
    "输入",
    "输出",
    "输入参数",
    "输出参数",
    "请求参数",
    "返回参数",
    "请求信息",
    "返回信息",
    "请求信息样例",
    "返回信息样例",
    "请求示例",
    "返回示例",
    "返回码",
    "目录",
    "返回",
    "备注",
    "说明",
    "略",
}

# §4.1.2 Conversion fragments: brackets, quotes, separators and digits only.
FRAGMENT_RE = re.compile(r"^[\{\}\[\]\(\)<>,;:\.\"'`/\\\|\-—_=\+\*&%#~\s0-9]+$")

# §4.1.3 Signature blocks closing a meeting record: an organisation name or a role
# followed by a colon, and bare date lines. Both are document furniture.
ORGANISATION_RE = re.compile(r"(公司|集团|有限|研究院|设计院|中心|厂|大学|学院|局|部门)")
SIGNATURE_TAIL_RE = re.compile(r"[:：]\s*$")
DATE_ONLY_RE = re.compile(r"^\d{2,4}\s*[年\-/\.]\s*\d{1,2}\s*[月\-/\.]?\s*(\d{1,2}\s*[日号]?)?$")
SIGNATURE_ROLES = {"签字", "签名", "盖章", "签署", "参会人员", "参加人员", "记录人", "编制", "审核", "批准"}

MAX_SIGNATURE_LENGTH = 30


def normalize(text: str) -> str:
    return re.sub(r"\s+", "", text or "")


def structural_label(text: str) -> bool:
    """§4.1.1 — the unit is nothing but a form label."""

    normalized = normalize(text)
    if not normalized:
        return False
    stripped = normalized.rstrip("：:。.")
    return normalized in STRUCTURAL_LABELS or stripped in STRUCTURAL_LABELS


def conversion_fragment(text: str) -> bool:
    """§4.1.2 — the unit is punctuation or digits left over from conversion."""

    normalized = normalize(text)
    return bool(normalized) and bool(FRAGMENT_RE.match(normalized))


def signature_block(text: str) -> bool:
    """§4.1.3 — the unit is a signature, sign-off or bare date line."""

    normalized = normalize(text)
    if not normalized or len(normalized) > MAX_SIGNATURE_LENGTH:
        return False
    if DATE_ONLY_RE.match(normalized):
        return True
    stripped = normalized.rstrip("：:")
    if stripped in SIGNATURE_ROLES:
        return True
    # An organisation name on its own line, closed by a colon, is a sign-off rather
    # than a sentence about the organisation.
    return bool(SIGNATURE_TAIL_RE.search(normalized)) and bool(ORGANISATION_RE.search(stripped))


RULES = (
    ("STRUCTURAL_LABEL", structural_label),
    ("CONVERSION_FRAGMENT", conversion_fragment),
    ("SIGNATURE_BLOCK", signature_block),
)


def unit_reason(text: str) -> str | None:
    """Name the §4.1 family a single unit falls into, or None if it carries content."""

    for name, predicate in RULES:
        if predicate(text):
            return name
    # Keep the chunker's own noise notion in force so the two never diverge.
    if normalize(text) and is_noise_text(text):
        return "CHUNKER_NOISE"
    return None


def classify(left_evidence: str, right_evidence: str) -> tuple[bool, str | None]:
    """Decide whether §4.1 determines this boundary, and say which side triggered it.

    §4.1 uses "任一" — either side being evidence-free is enough, because a boundary
    can only be judged when both sides could stand as retrieval evidence.
    """

    for side, evidence in (("LEFT", left_evidence), ("RIGHT", right_evidence)):
        reason = unit_reason(evidence)
        if reason:
            return True, f"{side}_{reason}"
    return False, None
