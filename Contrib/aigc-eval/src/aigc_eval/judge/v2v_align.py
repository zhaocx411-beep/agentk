"""V2V 源视频 vs 生成视频的**确定性**对齐剖面。

存在的理由与 `integrity.py` 一致：框架的第二条底线是「确定性拦截、不信任模型自评分」。
V2V 有两个互补的失败模式 —— 该改的没改、不该改的被改了 —— 而这两者都建立在一个
先决问题上：**到底哪里被改了**。这个问题不能问模型：

  · 模型答「整体保持一致」时，你无法区分「它核对过」与「它没看」；
  · 编辑幅度是连续量，模型对「明显 vs 轻微」的自评分方差极大（AGENT_DESIGN §9.1
    在崩坏维度上已经吃过这个亏）；
  · V2V 最常见的 badcase 是「漏改了其中一处」，这要求逐时刻定位，而模型给不出可核查的坐标。

所以这里用 ffmpeg 算逐时刻的 SSIM/PSNR 剖面，把「改了多少、改在哪」变成可审计的数字，
再喂给模型当**先验**（不是结论）。模型的职责因此收窄成它擅长的部分：解释这些差异是
prompt 要的编辑，还是不该发生的漂移。

零第三方依赖（stdlib + 系统 ffmpeg），与仓库其余部分一致。全部函数 best-effort：
探测失败返回 None/空，调用方回退到纯模型判断，绝不打断整条 case。
"""

from __future__ import annotations

import re
import subprocess
from pathlib import Path
from typing import Any


# 对齐剖面的采样率。2 fps 足以定位到「哪一段被改了」（编辑操作的时间尺度是秒级），
# 再高只是成倍拉长 ffmpeg 时间。
ALIGN_SAMPLE_FPS = 2.0
# 比对分辨率。SSIM 对分辨率不敏感，但降到 320 宽能让 5s 视频的比对稳定在 1~2s；
# 保持 16:9 之外的画幅时按长边缩放会引入形变，所以统一 scale 到同一尺寸再比。
ALIGN_WIDTH = 320
ALIGN_HEIGHT = 180

# ---------------------------------------------------------------------------
# 编辑判定阈值
#
# 这几个数是在 V2VA_gold50_AI 的 5 条样本上量出来的，含义写在这里免得后人乱调：
#   · 同一段未被编辑的画面（仅重编码/超分差异）SSIM 稳定在 0.96 以上；
#   · 被局部编辑（去掉一朵花、改一处文字）的时刻落在 0.70~0.90；
#   · 被整体重绘（换主体、换风格）的时刻落在 0.70 以下。
# 于是取 0.94 作「这一时刻是否被改动」的门槛，0.75 作「改动是否是大范围重绘」的门槛。
# 注意：**这些数字只用于描述与定位，不直接判违反** —— 一处 SSIM 极低完全可能正是
# prompt 要求的重绘。判违反仍由模型结合 prompt 语义做，程序只保证它拿到的定位是真的。
# ---------------------------------------------------------------------------
EDIT_SSIM_THRESHOLD = 0.94
HEAVY_EDIT_SSIM_THRESHOLD = 0.75
# 「几乎逐帧全等」的门槛：超过它意味着生成视频基本是源视频的重编码副本，
# 即编辑操作根本没执行。这是 V2V 唯一一个可以由程序直接给出强信号的失败模式。
UNCHANGED_SSIM_THRESHOLD = 0.985
# 被改动时刻的占比超过这个比例，视为「全片范围的改动」而非局部编辑。
GLOBAL_EDIT_RATIO = 0.6

# ---------------------------------------------------------------------------
# 空间网格
#
# 全片单值 SSIM 会把**小面积编辑平均掉**：实测 case_035（只改了背景一处文字）
# 全局均值 0.952，落在 EDIT_SSIM_THRESHOLD 之上 → 剖面报「未检出改动」，
# 而按 3×3 网格分块后，文字所在的左上格是 0.889，信号清晰可见。
# 漏掉这类定位会让模型拿到错误先验（「程序说没改动」），比不给先验更糟。
#
# 取 3×3 是因为 prompt 里的空间词就是这个粒度（「右上角的花」「背景中间的字」），
# 网格名直接对得上，模型不需要换算坐标。更细的网格会让每格样本太小、SSIM 抖动放大。
# ---------------------------------------------------------------------------
GRID_N = 3
GRID_WIDTH = 480
GRID_HEIGHT = 270
GRID_CELL_ZH = (
    "左上", "上中", "右上",
    "左中", "中央", "右中",
    "左下", "下中", "右下",
)


def _run(command: list[str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(command, capture_output=True, text=True, check=False)


def _ssim_series(source_path: Path, output_path: Path) -> list[dict[str, float]]:
    """逐采样点的 SSIM。返回 ``[{"index": n, "ssim": 0.97}, ...]``。

    两路都先 `fps` 再 `setpts=PTS-STARTPTS`：源视频与生成视频的起始 PTS 常有偏移
    （不同厂商的封装习惯），不归零会让 ssim 滤镜按错位的帧配对，把「时间轴没对齐」
    误报成「画面被改了」。scale 到同一固定尺寸是因为 ssim 要求两路尺寸严格相同，
    而 V2V 的生成视频常带超分（实测 case_029 是源视频的 2 倍边长）。
    """
    completed = _run(
        [
            "ffmpeg",
            "-hide_banner",
            "-v",
            "error",
            "-i",
            str(source_path),
            "-i",
            str(output_path),
            "-lavfi",
            (
                f"[0:v]fps={ALIGN_SAMPLE_FPS},scale={ALIGN_WIDTH}:{ALIGN_HEIGHT},"
                "setpts=PTS-STARTPTS[a];"
                f"[1:v]fps={ALIGN_SAMPLE_FPS},scale={ALIGN_WIDTH}:{ALIGN_HEIGHT},"
                "setpts=PTS-STARTPTS[b];"
                "[a][b]ssim=stats_file=-"
            ),
            "-f",
            "null",
            "-",
        ]
    )
    series: list[dict[str, float]] = []
    for line in (completed.stdout or "").splitlines():
        # 形如：n:12 Y:0.98 U:0.99 V:0.99 All:0.986 (18.7)
        match = re.search(r"\bn:(\d+)\b.*?\bAll:([0-9.]+)", line)
        if not match:
            continue
        series.append(
            {
                "index": int(match.group(1)),
                "time": round((int(match.group(1)) - 1) / ALIGN_SAMPLE_FPS, 3),
                "ssim": round(float(match.group(2)), 4),
            }
        )
    return series


def _segments(
    series: list[dict[str, float]], *, threshold: float
) -> list[dict[str, Any]]:
    """把逐点判定合并成连续区间，供模型按「段」而不是按「点」理解。

    只合并 `ssim < threshold` 的点。相邻两点间隔 1/fps，所以直接按 index 连续性合并。
    """
    segments: list[dict[str, Any]] = []
    current: dict[str, Any] | None = None
    for point in series:
        changed = point["ssim"] < threshold
        if changed and current is None:
            current = {"start": point["time"], "end": point["time"], "min_ssim": point["ssim"]}
        elif changed and current is not None:
            current["end"] = point["time"]
            current["min_ssim"] = min(current["min_ssim"], point["ssim"])
        elif not changed and current is not None:
            segments.append(current)
            current = None
    if current is not None:
        segments.append(current)
    for segment in segments:
        segment["duration"] = round(segment["end"] - segment["start"] + 1 / ALIGN_SAMPLE_FPS, 3)
        segment["min_ssim"] = round(segment["min_ssim"], 4)
        segment["span"] = f"{segment['start']:.1f}-{segment['end']:.1f}s"
    return segments


def _grid_profile(
    source_path: Path, output_path: Path, work_dir: Path
) -> list[dict[str, Any]] | None:
    """按 3×3 网格分区的 SSIM 均值/最低值，用于定位小面积编辑。

    ssim 滤镜只能写文件（`stats_file`），所以九格各写一个日志再读回来。整张滤镜图
    在**同一次** ffmpeg 调用里完成（split → 九对 crop → 九个 ssim → hstack 汇总），
    因此代价与单次全局比对同量级，不是九倍解码。
    """
    cell_w, cell_h = GRID_WIDTH // GRID_N, GRID_HEIGHT // GRID_N
    total = GRID_N * GRID_N
    work_dir.mkdir(parents=True, exist_ok=True)
    logs = [work_dir / f"grid_{i}.log" for i in range(total)]
    for log in logs:
        log.unlink(missing_ok=True)

    labels_a = "".join(f"[a{i}]" for i in range(total))
    labels_b = "".join(f"[b{i}]" for i in range(total))
    chains = [
        f"[0:v]fps={ALIGN_SAMPLE_FPS},scale={GRID_WIDTH}:{GRID_HEIGHT},"
        f"setpts=PTS-STARTPTS,split={total}{labels_a}",
        f"[1:v]fps={ALIGN_SAMPLE_FPS},scale={GRID_WIDTH}:{GRID_HEIGHT},"
        f"setpts=PTS-STARTPTS,split={total}{labels_b}",
    ]
    outs = ""
    for index in range(total):
        row, col = divmod(index, GRID_N)
        x, y = col * cell_w, row * cell_h
        chains.append(f"[a{index}]crop={cell_w}:{cell_h}:{x}:{y}[ac{index}]")
        chains.append(f"[b{index}]crop={cell_w}:{cell_h}:{x}:{y}[bc{index}]")
        # stats_file 路径里的 ':' 与 '\' 会被 ffmpeg 滤镜语法吃掉，必须转义。
        escaped = str(logs[index]).replace("\\", "\\\\").replace(":", "\\:")
        chains.append(f"[ac{index}][bc{index}]ssim=stats_file={escaped}[o{index}]")
        outs += f"[o{index}]"
    chains.append(f"{outs}hstack={total}")

    completed = _run(
        [
            "ffmpeg", "-hide_banner", "-v", "error",
            "-i", str(source_path), "-i", str(output_path),
            "-lavfi", ";".join(chains), "-f", "null", "-",
        ]
    )
    if completed.returncode != 0:
        return None

    cells: list[dict[str, Any]] = []
    for index, log in enumerate(logs):
        values: list[float] = []
        try:
            for line in log.read_text(encoding="utf-8", errors="replace").splitlines():
                match = re.search(r"\bAll:([0-9.]+)", line)
                if match:
                    values.append(float(match.group(1)))
        except OSError:
            continue
        if not values:
            continue
        cells.append(
            {
                "cell": index,
                "name": GRID_CELL_ZH[index] if index < len(GRID_CELL_ZH) else str(index),
                "ssim_mean": round(sum(values) / len(values), 4),
                "ssim_min": round(min(values), 4),
                "changed": (sum(values) / len(values)) < EDIT_SSIM_THRESHOLD,
            }
        )
    return cells or None


def _duration(video_path: Path) -> float:
    completed = _run(
        [
            "ffprobe", "-v", "error",
            "-show_entries", "format=duration",
            "-of", "csv=p=0", str(video_path),
        ]
    )
    try:
        return round(float((completed.stdout or "0").strip()), 3)
    except ValueError:
        return 0.0


def align_profile(
    source_path: Path, output_path: Path, work_dir: Path | None = None
) -> dict[str, Any] | None:
    """源视频 vs 生成视频的对齐剖面。探测失败返回 None。

    返回结构（全部由程序算出，可审计）：
      · `duration_source` / `duration_output` / `duration_delta`：时长是否被改变。
        V2V 一般应等长；差异过大意味着截断、变速或续写。
      · `ssim_mean` / `ssim_min` / `ssim_max`：整体保真水平。
      · `changed_ratio`：被改动时刻的占比 → 局部编辑 vs 全片重绘。
      · `edited_segments`：被改动的时间区间（含最低 SSIM），**这是给模型的定位先验**。
      · `preserved_segments`：与源视频几乎相同的区间，用于核对「保留区是否真的保留」。
      · `grid`：3×3 空间分区的相似度，定位小面积编辑（全局均值会把它们平均掉）。
      · `verdict`：程序侧的粗判，仅描述改动的**范围**，不判对错。
      · `series`：逐点原始数据，落进 trace 供审计（点数 = 时长 × 2，很小）。

    `work_dir` 只给网格通道放 ffmpeg 的 stats 日志；不传则跳过网格（退化成全局剖面）。
    """
    source_path, output_path = Path(source_path), Path(output_path)
    if not source_path.exists() or not output_path.exists():
        return None
    try:
        series = _ssim_series(source_path, output_path)
    except Exception:  # noqa: BLE001 - 对齐剖面是 best-effort，失败即回退纯模型判断
        return None
    if not series:
        return None

    grid: list[dict[str, Any]] | None = None
    if work_dir is not None:
        try:
            grid = _grid_profile(source_path, output_path, Path(work_dir))
        except Exception:  # noqa: BLE001 - 网格是增强信号，失败不影响全局剖面
            grid = None

    values = [point["ssim"] for point in series]
    duration_source = _duration(source_path)
    duration_output = _duration(output_path)
    edited = _segments(series, threshold=EDIT_SSIM_THRESHOLD)
    heavy = _segments(series, threshold=HEAVY_EDIT_SSIM_THRESHOLD)
    changed_ratio = sum(1 for v in values if v < EDIT_SSIM_THRESHOLD) / len(values)
    ssim_mean = sum(values) / len(values)
    changed_cells = [c for c in (grid or []) if c.get("changed")]

    # verdict 只回答「改动覆盖了多大范围」，不回答「改得对不对」——
    # 后者需要 prompt 语义，由模型判。这里越界会让程序开始猜用户意图。
    # 判 unchanged 必须**同时**满足全局与网格两侧都没信号：小面积编辑会被全局均值
    # 平均到 0.985 以上（case_035 实测 0.952 全局 / 0.889 左上格），
    # 只看全局会把「改了一处文字」误报成「什么都没改」，那是最糟的先验。
    if ssim_mean >= UNCHANGED_SSIM_THRESHOLD and not changed_cells:
        verdict = "unchanged"
    elif changed_ratio >= GLOBAL_EDIT_RATIO:
        verdict = "global_edit"
    elif edited or changed_cells:
        verdict = "local_edit"
    else:
        verdict = "minor_diff"

    return {
        "available": True,
        "sample_fps": ALIGN_SAMPLE_FPS,
        "sample_count": len(series),
        "duration_source": duration_source,
        "duration_output": duration_output,
        "duration_delta": round(duration_output - duration_source, 3),
        "ssim_mean": round(ssim_mean, 4),
        "ssim_min": round(min(values), 4),
        "ssim_max": round(max(values), 4),
        "changed_ratio": round(changed_ratio, 3),
        "edited_segments": edited,
        "heavy_edited_segments": heavy,
        "preserved_segments": _preserved(series),
        "grid": grid or [],
        "grid_changed_cells": [c["name"] for c in changed_cells],
        "verdict": verdict,
        "thresholds": {
            "edit": EDIT_SSIM_THRESHOLD,
            "heavy_edit": HEAVY_EDIT_SSIM_THRESHOLD,
            "unchanged": UNCHANGED_SSIM_THRESHOLD,
        },
        "series": series,
    }


def _preserved(series: list[dict[str, float]]) -> list[dict[str, Any]]:
    """与源视频几乎相同的连续区间（SSIM >= 编辑门槛）。

    用 `_segments` 的反相：把判定取反后复用同一套合并逻辑。
    """
    inverted = [
        {"index": p["index"], "time": p["time"], "ssim": 2.0 - p["ssim"]} for p in series
    ]
    merged = _segments(inverted, threshold=2.0 - EDIT_SSIM_THRESHOLD)
    for segment in merged:
        segment["min_ssim"] = round(2.0 - segment["min_ssim"], 4)
    return merged


def align_digest(profile: dict[str, Any] | None) -> str:
    """把剖面压成给模型看的一段中文说明。

    刻意写成「这是测量值、请你解释」而不是「这里有问题」：程序测得出**哪里变了**，
    但「变了是对还是错」取决于 prompt，越权断言会污染模型判断。
    """
    if not isinstance(profile, dict) or not profile.get("available"):
        return ""
    verdict_zh = {
        "unchanged": "生成视频与源视频**几乎逐帧相同**（可能编辑操作根本没执行）",
        "global_edit": "改动覆盖全片大部分时刻（整体重绘/换风格的特征）",
        "local_edit": "改动集中在若干局部时间段（局部编辑的特征）",
        "minor_diff": "全片仅有轻微差异，未检出明确的改动段",
    }.get(str(profile.get("verdict")), "")
    lines = [
        "【源视频 vs 生成视频 客观对齐测量（由程序用 ffmpeg SSIM 算出，非模型判断）】",
        f"- 逐时刻画面相似度：均值 {profile.get('ssim_mean')}、最低 {profile.get('ssim_min')}"
        f"（1.0 = 完全相同）；被改动时刻占比 {profile.get('changed_ratio')}。",
        f"- 程序侧范围判定：{verdict_zh}。",
        f"- 时长：源 {profile.get('duration_source')}s → 生成 {profile.get('duration_output')}s"
        f"（差 {profile.get('duration_delta')}s）。",
    ]
    edited = profile.get("edited_segments") or []
    if edited:
        spans = "、".join(
            f"{s['span']}(最低相似度 {s['min_ssim']})" for s in edited[:8]
        )
        lines.append(f"- **检出的改动时间段**：{spans}。")
    preserved = profile.get("preserved_segments") or []
    if preserved:
        lines.append(
            "- 与源视频基本一致的时间段：" + "、".join(s["span"] for s in preserved[:8]) + "。"
        )
    grid = [c for c in (profile.get("grid") or []) if isinstance(c, dict)]
    if grid:
        changed = [c for c in grid if c.get("changed")]
        if changed:
            detail = "、".join(f"{c['name']}({c['ssim_mean']})" for c in changed)
            lines.append(
                f"- **画面里改动集中的区域**（3×3 分区，值越低改动越大）：{detail}。"
                "把它与 prompt 里的空间说法（如「右上角」「背景」）对照，"
                "可核对改动是否发生在被点名的位置。"
            )
        else:
            weakest = min(grid, key=lambda c: c.get("ssim_mean", 1.0))
            lines.append(
                f"- 3×3 分区未检出明确改动区域，差异最大的是{weakest['name']}"
                f"（{weakest['ssim_mean']}）。"
            )
    lines.append(
        "【怎么用这些数字】这只是「哪里变了」的客观定位，**不代表对错**：改动段可能正是 prompt "
        "要求的编辑，一致段也可能是本该改却没改。请你把它当索引 ——\n"
        "  ① 判编辑指令达成：prompt 要求改的对象，其所在时刻是否落在改动段里？"
        "若全片都无改动段而 prompt 明确要求了编辑，即为未执行；\n"
        "  ② 判源视频保真：改动段里发生的变化，是否**超出**了 prompt 要求的范围"
        "（改到了没被点名的主体、背景或构图）？\n"
        "  ③ 相似度低本身不是问题，相似度高本身也不是优点 —— 一切以 prompt 要求为准。"
    )
    return "\n".join(lines)


def align_issues(
    profile: dict[str, Any] | None, *, has_edit_ops: bool
) -> list[dict[str, Any]]:
    """程序能独立确证的 V2V 异常，语义与 `integrity.check_integrity` 的 issues 一致。

    **刻意只留一条**：prompt 明确要求了编辑操作，而生成视频与源视频几乎逐帧相同。
    这是唯一一个不需要理解 prompt 语义就能确证的失败 —— 无论要改什么，
    「什么都没改」都是错的。其余判定都依赖「改得对不对」，必须交给模型。

    时长差异刻意**不**在这里报：V2V 允许续写与变速，程序无法区分它是缺陷还是意图，
    这类信号只作为 digest 里的描述交给模型。
    """
    if not isinstance(profile, dict) or not profile.get("available"):
        return []
    issues: list[dict[str, Any]] = []
    if has_edit_ops and str(profile.get("verdict")) == "unchanged":
        issues.append(
            {
                "type": "v2v_edit_not_applied",
                "severity": 3,
                "detail": (
                    f"prompt 要求了编辑操作，但生成视频与源视频逐时刻相似度均值 "
                    f"{profile.get('ssim_mean')}（≥{UNCHANGED_SSIM_THRESHOLD} 视为未改动），"
                    "未检出任何改动段——编辑操作疑未执行。"
                ),
            }
        )
    return issues
