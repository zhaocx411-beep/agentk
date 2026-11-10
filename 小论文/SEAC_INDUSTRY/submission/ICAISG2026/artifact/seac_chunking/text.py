from __future__ import annotations

import math
import re
from collections import Counter


TOKEN_RE = re.compile(r"[\u4e00-\u9fff]|[A-Za-z0-9_]+|[^\w\s\u4e00-\u9fff]")
PAGE_FOOTER_RE = re.compile(r"(版权所有|第\s*\d+\s*页[，,]\s*共\s*\d+\s*页|copyright)", re.I)


def tokenize(text: str) -> list[str]:
    return TOKEN_RE.findall(text.lower())


def token_count(text: str) -> int:
    return len(tokenize(text))


def char_ngrams(text: str, n: int = 2) -> Counter:
    normalized = re.sub(r"\s+", "", text.lower())
    if len(normalized) < n:
        return Counter([normalized]) if normalized else Counter()
    return Counter(normalized[index : index + n] for index in range(len(normalized) - n + 1))


def cosine_sparse(left: Counter, right: Counter) -> float:
    if not left or not right:
        return 0.0
    dot = sum(value * right.get(key, 0.0) for key, value in left.items())
    left_norm = math.sqrt(sum(value * value for value in left.values()))
    right_norm = math.sqrt(sum(value * value for value in right.values()))
    return dot / (left_norm * right_norm) if left_norm and right_norm else 0.0


def percentile(values: list[float], q: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    position = max(0.0, min(1.0, q)) * (len(ordered) - 1)
    lower = int(position)
    upper = min(lower + 1, len(ordered) - 1)
    fraction = position - lower
    return ordered[lower] * (1 - fraction) + ordered[upper] * fraction


def split_tokens(text: str, size: int, overlap: int = 0) -> list[str]:
    tokens = tokenize(text)
    if not tokens:
        return []
    step = max(1, size - overlap)
    return [" ".join(tokens[start : start + size]) for start in range(0, len(tokens), step)]


def split_structured_text(text: str, target_tokens: int, max_tokens: int) -> list[str]:
    """Split a long unit on internal logical boundaries before token fallback."""
    segments = [
        segment.strip()
        for segment in re.split(r"(?<=[。；！？])|[\r\n]+|(?=\s*\d+[、）])|(?=\s*[-•]\s+)", text)
        if segment.strip()
    ]
    pieces = []
    current = []
    current_tokens = 0
    for segment in segments:
        segment_tokens = token_count(segment)
        if segment_tokens > max_tokens:
            if current:
                pieces.append("\n".join(current))
                current, current_tokens = [], 0
            pieces.extend(split_tokens(segment, max_tokens, 0))
            continue
        if current and current_tokens + segment_tokens > target_tokens:
            pieces.append("\n".join(current))
            current, current_tokens = [], 0
        current.append(segment)
        current_tokens += segment_tokens
    if current:
        pieces.append("\n".join(current))
    return pieces or split_tokens(text, max_tokens, 0)


def is_noise_text(text: str) -> bool:
    """Identify navigation/footer/noise units that should not become retrieval children."""
    normalized = re.sub(r"\s+", "", text)
    if not normalized or PAGE_FOOTER_RE.search(text):
        return True
    if normalized in {"处理", "输出", "输入", "目录", "返回", "请求信息", "返回信息"}:
        return True
    if len(normalized) >= 30:
        counts = Counter(normalized)
        if counts.most_common(1)[0][1] / len(normalized) >= 0.72:
            return True
    return False
