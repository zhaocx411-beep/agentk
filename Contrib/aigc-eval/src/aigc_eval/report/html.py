"""把一次 native8-v1 判定渲染成单文件、可离线归档的 HTML 证据报告。

面向的读者是**复核者**，不是使用者：页面要能回答「这个结论是怎么来的」——
看了哪些帧、编译出哪些约束、模型陈述了哪些事实、程序采信了哪几条、每一步的
模型原始输入输出是什么。所有图片以 base64 内联，因此产物不依赖任何服务器，
可以直接丢进邮件或工单里。

三条本模块特有的不变量，改动时请一并保住：

1. **只接受 native8-v1**。上一版协议的 `score_total` / `final` / `annotation`
   已经不再产出，页面上也不能有等价的复合分或整体状态标签 —— 单一数字会让一条
   编造出来的结论被平均掉，这正是换协议要消灭的失效模式。
2. **`tier` 绝不单独示人**。定档规则对「没有任何 violated」的维度一律给 1.0，
   所以 28 条子判据全是 `not_observable` 的维度同样拿到 `score=1.0 / tier=无问题`。
   区分「查过没问题」和「压根没查」的字段是 `decision_status` 与
   `unresolved_subcriteria`；页面把还有悬置子判据的维度单独降为「未完全核实」一档
   （与 studio 前端 `verdict-glyphs.js` 的第五档同名同形），不允许它以绿色示人。
3. **形状 + 文字 + 颜色，颜色永远排第三**。每个判定状态都另有一个 SVG 形状和一句
   中文；`not_observable` 独占虚线圆环，斜纹底纹在整页里只表示「不完整」。脱色打印
   或色觉障碍下，判定仍然读得出来。这套编码与 studio 前端共用一份定义，两个界面
   不能教出互相矛盾的视觉语言。
"""

from __future__ import annotations

import base64
import html
import json
import os
import re
import subprocess
import tempfile
from pathlib import Path
from typing import Any

from aigc_eval import paths
from aigc_eval.config import LIVE_PROVIDERS
from aigc_eval.judge.native8_recheck import AUDIO_NOT_HEARD_REASON


RUNTIME_ROOT = paths.RUNTIME_ROOT
REPORT_ROOT = paths.REPORT_ROOT
TRACE_ROOT = paths.TRACE_ROOT
WORK_ROOT = paths.WORK_ROOT

VIEW_TREE_RAW = os.environ.get("VIDEO_JUDGE_REPORT_VIEW_TREE", "").strip()
VIEW_HOST = os.environ.get("VIDEO_JUDGE_REPORT_VIEW_HOST", "").strip().rstrip("/")
VIEW_TREE = Path(VIEW_TREE_RAW) if VIEW_TREE_RAW else None

SCOPE_ZH = {"issue": "原子问题"}

# 每条子判据「应该怎样」的正面表述。issue 自身只描述「实际怎样」，缺了这一半
# 复核者无法判断分歧出在事实还是标准上。prompt 里有对应约束原文时优先用原文，
# 拿不到才退回这里的通用表述。`report/vs_human.py` 也按 key 取用同一份措辞。
NATIVE_REQUIREMENT_TEXT = {
    "instruction_subject_attributes": "视频应满足 Prompt 对主体数量与属性的明确要求。",
    "instruction_action_relation": "视频应满足 Prompt 对动作、对象及时空关系的明确要求。",
    "instruction_camera": "视频应满足 Prompt 对景别、视角和运镜的明确要求。",
    "instruction_style_environment": "视频应满足 Prompt 对风格与环境的明确要求。",
    "instruction_multishot": "视频应完成 Prompt 明确要求的多镜头计划。",
    "instruction_requested_text_audio": "视频应满足 Prompt 对文字或音频内容的明确要求。",
    "instruction_v2v_edit": "输出视频应完成 Prompt 明确要求的编辑操作。",
    "reference_input_fidelity": "生成结果应保持输入参考主体及静态细节一致。",
    "reference_v2v_preservation": "V2V 未要求编辑的内容应保持源视频特征。",
    "reference_v2v_motion": "V2V 未要求改变的动态应保持源视频运动特征。",
    "visual_inshot_temporal": "同一镜头内主体与物体的外观、结构和状态应保持连续。",
    "visual_object_disappearance": "同一镜头内物体不得在没有合理原因时凭空消失。",
    "visual_multishot_continuity": "跨镜头的主体、物体和场景状态应合理承接。",
    "audio_signal_quality": "音频应清晰稳定，不出现明显底噪、失真、削波或响度异常。",
    "audio_av_sync": "可对齐的说话或动作应与声音同步。",
    "audio_temporal_consistency": "音频前后的音色、响度和空间感应保持合理一致。",
    "picture_clarity_codec": "画面应清晰，不出现影响观看的模糊或编解码异常。",
    "picture_text_clarity": "正常播放时画面中的文字应保持清晰可辨。",
    "picture_exposure_color": "画面曝光与色彩应自然稳定。",
    "picture_composition_static": "画面构图及静态内容应合理。",
    "picture_structural_visibility": "正常速度播放时，主体和物体结构应自然完整。",
    "motion_physics_structure": "运动过程应符合基本物理与结构规律。",
    "motion_coordination_stability": "动作与镜头运动应协调稳定。",
    "motion_temporal_disappearance": "连续运动过程中物体不得无因消失。",
    "motion_unrequested_cut": "非 Prompt 要求的切镜不得造成动作或主体状态突兀跳变。",
    "text_legibility_gibberish": "画面文字字形应正确可辨，不应出现乱码。",
    "text_position_correctness": "画面文字的位置与内容应合理正确。",
    "story_causality_continuity": "事件的因果、承接、动机和内容常识应合理连贯。",
}


# ---------------------------------------------------------------------------
# 判定视觉编码（与 static/studio/verdict-glyphs.js 同源，几何一致）
# ---------------------------------------------------------------------------


def _glyph(body: str) -> str:
    return (
        '<svg class="vglyph" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"'
        ' fill="none" stroke="currentColor" stroke-width="1.5"'
        f' stroke-linecap="round" stroke-linejoin="round">{body}</svg>'
    )


GLYPH_PASS = _glyph('<circle cx="8" cy="8" r="6.4" /><path d="M5.1 8.35 7.15 10.4 10.95 6.35" />')
GLYPH_VIOLATED = _glyph(
    '<circle cx="8" cy="8" r="6.4" /><path d="M5.75 5.75 10.25 10.25M10.25 5.75 5.75 10.25" />'
)
# 半个实心圆：一眼看出「不是满分也不是零分」，无需读色。
GLYPH_FLAW = _glyph(
    '<circle cx="8" cy="8" r="6.4" />'
    '<path d="M8 1.6a6.4 6.4 0 0 0 0 12.8z" fill="currentColor" stroke="none" />'
)
# 虚线描边在整套编码里是 not_observable 独占的记号，别处一律实线。
GLYPH_NOT_OBSERVABLE = _glyph(
    '<circle cx="8" cy="8" r="6.4" stroke-dasharray="2.2 2.1" />'
    '<path d="M6.2 6.35a1.85 1.85 0 1 1 2.5 1.75c-.48.2-.76.6-.76 1.12v.28" />'
    '<circle cx="8" cy="11.75" r=".92" fill="currentColor" stroke="none" />'
)
GLYPH_NOT_APPLICABLE = _glyph('<circle cx="8" cy="8" r="6.4" /><path d="M5.5 8h5" />')
GLYPH_REVIEW = _glyph(
    '<path d="M8 1.9 14.1 12.6a1 1 0 0 1-.87 1.5H2.77a1 1 0 0 1-.87-1.5z" stroke-dasharray="2.4 2" />'
    '<path d="M8 6.4v3" /><circle cx="8" cy="11.6" r=".9" fill="currentColor" stroke="none" />'
)

VERDICT_VIEW = {
    "pass": ("pass", "通过", GLYPH_PASS),
    "violated": ("violated", "违反", GLYPH_VIOLATED),
    "not_observable": ("not_observable", "未核实", GLYPH_NOT_OBSERVABLE),
    "not_applicable": ("not_applicable", "不适用", GLYPH_NOT_APPLICABLE),
}

# 维度档位。`unverified` 不是后端档位，见 `_dimension_state`。
TIER_VIEW = {
    "bad": ("恶劣错误", GLYPH_VIOLATED),
    "flaw": ("瑕疵", GLYPH_FLAW),
    "unverified": ("未完全核实", GLYPH_NOT_OBSERVABLE),
    "ok": ("无问题", GLYPH_PASS),
    "na": ("不适用", GLYPH_NOT_APPLICABLE),
}
# 坏的在前，「不适用」垫底。计数条按它排。
TIER_ORDER = ("bad", "flaw", "unverified", "ok", "na")


def _dimension_state(dimension: dict[str, Any]) -> str:
    """维度在页面上的档位键。

    后端对「子判据全是 not_observable」的维度算出的 score 是 1.0 —— 没有任何一条
    事实把它压下来。照搬就会印出一个绿色「无问题」，而真实情况是一条都没核实过。
    所以只要还有子判据悬着就不允许它以「无问题」示人，降级成 unverified 单列一档。
    已经查出缺陷的维度（0 / 0.5）保留原档位：缺陷是确定的，未核实的部分另行标注。
    """
    if dimension.get("applicable") is False:
        return "na"
    score = dimension.get("score")
    if score == 0:
        return "bad"
    if score == 0.5:
        return "flaw"
    if dimension.get("unresolved_subcriteria"):
        return "unverified"
    return "ok"


# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------


def _esc(value: Any) -> str:
    return html.escape("" if value is None else str(value))


def _pretty(value: Any) -> str:
    if value is None or value == "":
        return "—"
    if isinstance(value, str):
        return value
    try:
        return json.dumps(value, ensure_ascii=False, indent=2)
    except (TypeError, ValueError):
        return str(value)


def _time_seconds(value: str) -> float | None:
    value = value.strip().lower().removeprefix("t=").removesuffix("s")
    try:
        parts = [float(part) for part in value.split(":")]
    except ValueError:
        return None
    if len(parts) == 1:
        return parts[0]
    if len(parts) == 2:
        return parts[0] * 60 + parts[1]
    if len(parts) == 3:
        return parts[0] * 3600 + parts[1] * 60 + parts[2]
    return None


def _native_issue_time_range(issue: dict[str, Any]) -> str:
    """优先使用裁决给出的发生区间；只有单点时才借助证据帧补成区间。"""
    token_pattern = re.compile(r"(?:t=)?(?:\d{1,2}:){0,2}\d+(?:\.\d+)?s?", re.I)
    for row in issue.get("evidence") or []:
        if not isinstance(row, dict):
            continue
        raw = str(row.get("time") or "").strip()
        if raw in {"", "全片"}:
            continue
        values = [
            seconds for token in token_pattern.findall(raw)
            if (seconds := _time_seconds(token)) is not None
        ]
        if len(values) >= 2:
            return f"{min(values):.2f}s – {max(values):.2f}s（时间段）"
        if len(values) == 1:
            frame_times = [
                float(frame["time"])
                for frame in issue.get("evidence_frames") or []
                if isinstance(frame, dict) and isinstance(frame.get("time"), (int, float))
            ]
            if frame_times:
                return f"{min([values[0], *frame_times]):.2f}s – {max([values[0], *frame_times]):.2f}s（时间段）"
            return f"{values[0]:.2f}s – {values[0]:.2f}s（时间段）"
    frame_times = [
        float(frame["time"])
        for frame in issue.get("evidence_frames") or []
        if isinstance(frame, dict) and isinstance(frame.get("time"), (int, float))
    ]
    if frame_times:
        return f"{min(frame_times):.2f}s – {max(frame_times):.2f}s（时间段）"
    return "全片（时间段）"


def _native_issue_conclusion(
    issue: dict[str, Any], report: dict[str, Any], criterion_keys: list[str]
) -> tuple[str, str, str, str]:
    """把一条 issue 摊成「时间 / 要求 / 实际 / 解释」四段。

    「要求」取 prompt 编译出的约束原文优先、子判据通用表述兜底：人工标注写的就是
    「要求什么、实际什么」，拿子判据 key 去对照会让两栏不在同一抽象层级上。
    """
    requirements = {
        str(row.get("requirement_id") or row.get("id") or ""): str(row.get("statement") or "").strip()
        for row in report.get("prompt_requirements") or [] if isinstance(row, dict)
    }
    ref_ids = [str(value) for value in issue.get("ref_ids") or []]
    requirement_rows = [requirements[ref_id] for ref_id in ref_ids if requirements.get(ref_id)]
    if requirement_rows:
        requirement = "；".join(dict.fromkeys(requirement_rows))
    else:
        requirement = "；".join(dict.fromkeys(
            NATIVE_REQUIREMENT_TEXT[key] for key in criterion_keys if key in NATIVE_REQUIREMENT_TEXT
        )) or "视频应满足对应的 v1.1 原生八维子判据。"
    actual_rows = [
        str(row.get("detail") or "").strip()
        for row in issue.get("evidence") or [] if isinstance(row, dict) and str(row.get("detail") or "").strip()
    ]
    # 主结论只取最具体的一条；完整 occurrences 仍保留在下方证据列表中。
    actual = actual_rows[0] if actual_rows else "未记录可复核的实际表现。"
    explanation = str(issue.get("decision_summary") or "").strip()
    if not explanation:
        names = [key for key in criterion_keys if key]
        explanation = f"上述实际表现违反了对应子判据（{', '.join(names)}）。" if names else "上述实际表现构成该问题。"
    return _native_issue_time_range(issue), requirement, actual, explanation


def _data_uri(path: Path | None, *, max_bytes: int = 900_000, thumb_width: int | None = None) -> str | None:
    """Base64 data URI for an image, optionally downscaled to ``thumb_width``
    via ffmpeg first (keeps big inputs from bloating the HTML).

    缩略图是纯中间产物：以前按 ``_thumb_{stem}.jpg`` 写进 REPORT_ROOT 且从不删，
    既把垃圾留在正式产物目录，又因为所有 I2V 参考图都叫 ``01_reference.png``、
    各调用点还传不同的 thumb_width（360/480/560/1000），并发批量时多个 case 会
    抢同一个文件名互相覆盖。改成进程内唯一的临时文件并用完即删。
    """
    if not path or not Path(path).exists():
        return None
    path = Path(path)
    source = path
    thumb: Path | None = None
    if thumb_width and path.stat().st_size > max_bytes:
        handle, thumb_name = tempfile.mkstemp(prefix="judge_thumb_", suffix=".jpg")
        os.close(handle)
        thumb = Path(thumb_name)
        completed = subprocess.run(
            ["ffmpeg", "-y", "-v", "error", "-i", str(path), "-vf", f"scale={thumb_width}:-1", str(thumb)],
            capture_output=True,
            text=True,
            check=False,
        )
        if completed.returncode == 0 and thumb.stat().st_size > 0:
            source = thumb
    try:
        data = source.read_bytes()
        if len(data) > max_bytes and source is path:
            return None  # too big and could not be thumbnailed
        suffix = source.suffix.lower()
        mime = "image/png" if suffix == ".png" else "image/webp" if suffix == ".webp" else "image/jpeg"
        return f"data:{mime};base64," + base64.b64encode(data).decode("ascii")
    finally:
        if thumb is not None:
            thumb.unlink(missing_ok=True)


def _render_frames(frame_meta: list[dict[str, Any]]) -> str:
    cells = []
    for item in frame_meta:
        uri = _data_uri(item.get("path"), thumb_width=360)
        if not uri:
            continue
        time = item.get("time")
        stamp = f"{time:.2f}s" if isinstance(time, (int, float)) else "—"
        dense = item.get("dense")
        badge = '<span class="fr-dense">高运动</span>' if dense else ""
        cells.append(
            f"""<figure class="frame {'is-dense' if dense else ''}">
              <img src="{uri}" alt="keyframe {_esc(stamp)}" loading="lazy" />
              <figcaption>{_esc(stamp)} {badge}</figcaption>
            </figure>"""
        )
    return "\n".join(cells) if cells else '<p class="muted">无可嵌入关键帧。</p>'


def _render_constraints(constraints: list[dict[str, Any]]) -> str:
    rows = []
    for c in constraints:
        if not isinstance(c, dict):
            continue
        importance = str(c.get("requirement_importance") or "")
        hard = "核心" if importance == "core" else "细节" if importance == "detail" else (
            "硬" if c.get("hardness") == "hard" else "软"
        )
        rows.append(
            f"""<tr>
              <td class="mono">{_esc(c.get('requirement_id') or c.get('id'))}</td>
              <td>{_esc(c.get('category'))}</td>
              <td>{_esc(c.get('statement'))}</td>
              <td class="mono muted">{_esc(c.get('source'))}</td>
              <td><span class="pill">{_esc(c.get('dimension_id') or c.get('dimension') or 'instruction_following')}</span></td>
              <td class="center">{hard}</td>
            </tr>"""
        )
    return "\n".join(rows)


def _render_observations(observations: list[dict[str, Any]]) -> str:
    rows = []
    for o in observations:
        if not isinstance(o, dict):
            continue
        tags = " ".join(f'<span class="tag">{_esc(t)}</span>' for t in (o.get("tags") or []))
        rows.append(
            f"""<li class="obs">
              <span class="obs-time mono">{_esc(o.get('time'))}</span>
              <div class="obs-body">
                <p>{_esc(o.get('fact'))}</p>
                <div class="obs-meta">{tags}<span class="conf conf-{_esc(o.get('confidence'))}">置信 {_esc(o.get('confidence'))}</span>
                <span class="muted mono">{_esc(o.get('source'))}</span></div>
              </div>
            </li>"""
        )
    return "\n".join(rows) if rows else '<p class="muted">无观察事实。</p>'


def _render_evidence(cards: list[dict[str, Any]]) -> str:
    """问题定位证据：每条不通过项配一张原生帧截图（能定位则画红框），
    并标明是否经高清复核确认 —— 直接回答「是真实问题还是模型幻觉」。"""
    if not cards:
        return '<p class="muted">本次没有不通过项，或未能抽取到证据帧。</p>'
    items = []
    for c in cards:
        if not isinstance(c, dict):
            continue
        frame_rows = [row for row in (c.get("frames") or []) if isinstance(row, dict)]
        if not frame_rows and c.get("image_path"):
            frame_rows = [{
                "time": c.get("frame_time"), "role": "representative",
                "observation": "", "image_path": c.get("image_path"), "has_box": c.get("has_box"),
            }]
        scope = SCOPE_ZH.get(c.get("scope"), c.get("scope") or "")
        confirmed = c.get("confirmed")
        if confirmed is True:
            conf_badge = '<span class="vbadge v-bad">高清已确认存在</span>'
        elif confirmed is False:
            conf_badge = '<span class="vbadge v-ok">高清下推翻（疑似幻觉）</span>'
        else:
            conf_badge = ""
        box_note = (
            '<span class="ev-box">红框定位</span>' if c.get("has_box")
            else '<span class="muted">未提供空间定位框</span>'
        )
        frame_html: list[str] = []
        for row in frame_rows:
            ft = row.get("time")
            stamp = f"t={ft:.2f}s" if isinstance(ft, (int, float)) else _esc(ft or "—")
            uri = _data_uri(
                Path(row["image_path"]) if row.get("image_path") else None,
                thumb_width=560, max_bytes=1_400_000,
            )
            image = (
                f'<img src="{uri}" alt="evidence {stamp}" loading="lazy" />'
                if uri else '<div class="ev-noimg muted">未能抽取该时刻画面</div>'
            )
            frame_html.append(
                f'<figure class="ev-frame">{image}<figcaption><b>{_esc(row.get("role") or "representative")}</b> '
                f'<span class="mono">{stamp}</span><br>{_esc(row.get("observation") or "")}'
                f'{" · 红框定位" if row.get("has_box") else ""}</figcaption></figure>'
            )
        if not frame_html:
            frame_html.append('<div class="ev-noimg muted">整片级或缺少精确时刻，不生成误导性单帧</div>')
        reasoning = _esc(c.get("decision_summary") or c.get("note") or "—")
        counter = "、".join(str(value) for value in (c.get("counterevidence_checked") or [])) or "—"
        items.append(
            f"""<article class="ev-card">
              <div class="ev-frame-strip">{''.join(frame_html)}</div>
              <div>
                <div class="ev-head">
                  <span class="pill">{_esc(scope)}</span>
                  <strong>{_esc(c.get('title'))}</strong>
                </div>
                <p class="ev-meta">{box_note} {conf_badge}</p>
                <p class="ev-note"><b>裁决摘要：</b>{reasoning}</p>
                <p class="ev-note"><b>已检查反证：</b>{_esc(counter)}</p>
              </div>
            </article>"""
        )
    return f'<div class="ev-grid">{"".join(items)}</div>'


def _native_issue_evidence_cards(report: dict[str, Any]) -> list[dict[str, Any]]:
    """Fallback audit cards for caches created before top-level evidence was retained."""
    cards: list[dict[str, Any]] = []
    for issue in report.get("issues") or []:
        if not isinstance(issue, dict):
            continue
        evidence = [row for row in (issue.get("evidence") or []) if isinstance(row, dict)]
        first = evidence[0] if evidence else {}
        cards.append({
            "scope": "issue",
            "title": f"{issue.get('issue_id') or 'ISSUE'} · {', '.join(issue.get('ref_ids') or []) or issue.get('source') or '问题'}",
            "time": first.get("time") or "全片",
            "note": "；".join(str(row.get("detail") or "") for row in evidence if row.get("detail")) or "无文字证据",
            "image_path": None,
            "has_box": False,
        })
    return cards


def _render_cost(cost: dict[str, Any]) -> str:
    """Model-call and token cost of analyzing this one video."""
    if not isinstance(cost, dict) or not cost.get("calls"):
        return '<p class="muted">本次未调用大模型（离线或复用历史结果）。</p>'
    by_in = cost.get("calls_by_input") or {}
    by_mod = cost.get("prompt_tokens_by_modality") or {}
    mod_txt = "、".join(f"{k.upper()} {v:,}" for k, v in by_mod.items() if v) or "—"
    rows = []
    for d in cost.get("detail") or []:
        media = []
        if d.get("videos"):
            media.append(f"视频×{d['videos']}")
        if d.get("images"):
            media.append(f"图片×{d['images']}")
        media_txt = "、".join(media) or "纯文本"
        mb = (d.get("media_bytes") or 0) / 1e6
        retry = f'<span class="pill">重试 {d["attempt"]}</span>' if (d.get("attempt") or 1) > 1 else ""
        rows.append(
            f"""<tr>
              <td>{_esc(d.get('step'))} {retry}</td>
              <td class="center">{_esc(media_txt)}{f'<br><span class="muted mono">{mb:.1f} MB</span>' if mb else ''}</td>
              <td class="center mono">{int(d.get('prompt_tokens') or 0):,}</td>
              <td class="center mono">{int(d.get('output_tokens') or 0):,}</td>
              <td class="center mono muted">{int(d.get('thoughts_tokens') or 0):,}</td>
              <td class="center mono"><strong>{int(d.get('total_tokens') or 0):,}</strong></td>
              <td class="center mono muted">{int(d.get('elapsed_ms') or 0) / 1000:.1f}s</td>
            </tr>"""
        )
    return f"""
    <div class="signals" style="margin-bottom:16px">
      <div class="signal"><span>模型调用</span><strong>{cost['calls']} 次</strong></div>
      <div class="signal"><span>视频 / 图片 / 纯文本</span><strong>{by_in.get('video', 0)} / {by_in.get('image', 0)} / {by_in.get('text', 0)}</strong></div>
      <div class="signal"><span>合计 tokens</span><strong>{cost.get('total_tokens', 0):,}</strong></div>
      <div class="signal"><span>上传媒体</span><strong>{(cost.get('media_bytes', 0) / 1e6):.1f} MB</strong></div>
    </div>
    <p class="hint">输入 token 按模态：{_esc(mod_txt)} · 总耗时 {int(cost.get('elapsed_ms', 0)) / 1000:.1f}s</p>
    <div class="table-wrap">
      <table class="dim-table">
        <thead><tr><th>调用步骤</th><th class="center">输入</th><th class="center">输入 tokens</th>
        <th class="center">输出</th><th class="center">思考</th><th class="center">合计</th><th class="center">耗时</th></tr></thead>
        <tbody>{''.join(rows)}</tbody>
      </table>
    </div>"""


V2V_ALIGN_ZH = {
    "unchanged": "与源视频几乎逐帧相同（编辑疑未执行）",
    "global_edit": "全片范围的改动（整体重绘/换风格的特征）",
    "local_edit": "改动集中在若干局部时间段（局部编辑的特征）",
    "minor_diff": "全片仅有轻微差异，未检出明确的改动段",
}


def _render_v2v(report: dict[str, Any]) -> str:
    """展示 V2V 程序侧对齐测量；模型结论由原生八维卡片统一展示。"""
    align = report.get("v2v_align") if isinstance(report.get("v2v_align"), dict) else None
    if not align:
        return ""

    spans = "、".join(
        f"{_esc(s.get('span'))}（最低 {s.get('min_ssim')}）"
        for s in (align.get("edited_segments") or [])[:8]
    ) or "无"
    cells = "、".join(_esc(c) for c in (align.get("grid_changed_cells") or [])) or "无"
    grid_rows = "".join(
        f'<td class="center{" v2v-cell-hit" if c.get("changed") else ""}">'
        f'{_esc(c.get("name"))}<br><span class="mono muted">{c.get("ssim_mean")}</span></td>'
        + ("</tr><tr>" if idx % 3 == 2 and idx != 8 else "")
        for idx, c in enumerate(align.get("grid") or [])
        if isinstance(c, dict)
    )
    grid_html = (
        f'<table class="v2v-grid"><tbody><tr>{grid_rows}</tr></tbody></table>'
        if grid_rows else ""
    )
    measure = f"""<div class="v2v-measure">
              <p class="hint">下面这些数字由<strong>程序用 ffmpeg 逐时刻计算</strong>（{align.get('sample_fps')} fps ×
                {align.get('sample_count')} 个采样点），可重放核对。它只回答「哪里变了」，
                <strong>不回答改得对不对</strong>——改动段可能正是 prompt 要求的编辑，一致段也可能是本该改却没改。</p>
              <div class="v2v-kpis">
                <div><span class="muted mono">画面相似度均值</span><b>{align.get('ssim_mean')}</b><span class="muted">1.0 = 完全相同</span></div>
                <div><span class="muted mono">最低相似度</span><b>{align.get('ssim_min')}</b><span class="muted">改动最剧烈的时刻</span></div>
                <div><span class="muted mono">改动时刻占比</span><b>{align.get('changed_ratio')}</b><span class="muted">局部编辑 vs 全片重绘</span></div>
                <div><span class="muted mono">时长</span><b>{align.get('duration_source')}s → {align.get('duration_output')}s</b><span class="muted">差 {align.get('duration_delta')}s</span></div>
              </div>
              <p><strong>范围判定</strong> {_esc(V2V_ALIGN_ZH.get(str(align.get('verdict')), align.get('verdict')))}</p>
              <p><strong>检出的改动时间段</strong> {spans}</p>
              <p><strong>改动集中的画面区域</strong> {cells}
                <span class="muted">（3×3 分区，值越低改动越大；与 prompt 里的「右上角」「背景」等说法对照，
                可核对改动是否发生在被点名的位置）</span></p>
              {grid_html}
            </div>"""

    return f"""
  <section class="panel">
    <h2>V2V 专项：源视频对照</h2>
    <p class="hint">V2V 有一对<strong>互补</strong>的失败模式：<strong>该改的没改</strong>（编辑指令未达成）与
      <strong>不该改的被改了</strong>（源视频保真被破坏）。两侧都要判，且同一处变化不重复计两边。</p>
    {measure}
  </section>"""


def _render_trace(steps: list[dict[str, Any]]) -> str:
    blocks = []
    for step in steps:
        src = step.get("source", "")
        meta = " · ".join(
            x for x in [
                src,
                step.get("model") or "",
                f"{step['elapsed_ms']}ms" if step.get("elapsed_ms") is not None else "",
                f"{step['input'].get('media_count')} media" if step.get("input", {}).get("media_count") else "",
            ] if x
        )
        err = f'<p class="trace-err">错误：{_esc(step.get("error"))}</p>' if step.get("error") else ""
        sysins = step.get("input", {}).get("system_instruction") or ""
        sys_block = (
            f'<details><summary>System Instruction</summary><pre>{_esc(sysins)}</pre></details>' if sysins else ""
        )
        text_preview = step.get("input", {}).get("text_preview") or "—"
        parsed = step.get("output", {}).get("parsed_json")
        parsed_block = (
            f'<details open><summary>输出 JSON</summary><pre>{_esc(_pretty(parsed))}</pre></details>' if parsed else ""
        )
        raw = step.get("output", {}).get("raw_text")
        raw_block = f'<details><summary>输出原文</summary><pre>{_esc(raw)}</pre></details>' if raw else ""
        thought = step.get("output", {}).get("thought_summary")
        thought_block = (
            f'<details open><summary>模型思考摘要</summary><pre>{_esc(thought)}</pre></details>'
            if thought else ""
        )
        open_attr = "open" if src in LIVE_PROVIDERS else ""
        blocks.append(
            f"""<details class="trace-step" {open_attr}>
              <summary><span class="mono">#{step.get('index')}</span>
              <span class="trace-title">{_esc(step.get('title') or step.get('stage'))}</span>
              <span class="muted mono">{_esc(meta)}</span></summary>
              <div class="trace-body">
                {err}
                {sys_block}
                <details open><summary>输入文本（preview）</summary><pre>{_esc(text_preview)}</pre></details>
                {thought_block}
                {parsed_block}
                {raw_block}
              </div>
            </details>"""
        )
    return "\n".join(blocks) if blocks else '<p class="muted">无模型交互步骤。</p>'


def _actual_recheck_steps(trace: dict[str, Any]) -> list[dict[str, Any]]:
    rows = []
    for step in trace.get("steps") or []:
        if not isinstance(step, dict):
            continue
        title = str(step.get("title") or "")
        if title.startswith("4c ·"):
            rows.append(step)
    return rows


def _render_actual_rechecks(report: dict[str, Any], trace: dict[str, Any]) -> str:
    """Render only rechecks that are proven by the persisted model-call trace."""
    steps = _actual_recheck_steps(trace)
    if not steps:
        return '<p class="muted">本次未触发 4c 独立复核；4b 主判输入已在上方溯源区展示。</p>'
    blocks: list[str] = []
    for step in steps:
        parts = (step.get("input") or {}).get("parts_sanitized") or []
        counts = {"video": 0, "image": 0, "audio": 0}
        for part in parts:
            if not isinstance(part, dict) or part.get("type") != "media":
                continue
            mime = str(part.get("mime_type") or "")
            for kind in counts:
                if mime.startswith(kind + "/"):
                    counts[kind] += 1
                    break
        media = " / ".join(
            text for value, text in (
                (counts["video"], f'完整视频×{counts["video"]}'),
                (counts["image"], f'图片×{counts["image"]}'),
                (counts["audio"], f'音频×{counts["audio"]}'),
            ) if value
        ) or "纯文本"
        error = step.get("error")
        result = (step.get("output") or {}).get("parsed_json")
        error_html = f'<p class="trace-err">错误：{_esc(error)}</p>' if error else ""
        result_html = (
            f'<pre>{_esc(_pretty(result))}</pre>'
            if result else '<p class="muted">无结构化输出。</p>'
        )
        thought = (step.get("output") or {}).get("thought_summary")
        thought_html = (
            f'<details open><summary>模型思考摘要</summary><pre>{_esc(thought)}</pre></details>'
            if thought else ""
        )
        blocks.append(
            f'<details class="trace-step"><summary><span class="trace-title">{_esc(step.get("title"))}</span>'
            f'<span class="muted mono">{_esc(media)} · {"失败" if error else "完成"}</span></summary>'
            f'<div class="trace-body">'
            f'{error_html}{thought_html}{result_html}'
            f'</div></details>'
        )
    return "".join(blocks)


# ---------------------------------------------------------------------------
# page
# ---------------------------------------------------------------------------


def _load_trace_document(report: dict[str, Any]) -> dict[str, Any]:
    """Load the persisted sanitized trace for standalone HTML rendering."""
    inline = report.get("llm_trace")
    if isinstance(inline, dict) and isinstance(inline.get("steps"), list):
        return inline
    raw_path = report.get("trace_saved_path")
    if not raw_path:
        return {}
    path = Path(str(raw_path))
    try:
        resolved = path.resolve()
        trace_root = TRACE_ROOT.resolve()
        # 报告里的路径来自缓存 JSON，可能是别的机器写的。只认 trace 目录直下的文件，
        # 否则一份被改过的缓存就能让渲染读到任意路径。
        if resolved.parent != trace_root or not resolved.is_file():
            return {}
        document = json.loads(resolved.read_text(encoding="utf-8"))
    except (OSError, ValueError, json.JSONDecodeError):
        return {}
    return document if isinstance(document, dict) else {}


def _hydrate_report_artifacts(
    report: dict[str, Any], frame_meta: list[dict[str, Any]] | None, contact_path: Path | None
) -> tuple[list[dict[str, Any]], Path | None]:
    """从缓存报告里回捞可复用的帧与联系表，好让离线重渲染也带图。"""
    frames = list(frame_meta or [])
    media = report.get("media") or {}
    if contact_path is None:
        contact_url = str(media.get("contact_sheet_url") or "")
        marker = "./runtime/"
        if contact_url.startswith(marker):
            candidate = RUNTIME_ROOT / contact_url[len(marker):]
            if candidate.is_file():
                contact_path = candidate
    if frames and any(isinstance(row, dict) and Path(row.get("path", "")).is_file() for row in frames):
        return frames, contact_path
    contact_name = Path(str(media.get("contact_sheet_url") or "")).name
    work_key = contact_name.removesuffix("_contact.jpg")
    frame_dir = WORK_ROOT / work_key / "frames"
    paths_on_disk = sorted(frame_dir.glob("frame_*.jpg")) if frame_dir.is_dir() else []
    sampling_rows = [row for row in (report.get("sampling") or {}).get("frames", []) if isinstance(row, dict)]
    if paths_on_disk and sampling_rows:
        frames = [{**row, "path": path} for row, path in zip(sampling_rows, paths_on_disk)]
    return frames, contact_path


def _render_native8_dimensions(report: dict[str, Any]) -> str:
    issues = {row.get("issue_id"): row for row in report.get("issues") or [] if isinstance(row, dict)}
    issue_criteria: dict[str, list[str]] = {}
    for dimension in report.get("rubric") or []:
        for criterion in dimension.get("subcriteria") or []:
            for issue_id in criterion.get("issue_ids") or []:
                issue_criteria.setdefault(str(issue_id), []).append(str(criterion.get("key") or ""))
    cards: list[str] = []
    for dim in report.get("rubric") or []:
        score = dim.get("score")
        score_text = "—" if score is None else ("1" if score == 1 else "0" if score == 0 else "0.5")
        state = _dimension_state(dim)
        state_label, state_glyph = TIER_VIEW[state]
        unresolved = [str(key) for key in dim.get("unresolved_subcriteria") or []]

        criteria = ""
        for row in dim.get("subcriteria") or []:
            verdict_key, verdict_label, verdict_glyph = VERDICT_VIEW.get(
                str(row.get("verdict")), VERDICT_VIEW["not_observable"]
            )
            criteria += (
                f'<tr data-verdict="{verdict_key}">'
                f"<td>{_esc(row.get('name') or row.get('key'))}</td>"
                f'<td><span class="crit-verdict" data-verdict="{verdict_key}">{verdict_glyph}'
                f"{_esc(verdict_label)}</span> <code>{_esc(row.get('verdict'))}</code></td>"
                f"<td>{_esc(', '.join(row.get('issue_ids') or []) or '—')}</td></tr>"
            )

        issue_blocks: list[str] = []
        for issue_id in dim.get("feedback") or []:
            issue = issues.get(issue_id) or {}
            time_range, requirement, actual, explanation = _native_issue_conclusion(
                issue, report, issue_criteria.get(str(issue_id), [])
            )
            reviews = issue.get("specialist_reviews") or []
            issue_blocks.append(
                f"<details class='native-issue' open><summary><code>{_esc(issue_id)}</code> · {_esc(issue.get('source'))}</summary>"
                f"<div class='native-conclusion'><div class='native-time'>{_esc(time_range)}</div>"
                f"<p><b>要求：</b>{_esc(requirement)}</p>"
                f"<p><b>实际：</b>{_esc(actual)}</p>"
                f"<p><b>解释：</b>{_esc(explanation)}</p></div>"
                f"{'<h4>专项复核</h4><pre>' + _esc(_pretty(reviews)) + '</pre>' if reviews else ''}</details>"
            )

        # 程序档位与页面档位在「未完全核实」上会不一致，两个都要写出来：读者需要
        # 同时知道后端算出了什么、以及为什么页面不敢照着它显示绿色。
        meta_bits = [
            f"程序档位 <b>{_esc(dim.get('tier'))}</b>（score {_esc(score_text)}）",
            f"结论状态 <code>{_esc(dim.get('decision_status'))}</code>",
        ]
        if unresolved:
            meta_bits.append(
                f"{len(unresolved)} 项未核实 <span class='nc-unresolved'>{_esc('、'.join(unresolved))}</span>"
            )
        if dim.get("escalated"):
            meta_bits.append("三处以上瑕疵，已升级为恶劣错误")
        if dim.get("applicable") is False:
            meta_bits.append("本 case 不适用，子判据未评")

        cards.append(
            f"<section class='native-card' data-state='{state}' data-unresolved='{'1' if unresolved else '0'}'>"
            f"<span class='nc-rail' data-tier='{state}' aria-hidden='true'></span>"
            f"<header><h2>{_esc(dim.get('dimension_name'))}</h2>"
            f"<span class='nc-state' data-tier='{state}'>{state_glyph}{_esc(state_label)}</span>"
            f"<span class='score' data-tier='{state}'>{_esc(score_text)}</span></header>"
            f"<p class='nc-meta'>{' · '.join(meta_bits)}</p>"
            f"<details open><summary>子判据</summary><table class='crit-table'>"
            f"<thead><tr><th>判据</th><th>结论</th><th>问题</th></tr></thead>"
            f"<tbody>{criteria}</tbody></table></details>{''.join(issue_blocks)}</section>"
        )
    return f'<div class="native-grid">{"".join(cards)}</div>'


def _render_native8_specialists(report: dict[str, Any]) -> str:
    blocks: list[str] = []
    scan = report.get("native8_scan")
    if isinstance(scan, dict) and scan:
        summary = {
            "schema_version": scan.get("schema_version"),
            "instruction_checklist": scan.get("instruction_checklist") or [],
            "issues": scan.get("issues") or [],
            "rubric": scan.get("rubric") or [],
        }
        blocks.append(
            '<details class="trace-step" open><summary><span class="trace-title">'
            '4b · 原生八维主判</span></summary><div class="trace-body"><pre>'
            + _esc(_pretty(summary)) + '</pre></div></details>'
        )
    recheck = report.get("native8_recheck")
    if isinstance(recheck, dict) and recheck:
        blocks.append(
            '<details class="trace-step"><summary><span class="trace-title">4c · 候选原子问题增量取证</span></summary>'
            f'<div class="trace-body"><pre>{_esc(_pretty(recheck))}</pre></div></details>'
        )
    extras = report.get("observation_extras") or {}
    # 4a 排在 4c 前面：它跑在 4b 之前，是喂给主判的输入，不是对主判结果的复核。
    audio_reference = extras.get("audio_reference")
    if isinstance(audio_reference, dict) and audio_reference:
        blocks.append(
            '<details class="trace-step"><summary><span class="trace-title">4a · 参考音频对照</span></summary>'
            f'<div class="trace-body"><pre>{_esc(_pretty(audio_reference))}</pre></div></details>'
        )
    audio = extras.get("audio_recheck")
    if isinstance(audio, dict) and audio:
        blocks.append(
            '<details class="trace-step"><summary><span class="trace-title">4c · 音频独立复听</span></summary>'
            f'<div class="trace-body"><pre>{_esc(_pretty(audio))}</pre></div></details>'
        )
    # 两个"没听到"标记要出现在页面上，否则音频判据为什么全是未核实，读者无从得知
    # —— 而这正是平台静默降级时唯一的线索。
    for key, label in (
        ("audio_not_heard", "音频复听：模型自报未收到可听音频"),
        ("audio_reference_not_heard", "参考音频对照：模型自报未收到可听音频"),
    ):
        flag = extras.get(key)
        if flag:
            detail = "、".join(flag) if isinstance(flag, list) else ""
            blocks.append(
                f'<details class="trace-step"><summary><span class="trace-title">{_esc(label)}'
                + (f"（{_esc(detail)}）" if detail else "")
                + '</span></summary><div class="trace-body"><pre>'
                + _esc(AUDIO_NOT_HEARD_REASON)
                + '</pre></div></details>'
            )
    funnel = report.get("review_funnel")
    if isinstance(funnel, dict) and funnel:
        blocks.append(
            '<details class="trace-step"><summary><span class="trace-title">4b 候选 → 4c 增量证据 → 程序准入</span></summary>'
            f'<div class="trace-body"><pre>{_esc(_pretty(funnel))}</pre></div></details>'
        )
    return "".join(blocks) if blocks else '<p class="muted">本次没有可展示的专项结果。</p>'


def _render_review_funnel(report: dict[str, Any]) -> str:
    """程序未采信的判断。空的时候也要出一行 —— 否则读者无从知道这一关跑过没有。"""
    required = [row for row in report.get("review_required") or [] if isinstance(row, dict)]
    candidates = [row for row in report.get("review_candidates") or [] if isinstance(row, dict)]
    funnel = report.get("review_funnel") if isinstance(report.get("review_funnel"), dict) else {}
    if not required and not candidates:
        return f'<p class="muted">{GLYPH_PASS} 没有被程序驳回或挂起的判断。</p>'

    dimension_names = {
        str(row.get("dimension_id")): str(row.get("dimension_name") or row.get("dimension_id"))
        for row in report.get("rubric") or [] if isinstance(row, dict)
    }
    criterion_names = {
        (str(row.get("dimension_id")), str(sub.get("key"))): str(sub.get("name") or sub.get("key"))
        for row in report.get("rubric") or [] if isinstance(row, dict)
        for sub in row.get("subcriteria") or [] if isinstance(sub, dict)
    }
    reject_reason = {
        "incomplete_tier_or_dimension_evidence": "缺少定档事实或证据强度不足，程序拒绝采信",
    }
    rows = "".join(
        f"<tr><td class='mono'>{_esc(row.get('issue_id'))}</td>"
        f"<td>{_esc(dimension_names.get(str(row.get('dimension_id')), row.get('dimension_id')))}"
        f" · {_esc(criterion_names.get((str(row.get('dimension_id')), str(row.get('subcriterion'))), row.get('subcriterion')))}</td>"
        f"<td>{_esc(reject_reason.get(str(row.get('reason')), row.get('reason')))}</td></tr>"
        for row in required
    )
    funnel_bits = " · ".join(
        text for text in (
            f"采信 {funnel.get('admitted_issue_count')} 条" if funnel.get("admitted_issue_count") is not None else "",
            f"驳回 {funnel.get('gate_rejection_count')} 条" if funnel.get("gate_rejection_count") is not None else "",
            f"仍悬置 {funnel.get('unresolved_count')} 条" if funnel.get("unresolved_count") is not None else "",
        ) if text
    )
    return (
        f'<p class="review-head">{GLYPH_REVIEW} <strong>程序未采信 {len(required)} 条判断 · '
        f'待人工复核 {len(candidates)} 条</strong>'
        '<span class="muted"> 这些内容没有计入上方任何维度。</span></p>'
        + (f'<p class="hint">{_esc(funnel_bits)}</p>' if funnel_bits else "")
        + (
            '<div class="table-wrap"><table><thead><tr><th>issue</th><th>落点</th><th>驳回原因</th></tr></thead>'
            f'<tbody>{rows}</tbody></table></div>' if rows else ""
        )
        + (
            f'<details><summary>复核候选 issue 原文（{len(candidates)} 条）</summary>'
            f'<pre>{_esc(_pretty(candidates))}</pre></details>' if candidates else ""
        )
    )


def _quality_frame_seconds(value: Any) -> float | None:
    if isinstance(value, (int, float)):
        return float(value)
    if not isinstance(value, str):
        return None
    match = re.search(r"(?:(\d+):)?(\d+(?:\.\d+)?)", value)
    if not match:
        return None
    return float(match.group(1) or 0) * 60 + float(match.group(2))


def _trace_uses_full_video(trace: dict[str, Any], title_fragment: str) -> bool:
    for step in trace.get("steps") or []:
        if title_fragment not in str(step.get("title") or ""):
            continue
        parts = (step.get("input") or {}).get("parts_sanitized") or []
        if any(
            isinstance(part, dict)
            and part.get("type") == "media"
            and str(part.get("mime_type") or "").startswith("video/")
            for part in parts
        ):
            return True
    return False


def _render_quality_input_audit(report: dict[str, Any], trace: dict[str, Any]) -> str:
    """展示 4b 实际用于画面/动态判定的完整视频通道、帧与逐帧观察。"""
    scan = report.get("native8_scan") if isinstance(report.get("native8_scan"), dict) else {}
    frames = [row for row in scan.get("_frames") or [] if isinstance(row, dict)]
    per_frame = [row for row in scan.get("per_frame") or [] if isinstance(row, dict)]
    if not frames:
        return '<p class="muted">本次结果没有保存 4b 关键帧输入记录。</p>'

    def dimension_issues(dimension_id: str) -> list[str]:
        dimension = next(
            (item for item in report.get("rubric") or [] if item.get("dimension_id") == dimension_id),
            {},
        )
        return [str(value) for value in dimension.get("feedback") or []]

    finding_by_time = {
        round(value, 2): row
        for row in per_frame
        if (value := _quality_frame_seconds(row.get("time"))) is not None
    }
    cells: list[str] = []
    for frame in frames:
        value = _quality_frame_seconds(frame.get("time"))
        path = Path(str(frame.get("path") or ""))
        uri = _data_uri(path if path.exists() else None, thumb_width=360)
        if not uri:
            continue
        finding = finding_by_time.get(round(value, 2)) if value is not None else None
        detail = " · ".join(
            str((finding or {}).get(key) or "").strip()
            for key in ("picture_facts", "motion_facts")
            if str((finding or {}).get(key) or "").strip()
        ) or "模型未返回该帧的画面/动态单独说明"
        if frame.get("focus_role"):
            detail = f"观察器疑点复取/{frame.get('focus_role')}（非问题结论） · {detail}"
        stamp = f"t={value:.2f}s" if value is not None else "t=—"
        cells.append(
            f'<figure class="quality-frame"><img src="{uri}" alt="4b input {stamp}" loading="lazy" />'
            f'<figcaption><strong>{_esc(stamp)}</strong><span>{_esc(detail)}</span></figcaption></figure>'
        )
    full_video = _trace_uses_full_video(trace, "原生八维主判")
    returned_count = sum(
        1 for frame in frames
        if (value := _quality_frame_seconds(frame.get("time"))) is not None
        and round(value, 2) in finding_by_time
    )
    summary = (
        '<div class="signals">'
        f'<div class="signal"><span>画面质量 issue</span><strong>{_esc(", ".join(dimension_issues("picture_quality")) or "无")}</strong></div>'
        f'<div class="signal"><span>动态质量 issue</span><strong>{_esc(", ".join(dimension_issues("motion_quality")) or "无")}</strong></div>'
        f'<div class="signal"><span>时序输入</span><strong>{"完整视频 + 关键帧" if full_video else "关键帧"}</strong></div>'
        f'<div class="signal"><span>逐帧输出覆盖</span><strong>{returned_count}/{len(frames)}</strong></div>'
        '</div>'
    )
    return (
        summary
        + '<details class="trace-step" open><summary><span class="trace-title">4b · 原生八维主判实际帧输入</span>'
        f'<span class="muted mono">{len(frames)} 帧</span></summary><div class="trace-body">'
        '<p class="hint">完整视频负责连续运动和正常速度判断；关键帧负责静态细节与跨时刻比对。'
        '两类证据互补，关键帧不是视频输入的替代品。</p>'
        f'<div class="quality-frames">{"".join(cells) or "<p class=muted>帧文件已不可用。</p>"}</div>'
        '</div></details>'
    )


def _render_tally(rubric: list[dict[str, Any]]) -> str:
    """八维档位计数。按页面档位（含「未完全核实」）统计，不按后端 tier —— 否则
    一个一次都没核实过的维度会被算进「无问题」，headline 自己先撒了谎。"""
    counts: dict[str, int] = {}
    for dimension in rubric:
        if isinstance(dimension, dict):
            key = _dimension_state(dimension)
            counts[key] = counts.get(key, 0) + 1
    return "".join(
        f'<li class="tally-item" data-tier="{key}">{TIER_VIEW[key][1]}'
        f'<b>{counts[key]}</b><span>{_esc(TIER_VIEW[key][0])}</span></li>'
        for key in TIER_ORDER if counts.get(key)
    )


def render_report_html(
    report: dict[str, Any],
    *,
    run_id: str,
    frame_meta: list[dict[str, Any]] | None = None,
    contact_path: Path | None = None,
    input_image_path: Path | None = None,
) -> str:
    if report.get("schema_version") != "native8-v1":
        raise ValueError("report_html only accepts schema_version=native8-v1")
    frame_meta, contact_path = _hydrate_report_artifacts(report, frame_meta, contact_path)
    case = report.get("case", {})
    prompt_data = report.get("prompt", {})
    qa_gt = {
        "标签": prompt_data.get("gold_label", ""),
        "问题备注": prompt_data.get("gold_note", ""),
    }
    qa_gt_rows = "".join(
        f"<dt>{_esc(key)}</dt><dd>{_esc(value or '—')}</dd>"
        for key, value in qa_gt.items()
    )
    meta = report.get("metadata", {})
    sampling = report.get("sampling", {})
    llm = _load_trace_document(report)
    engine = report.get("engine", "")
    model_name = next((s.get("model") for s in (llm.get("steps") or []) if s.get("model")), None)

    status_text, status_cls = (
        ("评测完成 · COMPLETE", "pass")
        if report.get("analysis_status") == "complete"
        else ("评测失败 · FAILED", "blocked")
    )
    windows = sampling.get("windows", [])
    win_text = ", ".join(f"{w.get('start')}–{w.get('end')}s" for w in windows) or "无"

    input_uri = _data_uri(input_image_path, thumb_width=480)
    contact_uri = _data_uri(contact_path, thumb_width=1000)

    rubric = [row for row in report.get("rubric") or [] if isinstance(row, dict)]
    evidence_cards = report.get("evidence") or _native_issue_evidence_cards(report)
    footer_flow = "Prompt 预编译 → 全片观察 → 4b 原生八维主判 → 4c 候选增量取证 → 程序确定性三档"

    return f"""<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>{_esc(case.get('label'))} · Video Judge 评测报告</title>
<style>
/* 报告是一份能独立分发的静态文档，样式全部内嵌。改这里遵守四条：
   1. 颜色只从下面的 token 取，不在组件规则里写死色值 —— 深色模式靠覆盖
      token 实现，写死一处就会在深色下变成看不见的白底黑字。
   2. 圆角、阴影走 --r-* / --sh-* 阶梯，同层级控件必须同档。
   3. 档位色是第二信道，形状与中文才是主信道，见下面「一档一色」那段。
   4. 视觉语言与 Studio 主界面（static/studio.css）保持同源：同一套系统蓝、
      同一档圆角、同一种"半像素描边 + 贴身投影"的卡片。两边都改动时，
      先在 studio.css 定调，再搬过来 —— 那边是操作界面，约束更强。 */
:root {{
  --bg:#f4f4f7; --panel:#ffffff; --panel-alt:#fafafc; --fill:#f0f0f4;
  --ink:#1c1c1e; --soft:#585b66; --faint:#8a8d99;
  --line:rgba(20,22,40,.09); --line-soft:rgba(20,22,40,.055);
  --ok:#17864c; --mid:#a5670a; --bad:#ce3131; --na:#9aa2af;
  --ok-soft:rgba(23,134,76,.09); --mid-soft:rgba(165,103,10,.1); --bad-soft:rgba(206,49,49,.09);
  /* 系统蓝，与 Studio 的 --brand 同值 */
  --accent:#0068e0; --accent-soft:rgba(0,104,224,.08);
  --violet:#5b4bd6; --violet-soft:rgba(91,75,214,.1);
  --mono:"SF Mono","SFMono-Regular",ui-monospace,Menlo,Consolas,monospace;
  /* macOS 的圆角比通用 Web 收得紧：控件 6、分组 9、卡片 11、报头 14 */
  --r-ctl:6px; --r-sm:6px; --r-md:9px; --r-lg:11px; --r-xl:14px;
  /* 卡片的边由半像素描边给，不再用 1px border —— 十几张卡各画一圈实线，
     整页会读成表格。投影只负责"离开纸面一点"，不负责画边。 */
  --hair:0 0 0 .5px rgba(20,22,40,.14);
  --sh-1:var(--hair), 0 1px 2px rgba(16,18,40,.045);
  --sh-2:var(--hair), 0 2px 4px rgba(16,18,40,.05), 0 12px 30px -10px rgba(16,18,40,.12);
  --film:#101218; --film-line:#22252e; --film-ink:#e5e8ee;
}}

/* 报告常被单独发出去看，跟随系统深色而不是强制浅底。
   只覆盖 token，组件规则一条都不用改。 */
@media (prefers-color-scheme: dark) {{
  :root {{
    --bg:#161618; --panel:#1f1f22; --panel-alt:#26262a; --fill:#2c2c31;
    --ink:#f2f2f5; --soft:rgba(236,236,243,.74); --faint:rgba(236,236,243,.45);
    --line:rgba(255,255,255,.1); --line-soft:rgba(255,255,255,.06);
    --ok:#4cc984; --mid:#e5a83f; --bad:#ff7070; --na:#8b93a3;
    --ok-soft:rgba(76,201,132,.15); --mid-soft:rgba(229,168,63,.15); --bad-soft:rgba(255,112,112,.15);
    --accent:#4da3ff; --accent-soft:rgba(77,163,255,.15);
    --violet:#9686ff; --violet-soft:rgba(150,134,255,.16);
    --hair:0 0 0 .5px rgba(255,255,255,.12);
    --sh-1:var(--hair), 0 1px 2px rgba(0,0,0,.3);
    --sh-2:var(--hair), 0 2px 4px rgba(0,0,0,.3), 0 14px 34px -12px rgba(0,0,0,.6);
  }}
}}

* {{ box-sizing:border-box; }}
html {{ scroll-behavior:smooth; }}
/* 14px 是这份文档的正文档位：比 Studio 的 13px 界面字大一档（报告是读的，
   不是操作的），又比通用 Web 的 15~16px 紧一档，免得整页显得松散。 */
body {{ margin:0; background:var(--bg); color:var(--ink);
  font-family:"SF Pro Text","SF Pro",-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;
  font-size:14px; line-height:1.6; letter-spacing:-.005em;
  -webkit-font-smoothing:antialiased; -moz-osx-font-smoothing:grayscale; }}
.wrap {{ max-width:1120px; margin:0 auto; padding:36px 26px 88px; }}
.mono {{ font-family:var(--mono); font-size:.85em; letter-spacing:0; }}
.muted {{ color:var(--soft); }}
.center {{ text-align:center; }}
a {{ color:var(--accent); text-underline-offset:2px; }}
code {{ font-family:var(--mono); font-size:.86em; letter-spacing:0;
  background:var(--fill); border-radius:4px; padding:1px 5px; }}
::selection {{ background:var(--accent-soft); }}
.vglyph {{ flex:none; vertical-align:-2px; }}

/* 一档一色 —— 但每处都同时带着形状与文字，颜色掉了也读得出来。
   颜色只挂在自带 data-tier / data-verdict 的元素上，不让它顺着继承污染正文。 */
[data-tier="bad"], [data-verdict="violated"] {{ color:var(--bad); }}
[data-tier="flaw"] {{ color:var(--mid); }}
[data-tier="ok"], [data-verdict="pass"] {{ color:var(--ok); }}
[data-tier="unverified"], [data-verdict="not_observable"] {{ color:var(--soft); }}
[data-tier="na"], [data-verdict="not_applicable"] {{ color:var(--na); }}

/* 报头。不用渐变、不用色块 —— 它靠字号（h1 比任何 h2 大一倍）和
   右侧的判定戳来自我区分，这是 macOS 文档头的做法：结构够清楚时不加装饰。 */
.masthead {{ display:flex; justify-content:space-between; align-items:flex-start; gap:28px;
  padding:26px 28px; border-radius:var(--r-xl); box-shadow:var(--sh-1); background:var(--panel); }}
.eyebrow {{ font-size:.68rem; letter-spacing:.09em; text-transform:uppercase;
  color:var(--faint); margin:0 0 9px; font-weight:600; }}
.masthead h1 {{ margin:0 0 7px; font-size:1.62rem; line-height:1.24; font-weight:600; letter-spacing:-.024em; }}
.masthead .sub {{ margin:0; color:var(--soft); font-size:.92rem; }}
.badges {{ margin-top:15px; display:flex; flex-wrap:wrap; gap:6px; }}
/* 胶囊标签：macOS 的做法是浅底同色字，不描边 */
.badge {{ font-size:.72rem; padding:2px 9px; border-radius:var(--r-ctl); background:var(--fill);
  color:var(--soft); font-weight:500; white-space:nowrap; }}
.badge.live {{ background:var(--ok-soft); color:var(--ok); }}
.badge.src {{ background:var(--violet-soft); color:var(--violet); }}

.stamp {{ text-align:center; min-width:168px; flex:none; }}
.stamp .tag {{ display:inline-block; font-weight:600; font-size:.92rem; padding:7px 15px;
  border-radius:var(--r-ctl); letter-spacing:.01em; }}
.stamp.blocked .tag {{ background:var(--bad-soft); color:var(--bad); box-shadow:0 0 0 1px currentColor inset; }}
.stamp.pass .tag {{ background:var(--ok-soft); color:var(--ok); box-shadow:0 0 0 1px currentColor inset; }}
/* 总分用 SF 的数字：等宽数位 + 收紧字距，才像仪表盘上的读数 */
.stamp .total {{ display:block; margin-top:13px; font-size:2.15rem; font-weight:600;
  line-height:1.05; font-variant-numeric:tabular-nums; letter-spacing:-.035em; }}
.stamp .total small {{ font-size:.76rem; font-weight:400; color:var(--soft); letter-spacing:0; }}

/* 八维结论：整份报告的第一结论区。左侧色条留着 —— 它是全页唯一一处
   "这句话最重要"的标记，收到 2px 与 Studio 的导轨同宽。 */
.summary-line {{ margin:16px 0 22px; padding:16px 20px; background:var(--panel);
  border-left:2px solid var(--accent); border-radius:var(--r-md);
  box-shadow:var(--sh-1); font-size:.97rem; }}
.tally {{ list-style:none; display:flex; flex-wrap:wrap; gap:6px; margin:12px 0 0; padding:0; }}
.tally-item {{ display:inline-flex; align-items:center; gap:5px; padding:3px 11px; border-radius:99px;
  background:var(--fill); font-size:.79rem; }}
.tally-item[data-tier="bad"] {{ background:var(--bad-soft); }}
.tally-item[data-tier="flaw"] {{ background:var(--mid-soft); }}
.tally-item[data-tier="ok"] {{ background:var(--ok-soft); }}
/* 「未完全核实」沿用全站的虚线记号：它不是一档结论，是一块缺口。 */
.tally-item[data-tier="unverified"] {{ background:transparent; border:1px dashed var(--line); }}
.tally-item b {{ font:600 .9rem/1 var(--mono); font-variant-numeric:tabular-nums; }}
.tally-item span {{ color:var(--soft); }}

.grid {{ display:grid; gap:16px; }}
.cols-2 {{ grid-template-columns:1fr 1fr; }}
/* 子项数量不定时用这个：只有一张图就铺满，两张才对分。
   固定 cols-2 会在缺图的场合留下一整列空白（无输入参考图的 upload case
   就是这样），而缺图与否只有运行时才知道。 */
.grid-fit {{ grid-template-columns:repeat(auto-fit,minmax(min(340px,100%),1fr)); }}
@media (max-width:820px) {{ .cols-2 {{ grid-template-columns:1fr; }} .masthead {{ flex-direction:column; }} }}

.panel {{ background:var(--panel); border-radius:var(--r-lg);
  padding:22px 24px; margin-bottom:16px; box-shadow:var(--sh-1); }}
/* 十几个同构面板顺着往下排，标题必须有个固定的视觉锚点，不然滚起来分不清
   "上一节结束了没有"。这里用一条贯穿整卡的细分隔线做锚点，而不是标题前的
   彩色竖条 —— 全页只允许 .summary-line 一处用色条，多了就不叫强调了。 */
.panel > h2 {{ display:flex; align-items:center; gap:9px; margin:0 0 14px; padding-bottom:12px;
  border-bottom:1px solid var(--line-soft);
  font-size:1.01rem; font-weight:600; letter-spacing:-.016em; }}
/* hint 紧跟标题时，分隔线要让给 hint：让它跟标题成一组，而不是被切开 */
.panel > h2:has(+ .hint) {{ margin-bottom:7px; padding-bottom:0; border-bottom:0; }}
.panel .hint {{ margin:0 0 15px; padding-bottom:13px; border-bottom:1px solid var(--line-soft);
  color:var(--soft); font-size:.84rem; max-width:88ch; }}

/* 关键读数排成一行。macOS 里这类"分组盒"是内凹的浅底格子，不描边。 */
.signals {{ display:grid; grid-template-columns:repeat(4,1fr); gap:1px;
  background:var(--line-soft); border-radius:var(--r-md); overflow:hidden; }}
.signal {{ background:var(--panel-alt); padding:13px 12px; text-align:center; }}
.signal span {{ display:block; font-size:.66rem; color:var(--faint); text-transform:uppercase; letter-spacing:.06em; }}
.signal strong {{ font-size:1.3rem; font-weight:600; font-variant-numeric:tabular-nums; letter-spacing:-.025em; }}

.frames {{ display:grid; grid-template-columns:repeat(auto-fill,minmax(124px,1fr)); gap:10px; }}
.frame {{ margin:0; border-radius:var(--r-md); overflow:hidden; background:#000; box-shadow:var(--sh-1);
  transition:box-shadow .22s cubic-bezier(.32,.72,0,1), transform .22s cubic-bezier(.32,.72,0,1); }}
.frame:hover {{ transform:translateY(-2px); box-shadow:var(--sh-2); }}
/* 高密度采样帧：描边而不是变形，它是标注不是强调 */
.frame.is-dense {{ box-shadow:0 0 0 1.5px var(--mid); }}
.frame img {{ display:block; width:100%; height:auto; }}
.frame figcaption {{ font-size:.69rem; font-family:var(--mono); letter-spacing:0; padding:6px 8px;
  background:var(--panel); color:var(--soft); display:flex; justify-content:space-between; align-items:center; gap:6px; }}
.quality-frames {{ display:grid; grid-template-columns:repeat(auto-fill,minmax(176px,1fr)); gap:10px;
  align-items:stretch; }}
/* 图注长短不一，而网格行高是齐平的。卡不撑满时，多出来的高度会露出卡片
   自己的黑底（那是给图片留的），看着像每张图下面挂了一条黑边。
   所以卡做成竖向 flex、图注 flex:1 —— 空高归图注，黑底永远只在图后面。 */
.quality-frame {{ margin:0; border-radius:var(--r-md); overflow:hidden; background:#000;
  box-shadow:var(--sh-1); display:flex; flex-direction:column; height:100%; }}
.quality-frame img {{ display:block; width:100%; height:auto; }}
.quality-frame figcaption {{ flex:1; padding:8px 10px; background:var(--panel); font-size:.72rem; line-height:1.45; }}
.quality-frame figcaption strong, .quality-frame figcaption span {{ display:block; }}
.quality-frame figcaption span {{ margin-top:3px; color:var(--soft); }}
.fr-dense {{ background:var(--mid); color:#fff; border-radius:3px; padding:0 5px; font-size:.63rem; font-weight:600; }}
.contact img, .inputimg img {{ width:100%; border-radius:var(--r-md); box-shadow:var(--sh-1); display:block; }}

table {{ width:100%; border-collapse:collapse; font-size:.85rem; }}
th, td {{ text-align:left; padding:9px 11px; border-bottom:1px solid var(--line-soft); vertical-align:top; }}
/* 表头走 macOS 列表的做法：小字、灰、不大写变形太狠，靠底线定界 */
th {{ font-size:.7rem; letter-spacing:.02em; color:var(--faint); font-weight:600;
  border-bottom:1px solid var(--line); white-space:nowrap; }}
tbody tr {{ transition:background .12s ease; }}
tbody tr:hover td {{ background:var(--panel-alt); }}
tbody tr:last-child td {{ border-bottom:0; }}
.table-wrap {{ overflow-x:auto; }}

.ev-grid {{ display:grid; grid-template-columns:repeat(auto-fill,minmax(320px,1fr)); gap:14px; }}
.ev-card {{ margin:0; border-radius:var(--r-lg); overflow:hidden;
  background:var(--panel-alt); box-shadow:var(--sh-1); }}
/* 帧条是"胶片"，深底是刻意的：它把截图和周围的文字彻底分开。 */
.ev-frame-strip {{ display:grid; grid-template-columns:repeat(auto-fit,minmax(150px,1fr)); background:var(--film-line); gap:1px; }}
.ev-frame {{ margin:0; background:#000; }}
.ev-frame img {{ display:block; width:100%; height:auto; }}
.ev-frame figcaption {{ padding:8px 10px; color:var(--film-ink); background:var(--film); font-size:.76rem; line-height:1.4; }}
.ev-noimg {{ padding:40px 10px; text-align:center; font-size:.82rem; }}
.ev-card > div:last-child {{ padding:13px 15px; }}
.ev-head {{ display:flex; flex-wrap:wrap; gap:7px; align-items:center; margin-bottom:7px; }}
.ev-meta {{ margin:0 0 6px; font-size:.76rem; color:var(--soft); }}
.ev-note {{ margin:0; font-size:.87rem; }}
.ev-box {{ background:var(--bad-soft); color:var(--bad); border-radius:4px; padding:1px 7px; font-size:.71rem; font-weight:600; }}
.pill {{ font-size:.71rem; padding:2px 9px; border-radius:var(--r-ctl); background:var(--fill); color:var(--soft); }}
.vbadge {{ font-size:.72rem; font-weight:600; padding:2px 9px; border-radius:var(--r-ctl); }}
.vbadge.v-bad {{ background:var(--bad-soft); color:var(--bad); }}
.vbadge.v-ok {{ background:var(--ok-soft); color:var(--ok); }}

/* V2V 专项。KPI 卡横排、3×3 网格用表格画 —— 改动集中的格子标红，
   让「改在哪」一眼可见，不必读数字。 */
.v2v-kpis {{ display:grid; grid-template-columns:repeat(auto-fit,minmax(150px,1fr)); gap:1px; margin:12px 0;
  background:var(--line-soft); border-radius:var(--r-md); overflow:hidden; }}
.v2v-kpis > div {{ display:flex; flex-direction:column; gap:2px; padding:11px 13px; background:var(--panel-alt); }}
.v2v-kpis b {{ font-size:1.12rem; font-family:var(--mono); font-variant-numeric:tabular-nums; letter-spacing:-.02em; }}
.v2v-kpis span.muted {{ font-size:.68rem; }}
.v2v-grid {{ width:auto; margin:10px 0 0; border-collapse:separate; border-spacing:3px; }}
.v2v-grid td {{ width:74px; padding:8px 4px; background:var(--fill); border:0; border-radius:var(--r-sm);
  font-size:.72rem; font-variant-numeric:tabular-nums; }}
.v2v-grid td.v2v-cell-hit {{ background:var(--bad-soft); color:var(--bad); font-weight:600; }}
.v2v-grid tbody tr:hover td {{ background:var(--fill); }}
.v2v-grid tbody tr:hover td.v2v-cell-hit {{ background:var(--bad-soft); }}

/* ---- 八维卡 ---- */
/* 八维卡固定两列。原来的 auto-fit minmax(430px) 会随窗口宽度在 1/2/3 列之间跳，
   而八张卡只有 2×4 才既不出现孤行、又让每张卡宽到放得下判据表。 */
.native-grid {{ display:grid; grid-template-columns:repeat(2, minmax(0,1fr)); gap:12px; }}
@media (max-width:940px) {{ .native-grid {{ grid-template-columns:minmax(0,1fr); }} }}
.native-card {{ position:relative; border-radius:var(--r-lg);
  padding:18px 18px 15px; background:var(--panel-alt); box-shadow:var(--sh-1); overflow:hidden; }}
.native-card[data-state="na"] {{ opacity:.72; }}
.native-card > header {{ display:flex; gap:10px; align-items:center; flex-wrap:wrap; }}
.native-card > header h2 {{ margin:0; font-size:.98rem; font-weight:600; letter-spacing:-.016em; }}
.native-card .score {{ margin-left:auto; font:600 1.55rem/1 var(--mono);
  font-variant-numeric:tabular-nums; letter-spacing:-.035em; }}
/* 顶部导轨是档位的第一眼信号；还有子判据悬着就改斜纹。
   2px 与 Studio 的维度卡导轨同宽 —— 它是记号，不是色块。
   斜纹在整页里只表示「不完整」，别的地方不要引入。 */
.nc-rail {{ position:absolute; left:0; right:0; top:0; height:2px; background:currentColor; }}
.native-card[data-unresolved="1"] .nc-rail {{
  background:repeating-linear-gradient(115deg, currentColor 0 3px, transparent 3px 7px); }}
.nc-state {{ display:inline-flex; align-items:center; gap:5px; font-size:.75rem; font-weight:600;
  padding:2px 9px; border-radius:var(--r-ctl);
  background:var(--panel); box-shadow:0 0 0 1px currentColor inset; }}
/* 淡底实字比"白底 + 描边"更贴近 macOS 的状态标签。用 color-mix 从 currentColor
   现算淡底，八种档位色就不必各写一条规则；不支持时退回上面的描边版。 */
@supports (background:color-mix(in srgb, red, white)) {{
  .nc-state {{ background:color-mix(in srgb, currentColor 12%, var(--panel)); box-shadow:none; }}
}}
.nc-meta {{ margin:10px 0; font-size:.77rem; color:var(--soft); }}
.nc-meta b {{ color:var(--ink); }}
.nc-unresolved {{ font-family:var(--mono); font-size:.9em; letter-spacing:0; }}
/* 行上的 data-verdict 只为整行底纹服务；正文一律回到墨色，
   否则判定色会顺着继承糊满判据名和 issue 号，颜色就不再是第三信道了。 */
.crit-table tbody td {{ color:var(--ink); }}
.crit-table tbody tr:hover td {{ background:transparent; }}
.crit-verdict {{ display:inline-flex; align-items:center; gap:4px; font-weight:600; }}
/* 未核实的整行套一层斜纹底：扫视时它自成一块，绝不会和 pass 行混起来。 */
.crit-table tr[data-verdict="not_observable"] td {{
  background:repeating-linear-gradient(115deg, var(--fill) 0 5px, transparent 5px 11px); }}
.crit-table tr[data-verdict="not_applicable"] td {{ opacity:.55; }}
.native-issue summary {{ cursor:pointer; font-weight:600; border-radius:var(--r-sm); }}
.native-conclusion {{ margin:10px 0; padding:13px 15px; border-radius:var(--r-md);
  background:var(--panel); box-shadow:var(--sh-1); }}
.native-conclusion p {{ margin:7px 0 0; line-height:1.58; }}
.native-time {{ font:600 .88rem/1.4 var(--mono); letter-spacing:0; color:var(--ink); }}
.review-head {{ margin:0 0 8px; }}

.obs {{ display:flex; gap:16px; padding:11px 0; border-bottom:1px solid var(--line-soft); list-style:none; }}
.obs:last-child {{ border-bottom:0; }}
ul.obslist {{ margin:0; padding:0; }}
.obs-time {{ flex:0 0 118px; color:var(--accent); font-variant-numeric:tabular-nums; }}
.obs-body p {{ margin:0 0 5px; }}
.obs-meta {{ display:flex; flex-wrap:wrap; gap:6px; align-items:center; font-size:.73rem; }}
.tag {{ background:var(--accent-soft); color:var(--accent); border-radius:4px; padding:1px 7px; }}
.conf {{ border-radius:4px; padding:1px 7px; }}
.conf-high {{ background:var(--ok-soft); color:var(--ok); }}
.conf-medium {{ background:var(--mid-soft); color:var(--mid); }}
.conf-low {{ background:var(--bad-soft); color:var(--bad); }}

.trace-step {{ border-radius:var(--r-md); margin-bottom:7px;
  background:var(--panel-alt); box-shadow:var(--sh-1); overflow:hidden; }}
.trace-step > summary {{ cursor:pointer; padding:10px 14px; display:flex; gap:10px; align-items:center;
  flex-wrap:wrap; transition:background .12s ease; }}
.trace-step > summary:hover {{ background:var(--fill); }}
.trace-step > summary::-webkit-details-marker {{ color:var(--faint); }}
.trace-title {{ font-weight:600; }}
.trace-body {{ padding:0 15px 13px; }}
.trace-body details {{ margin:8px 0; }}
.trace-body summary {{ cursor:pointer; font-size:.81rem; color:var(--accent); border-radius:var(--r-sm); }}
/* 代码块固定深底：它在深浅两个主题下都是代码，不跟随 token。 */
pre {{ background:var(--film); color:var(--film-ink); padding:13px 15px; border-radius:var(--r-md);
  overflow:auto; font-family:var(--mono); font-size:.77rem; line-height:1.55; letter-spacing:0; max-height:460px; }}
.trace-err {{ color:var(--bad); font-weight:600; }}
.kv {{ display:grid; grid-template-columns:auto 1fr; gap:5px 16px; font-size:.87rem; }}
.kv dt {{ color:var(--soft); }} .kv dd {{ margin:0; }}
footer {{ margin-top:30px; padding-top:18px; border-top:1px solid var(--line);
  color:var(--faint); font-size:.77rem; line-height:1.7; text-align:center; }}

/* 打印：面板不许被跨页切断，深底代码块换成浅底省墨。
   卡片的边此刻要变回真 border —— 打印机不打背景与投影，只靠 box-shadow
   画边的卡片印出来会完全没有轮廓。 */
@media print {{
  body {{ background:#fff; font-size:11pt; }}
  .wrap {{ max-width:none; padding:0; }}
  .panel, .masthead, .summary-line, .native-card, .ev-card, .trace-step,
  .frame, .quality-frame, .native-conclusion {{
    box-shadow:none; border:1px solid #d4d5dc; }}
  .panel, .masthead, .summary-line {{ break-inside:avoid; }}
  .native-card, .ev-card, .trace-step {{ break-inside:avoid; }}
  .signals, .v2v-kpis {{ border:1px solid #d4d5dc; }}
  pre {{ background:#f4f5f7; color:#14161d; max-height:none; }}
  details {{ display:block; }}
}}

/* 尊重"减少动效"：整页只有 .frame 的悬停位移一处动画，直接停掉。 */
@media (prefers-reduced-motion: reduce) {{
  html {{ scroll-behavior:auto; }}
  * {{ transition-duration:0.01ms !important; }}
  .frame:hover {{ transform:none; }}
}}
</style>
</head>
<body>
<div class="wrap">

  <header class="masthead">
    <div>
      <p class="eyebrow">Generative Video QA · Evidence Report</p>
      <h1>{_esc(case.get('label'))}</h1>
      <p class="sub">{_esc(case.get('focus'))}</p>
      <div class="badges">
        <span class="badge">{_esc(case.get('mode'))}</span>
        <span class="badge src">视频源 {_esc(report.get('source_label') or report.get('source') or 'Kling')}</span>
        <span class="badge {'live' if engine in LIVE_PROVIDERS else ''}">引擎 {_esc(engine)}</span>
        {f'<span class="badge">{_esc(model_name)}</span>' if model_name else ''}
        <span class="badge">{_esc(meta.get('width'))}×{_esc(meta.get('height'))} · {_esc(meta.get('fps'))}fps · {_esc(meta.get('duration_seconds'))}s</span>
        <span class="badge">run {_esc(run_id)}</span>
      </div>
    </div>
    <div class="stamp {status_cls}">
      <span class="tag">{_esc(status_text)}</span>
      <span class="total">{len(report.get('issues') or [])}<small> 个原子问题</small></span>
      <span class="muted mono">{_esc(report.get('schema_version'))}</span>
    </div>
  </header>

  <div class="summary-line">
    <p style="margin:0"><strong>八维结论：</strong>本协议不产出总分，也没有整体质量标签 ——
      八个维度各自独立成立，压成一个数会让一条编造出来的结论被平均掉。
      <span class="muted">analysis_status 只表示流程是否成功，不是质量评价。</span></p>
    <ul class="tally">{_render_tally(rubric)}</ul>
    <p class="muted" style="margin:8px 0 0;font-size:.82rem">「未完全核实」是页面单列的一档：该维没有确证缺陷，
      但仍有子判据是 <code>not_observable</code>。后端算出的 <code>tier</code> 会是「无问题」，
      而没核实过不等于没问题，所以这里不按绿色显示。</p>
  </div>

  <section class="panel">
    <h2>QA 标注 GT</h2>
    <p class="hint">来自测试集 XLSX，仅用于与模型评测结果对照，不参与本次模型推理。</p>
    <dl class="kv">{qa_gt_rows}</dl>
  </section>

  <section class="panel">
    <h2>原生八维结论（v1.1 · 三档）</h2>
    <p class="hint">八维及子判据是唯一质量事实源：<strong>0 恶劣错误 / 0.5 瑕疵 / 1 无问题</strong>。
      同一实际根因只建立一个 issue；跨维归因通过多个子判据引用同一 issue 保留。
      每张卡同时给出程序档位与 <code>decision_status</code>，未核实的子判据逐条列名。</p>
    {_render_native8_dimensions(report)}
  </section>

  <section class="panel">
    <h2>程序未采信 / 待人工复核</h2>
    <p class="hint">证据强度或定档事实不足的 violated 会被程序驳回，不计入任何维度的档位，
      而是留在这里等复核 —— 它们既不是「已确认的问题」，也不是「已排除的问题」。</p>
    {_render_review_funnel(report)}
  </section>

  <section class="panel">
    <h2>4b 主判 / 4c 增量取证结果</h2>
    <p class="hint">展示统一扫描及按疑点触发的指令、一致性、画面、动态、文字、故事、音频和 V2V 专项事实。完整模型输入输出见报告末尾。</p>
    {_render_native8_specialists(report)}
  </section>

  <section class="panel">
    <h2>画面质量 / 动态质量判定输入溯源</h2>
    <p class="hint">这里只展示实际送入相关裁决阶段的帧和完整视频通道，不把普通报告截图冒充模型输入。</p>
    {_render_quality_input_audit(report, llm)}
  </section>

  {_render_v2v(report)}

  <section class="panel">
    <h2>Agent 所见的证据</h2>
    <p class="hint">运动感知采样：{_esc(sampling.get('strategy'))} · {sampling.get('dense_count', 0)} 帧加密于高运动窗口 {_esc(win_text)}。黄框为高运动加密帧。</p>
    <div class="grid grid-fit" style="margin-bottom:16px">
      {f'<figure class="inputimg" style="margin:0"><img src="{input_uri}" alt="输入参考图" /><figcaption class="muted mono">输入参考图（I2V 首帧参考）</figcaption></figure>' if input_uri else ''}
      {f'<figure class="contact" style="margin:0"><img src="{contact_uri}" alt="6fps 联系表" /><figcaption class="muted mono">全片 6fps 联系表</figcaption></figure>' if contact_uri else ''}
    </div>
    <div class="frames">{_render_frames(frame_meta or [])}</div>
  </section>

  <section class="panel">
    <h2>Prompt → 可验证约束</h2>
    <p class="hint">原始 prompt：{_esc((report.get('prompt') or {}).get('zh') or (report.get('prompt') or {}).get('en'))}</p>
    <table>
      <thead><tr><th>ID</th><th>类别</th><th>约束陈述</th><th>源文本</th><th>维度</th><th>软硬</th></tr></thead>
      <tbody>{_render_constraints(report.get('prompt_requirements') or [])}</tbody>
    </table>
  </section>

  <section class="panel">
    <h2>VLM 观察（只陈述事实，不下结论）</h2>
    <ul class="obslist">{_render_observations(report.get('observations', []))}</ul>
  </section>

  <section class="panel">
    <h2>问题定位证据（帧截图 + 红框）</h2>
    <p class="hint">按最终 ISSUE-ID 的发生时刻抽取高质量静态帧；独立画面定位调用能够确定空间位置时，
      才在对应局部画<strong style="color:var(--bad)">红框</strong>。该定位步骤只补充坐标，不重新裁决问题。</p>
    {_render_evidence(evidence_cards)}
  </section>

  <section class="panel">
    <h2>独立复核调用记录（实际发生）</h2>
    <p class="hint">仅展示调用 trace 中真实执行的 4c 候选原子问题增量取证与音频独立复听；
      4b 主判输入已在画面/动态输入溯源区展示。</p>
    {_render_actual_rechecks(report, llm)}
  </section>

  <section class="panel">
    <h2>本次分析成本</h2>
    <p class="hint">分析这一个视频实际发生的大模型调用次数、输入模态与 token 消耗。</p>
    {_render_cost(report.get('cost', {}))}
  </section>

  <section class="panel">
    <h2>每一步的完整输入 / 输出</h2>
    <p class="hint">每一步 Prompt 编译器 / 观察器 / 4b 主判 / 4c 增量取证的 system instruction、输入与模型输出 JSON，均可展开核对。</p>
    {_render_trace(llm.get('steps', []))}
  </section>

  <footer>
    Video Judge · 受约束推理评测（{footer_flow}）<br>
    run_id {_esc(run_id)} · 引擎 {_esc(engine)} {f'· 模型 {_esc(model_name)}' if model_name else ''}
  </footer>

</div>
</body>
</html>"""


def write_report_html(
    report: dict[str, Any],
    *,
    run_id: str,
    frame_meta: list[dict[str, Any]] | None = None,
    contact_path: Path | None = None,
    input_image_path: Path | None = None,
) -> tuple[Path, str | None]:
    """Render and persist the report; return (local_path, view_url or None).

    文件名**必须**是 run_id，不接受自定义：``/api/report?run_id=X`` 直接去找
    ``reports/X.html``，改名就等于让 studio 里每一次运行的「打开报告」都 404。
    而且 run_id 唯一、case 名不唯一——按 case 名存会让同一案例的重跑互相覆盖，
    历史里的旧报告全部指向最新那一份。
    """
    html_text = render_report_html(
        report,
        run_id=run_id,
        frame_meta=frame_meta,
        contact_path=contact_path,
        input_image_path=input_image_path,
    )
    REPORT_ROOT.mkdir(parents=True, exist_ok=True)
    # 只取 basename：run_id 理论上不含分隔符，但报告落盘在 reports/ 之外的后果
    # 太重，这里不省这一步。
    report_name = Path(run_id).name
    if not report_name.endswith(".html"):
        report_name += ".html"
    local_path = REPORT_ROOT / report_name
    local_path.write_text(html_text, encoding="utf-8")

    view_url: str | None = None
    if VIEW_TREE is not None and VIEW_HOST:
        try:
            VIEW_TREE.mkdir(parents=True, exist_ok=True)
            (VIEW_TREE / report_name).write_text(html_text, encoding="utf-8")
            view_url = f"{VIEW_HOST}/{report_name}"
        except Exception:  # noqa: BLE001 - view mirror is optional
            view_url = None
    return local_path, view_url
