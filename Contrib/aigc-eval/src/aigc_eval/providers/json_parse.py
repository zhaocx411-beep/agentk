"""从模型文本里抠出 JSON 对象，容忍常见的畸形输出。

与具体 provider 无关：只要是"让模型输出 JSON"就会遇到这些畸形。原先住在
gemini.py 里，Gemini 直连通道下线后搬到这里。
"""

from __future__ import annotations

import json
import re
from typing import Any


def extract_json_object(text: str) -> dict[str, Any]:
    """解析模型输出里的 JSON 对象。

    级联策略：围栏/裸对象提取 → 直接解析 → 去尾逗号 → 截断补全。
    只有全部策略都失败才抛 ValueError，让调用方决定是否重试模型调用。
    """
    cleaned = text.strip()
    fence = re.search(r"```(?:json)?\s*(\{.*\})\s*```", cleaned, re.DOTALL)
    if fence:
        cleaned = fence.group(1)

    candidates: list[str] = [cleaned]
    start = cleaned.find("{")
    end = cleaned.rfind("}")
    if start >= 0 and end > start:
        candidates.append(cleaned[start : end + 1])
    repaired = _balanced_repair(cleaned)
    if repaired:
        candidates.append(repaired)

    for candidate in candidates:
        for variant in (candidate, _strip_trailing_commas(candidate)):
            try:
                parsed = json.loads(variant)
            except (json.JSONDecodeError, TypeError):
                continue
            if isinstance(parsed, dict):
                return parsed
    raise ValueError(f"无法从模型输出解析 JSON（{len(text)} chars）：{text[:200]}")


def _strip_trailing_commas(text: str) -> str:
    """去掉紧贴右括号的逗号。"""
    return re.sub(r",(\s*[}\]])", r"\1", text)


def _balanced_repair(text: str) -> str | None:
    """从第一个 ``{`` 起扫一遍，把未闭合的字符串与括号补上。

    对付的是最常见的失败形态：输出在对象中间被截断（finishReason=MAX_TOKENS）
    或漏了个右括号。扫描时跟踪字符串状态与转义，所以字符串字面量里的括号会被忽略。
    """
    start = text.find("{")
    if start < 0:
        return None
    stack: list[str] = []
    in_str = False
    esc = False
    out: list[str] = []
    for ch in text[start:]:
        out.append(ch)
        if esc:
            esc = False
            continue
        if ch == "\\":
            esc = True
            continue
        if ch == '"':
            in_str = not in_str
            continue
        if in_str:
            continue
        if ch in "{[":
            stack.append(ch)
        elif ch == "}":
            if stack and stack[-1] == "{":
                stack.pop()
        elif ch == "]":
            if stack and stack[-1] == "[":
                stack.pop()
    repaired = "".join(out)
    if in_str:  # 在字符串字面量里被截断
        repaired += '"'
    for open_ch in reversed(stack):  # 补上未闭合的结构
        repaired += "}" if open_ch == "{" else "]"
    return repaired
