"""Deterministic video integrity pre-checks with ffprobe/ffmpeg."""

from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path
from typing import Any


def _run(command: list[str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(command, capture_output=True, text=True, check=False)


def inspect_video(video_path: Path) -> dict[str, Any]:
    completed = _run(
        [
            "ffprobe",
            "-v",
            "error",
            "-print_format",
            "json",
            "-show_entries",
            "format=duration,size:stream=codec_type,codec_name,width,height,r_frame_rate,nb_frames,avg_frame_rate",
            str(video_path),
        ]
    )
    if completed.returncode != 0:
        raise RuntimeError(f"ffprobe 失败：{completed.stderr.strip() or completed.stdout.strip()}")
    probe = json.loads(completed.stdout)
    video_stream = next(stream for stream in probe["streams"] if stream["codec_type"] == "video")
    audio_stream = next((s for s in probe["streams"] if s.get("codec_type") == "audio"), None)
    rate = video_stream.get("avg_frame_rate") or video_stream.get("r_frame_rate") or "0/1"
    numerator, denominator = (int(value) for value in rate.split("/"))
    duration = float(probe["format"].get("duration") or 0)
    frame_count = int(video_stream.get("nb_frames") or 0)
    fps = round(numerator / denominator, 2) if denominator else 0.0
    return {
        "duration_seconds": round(duration, 3),
        "width": int(video_stream["width"]),
        "height": int(video_stream["height"]),
        "fps": fps,
        "frame_count": frame_count,
        "codec": video_stream.get("codec_name", "unknown"),
        "file_size_mb": round(int(probe["format"].get("size") or 0) / 1_000_000, 2),
        "expected_frames": int(round(duration * fps)) if duration and fps else 0,
        "has_audio": audio_stream is not None,
        "audio_codec": (audio_stream or {}).get("codec_name"),
    }


def check_integrity(video_path: Path, metadata: dict[str, Any] | None = None) -> dict[str, Any]:
    """Lightweight container/stream integrity signals for the adjudicator."""

    meta = metadata or inspect_video(video_path)
    issues: list[dict[str, Any]] = []
    score = 100

    if meta["duration_seconds"] <= 0.2:
        issues.append(
            {
                "type": "abnormal_duration",
                "severity": 3,
                "detail": f"时长异常：{meta['duration_seconds']}s",
            }
        )
        score -= 40

    if meta["width"] < 16 or meta["height"] < 16:
        issues.append(
            {
                "type": "invalid_resolution",
                "severity": 3,
                "detail": f"分辨率异常：{meta['width']}x{meta['height']}",
            }
        )
        score -= 40

    if meta["frame_count"] and meta["expected_frames"]:
        gap = abs(meta["frame_count"] - meta["expected_frames"]) / max(meta["expected_frames"], 1)
        if gap > 0.15:
            issues.append(
                {
                    "type": "frame_count_mismatch",
                    "severity": 2,
                    "detail": f"帧数与时长*fps偏差 {gap:.0%}（{meta['frame_count']} vs {meta['expected_frames']}）",
                }
            )
            score -= 15

    # Black-frame / freeze heuristics on a short decode sample
    sample = _run(
        [
            "ffmpeg",
            "-v",
            "error",
            "-i",
            str(video_path),
            "-vf",
            "blackdetect=d=0.2:pix_th=0.10,freezedetect=n=0.003:d=0.5",
            "-f",
            "null",
            "-",
        ]
    )
    stderr = sample.stderr or ""
    if "black_start" in stderr:
        issues.append(
            {
                "type": "black_frames",
                "severity": 2,
                "detail": "检测到持续黑帧片段",
            }
        )
        score -= 12
    if "freeze_start" in stderr:
        issues.append(
            {
                "type": "frozen_frames",
                "severity": 1,
                "detail": "检测到疑似冻结帧（仍需 VLM 判断是否有意静止）",
            }
        )
        score -= 8

    if sample.returncode != 0 and not issues:
        issues.append(
            {
                "type": "decode_warning",
                "severity": 1,
                "detail": "采样解码出现警告，建议人工复核",
            }
        )
        score -= 5

    score = max(0, min(100, score))
    verdict = "pass"
    if any(item["severity"] >= 3 for item in issues):
        verdict = "violated"
    elif issues:
        verdict = "needs_review"

    return {
        "score": score,
        "verdict": verdict,
        "issues": issues,
        "notes": "完整性预检为确定性工具结果；冻结/黑帧等仍需 VLM 结合语义判定。",
    }


def extract_keyframes(video_path: Path, output_dir: Path, fps: float = 1.5, max_frames: int = 12) -> list[Path]:
    output_dir.mkdir(parents=True, exist_ok=True)
    pattern = output_dir / "frame_%03d.jpg"
    for old in output_dir.glob("frame_*.jpg"):
        old.unlink()
    # `-frames:v max_frames` 配固定 fps 时，只会保留**开头** max_frames/fps 秒
    # ——12 帧 @1.5fps 就是前 8 秒，更长的片子后半段根本不进拼图。按时长压低采样率，
    # 让同样的帧预算均匀铺满全片。
    effective_fps = fps
    try:
        duration = float(inspect_video(video_path).get("duration_seconds") or 0)
        if duration > 0 and max_frames > 0:
            effective_fps = min(fps, max_frames / duration)
    except (RuntimeError, TypeError, ValueError):
        pass
    completed = _run(
        [
            "ffmpeg",
            "-y",
            "-i",
            str(video_path),
            "-vf",
            f"fps={effective_fps:.6f},scale=512:-1",
            "-frames:v",
            str(max_frames),
            str(pattern),
        ]
    )
    if completed.returncode != 0:
        raise RuntimeError(f"抽帧失败：{completed.stderr.strip()[:500]}")
    return sorted(output_dir.glob("frame_*.jpg"))


# ---------------------------------------------------------------------------
# Motion-aware sampling
#
# Uniform sampling under-covers high-motion moments (a spinning ball, a fast
# pan) where rotation/continuity artefacts live. We derive a per-frame motion
# signal with ffmpeg only (no numpy/opencv): tblend=difference turns each frame
# into its delta from the previous one, and signalstats' YAVG is the mean luma
# of that delta — i.e. how much moved. High-motion windows then get densified
# sampling on top of a base uniform grid.
# ---------------------------------------------------------------------------


def detect_shot_segments(
    video_path: Path,
    metadata: dict[str, Any],
    *,
    threshold: float = 0.32,
    max_shots: int = 16,
) -> list[dict[str, float | int]]:
    duration = float(metadata.get("duration_seconds") or 0)
    if duration <= 0:
        return []
    completed = _run(
        [
            "ffmpeg", "-v", "info", "-i", str(video_path),
            "-vf", f"select='gt(scene,{threshold})',metadata=print:file=-",
            "-an", "-f", "null", "-",
        ]
    )
    boundaries: list[float] = []
    for line in (completed.stdout + "\n" + completed.stderr).splitlines():
        match = re.search(r"pts_time:([0-9.]+)", line)
        if not match:
            continue
        value = float(match.group(1))
        if 0.15 < value < duration - 0.15 and all(abs(value - old) >= 0.2 for old in boundaries):
            boundaries.append(round(value, 3))
    boundaries = sorted(boundaries)[: max(0, max_shots - 1)]
    edges = [0.0, *boundaries, duration]
    return [
        {"shot_id": index, "start": round(edges[index - 1], 3), "end": round(edges[index], 3)}
        for index in range(1, len(edges))
        if edges[index] - edges[index - 1] >= 0.15
    ]


def normalize_bbox(bbox: Any) -> tuple[float, float, float, float] | None:
    """把模型给的 bbox 统一成 0..1，识别不出尺度就返回 None。

    提示词要求 0~1 归一化，但 Keye 常按 Qwen2-VL 的习惯回整数坐标
    （实测返回过 ``[145, 137, 435, 489]``）。以前这种框会被直接 clamp 成
    ``x1=1.0, x2=144.94``，ffmpeg crop 表达式随即失败，整条区域通道静默失效
    ——定位器明明挑出了 4 个目标，结果一个都没比上。
    """
    if not isinstance(bbox, (list, tuple)) or len(bbox) != 4:
        return None
    try:
        values = [float(v) for v in bbox]
    except (TypeError, ValueError):
        return None
    peak = max(abs(v) for v in values)
    if peak <= 1.5:
        scale = 1.0
    elif peak <= 1000.0:
        scale = 1000.0  # Qwen 系惯用的千分比坐标；也近似于 ~1000px 宽帧的像素坐标
    else:
        scale = peak  # 更大的量级只能按最大值折算，至少保住相对位置
    x1, y1, x2, y2 = (v / scale for v in values)
    return x1, y1, x2, y2


def extract_region_crops(
    video_path: Path,
    output_dir: Path,
    regions: list[dict[str, Any]],
    *,
    target_side: int = 896,
    pad: float = 0.06,
) -> list[dict[str, Any]]:
    """Crop a normalized bbox out of the native frame at each timestamp and upscale it.

    Small on-screen text (a product label, a radio faceplate) survives neither the
    scan-resolution downscale nor Keye's per-frame token cap: at full-frame scale it
    is only a few dozen pixels tall and the model reports "no text in frame". Cropping
    the located region and scaling it *up* spends the same token budget on 100x fewer
    pixels, which is what makes the text legible.

    ``regions`` items: ``{"time": float, "bbox": [x1, y1, x2, y2]}``. bbox 可以是
    0..1 归一化，也可以是整数坐标（交给 :func:`normalize_bbox` 折算）。
    Returns the same dicts plus a ``path`` for each crop made.
    """
    output_dir.mkdir(parents=True, exist_ok=True)
    for old in output_dir.glob("crop_*.jpg"):
        old.unlink()
    made: list[dict[str, Any]] = []
    for index, region in enumerate(regions, start=1):
        normalized = normalize_bbox(region.get("bbox"))
        if normalized is None:
            continue
        x1, y1, x2, y2 = normalized
        # 收紧到 0..1 并加一点边距，避免把文字边缘切掉
        x1, x2 = sorted((max(0.0, x1 - pad), min(1.0, x2 + pad)))
        y1, y2 = sorted((max(0.0, y1 - pad), min(1.0, y2 + pad)))
        width, height = x2 - x1, y2 - y1
        if width <= 0.01 or height <= 0.01:
            continue
        out = output_dir / f"crop_{index:02d}.jpg"
        # crop 用 iw/ih 表达式，避免自己去查分辨率；随后放大到 target_side
        vf = (
            f"crop=iw*{width:.4f}:ih*{height:.4f}:iw*{x1:.4f}:ih*{y1:.4f},"
            f"scale='min({target_side},iw*4)':-2:flags=lanczos"
        )
        completed = _run([
            "ffmpeg", "-y", "-v", "error",
            "-ss", f"{max(0.0, float(region.get('time') or 0)):.3f}",
            "-i", str(video_path), "-frames:v", "1", "-q:v", "2", "-vf", vf, str(out),
        ])
        if completed.returncode == 0 and out.exists():
            made.append({**region, "path": out, "bbox_padded": [x1, y1, x2, y2]})
    return made


def extract_native_frames(
    video_path: Path,
    output_dir: Path,
    times: list[float],
    *,
    max_side: int | None = None,
) -> list[dict[str, Any]]:
    """Grab stills at the given timestamps, optionally capped to ``max_side`` px.

    Gemini charged a flat ~1080 tokens per image regardless of resolution, so the
    original code always kept native size. Keye (Qwen2-VL style dynamic-resolution
    ViT) is the opposite: cost scales with pixels at roughly ``(H/28) * (W/28)``
    tokens, so one 1080p still costs ~2.6k tokens and a 12-frame scan alone blows
    past the 32k context window. ``max_side`` bounds the longer edge (aspect ratio
    preserved, never upscaled) to keep each scan inside its token budget.
    """
    output_dir.mkdir(parents=True, exist_ok=True)
    for old in output_dir.glob("zoom_*.jpg"):
        old.unlink()
    # force_original_aspect_ratio=decrease + min(iw,N) shrinks only when needed.
    scale_filter = (
        f"scale='min(iw,{int(max_side)})':'min(ih,{int(max_side)})'"
        ":force_original_aspect_ratio=decrease:flags=lanczos"
        if max_side and max_side > 0
        else None
    )
    frames: list[dict[str, Any]] = []
    for index, t in enumerate(times, start=1):
        out = output_dir / f"zoom_{index:02d}.jpg"
        command = [
            # -ss before -i seeks fast; -q:v 2 keeps JPEG quality high.
            "ffmpeg", "-y", "-v", "error", "-ss", f"{max(0.0, t):.3f}",
            "-i", str(video_path), "-frames:v", "1", "-q:v", "2",
        ]
        if scale_filter:
            command += ["-vf", scale_filter]
        command.append(str(out))
        completed = _run(command)
        if completed.returncode == 0 and out.exists():
            frames.append({"path": out, "time": round(t, 3)})
    return frames


def motion_profile(video_path: Path, sample_fps: float = 8.0, scale_width: int = 160) -> list[dict[str, float]]:
    """Per-frame motion magnitude as ``[{"time": s, "motion": YAVG}, ...]``.

    Returns an empty list (caller falls back to uniform sampling) if the probe
    fails, so this never blocks the pipeline.
    """
    completed = _run(
        [
            "ffmpeg",
            "-v",
            "error",
            "-i",
            str(video_path),
            "-vf",
            (
                f"fps={sample_fps},scale={scale_width}:-1,"
                "tblend=all_mode=difference,signalstats,"
                "metadata=print:key=lavfi.signalstats.YAVG:file=-"
            ),
            "-f",
            "null",
            "-",
        ]
    )
    samples: list[dict[str, float]] = []
    pending_time: float | None = None
    for line in completed.stdout.splitlines():
        line = line.strip()
        time_match = re.search(r"pts_time:([0-9.]+)", line)
        if time_match:
            pending_time = float(time_match.group(1))
            continue
        value_match = re.search(r"lavfi\.signalstats\.YAVG=([0-9.]+)", line)
        if value_match and pending_time is not None:
            samples.append({"time": round(pending_time, 3), "motion": round(float(value_match.group(1)), 4)})
            pending_time = None
    return samples


def exposure_profile(video_path: Path, sample_fps: float = 2.0) -> dict[str, float]:
    """全片的曝光/色彩客观统计量。

    为什么需要它：判官模型看到的是降帧降分辨率的画面，对「整片偏暗还是偏亮」
    「饱和度是否过高」这类**全局分布**量的感知很不可靠，而且它会用美学判断
    覆盖掉观察结论（实测 visual 维度 23 个正例漏了 20 个，理由清一色是
    「画面有电影感、光影漂亮」）。ffmpeg 能把这些量精确算出来，交给模型当
    客观参照，比让它「凭感觉看」稳得多。

    返回全片均值：``y``（亮度 0-255）、``sat``（饱和度）、``yhigh``/``ylow``
    （高光/暗部）、``contrast``（yhigh-ylow）。探测失败返回空 dict，调用方
    自行降级 —— 这个信号是参考项，不该阻断判定。
    """
    completed = _run(
        [
            "ffmpeg",
            "-v",
            "error",
            "-i",
            str(video_path),
            "-vf",
            (
                f"fps={sample_fps},signalstats,"
                "metadata=print:file=-"
            ),
            "-f",
            "null",
            "-",
        ]
    )
    buckets: dict[str, list[float]] = {}
    for line in completed.stdout.splitlines():
        match = re.search(r"lavfi\.signalstats\.(\w+)=(-?[0-9.]+)", line.strip())
        if match:
            buckets.setdefault(match.group(1), []).append(float(match.group(2)))
    if not buckets.get("YAVG"):
        return {}

    def mean(key: str) -> float:
        vals = buckets.get(key) or []
        return round(sum(vals) / len(vals), 2) if vals else 0.0

    profile = {
        "y": mean("YAVG"),
        "sat": mean("SATAVG"),
        "yhigh": mean("YHIGH"),
        "ylow": mean("YLOW"),
    }
    profile["contrast"] = round(profile["yhigh"] - profile["ylow"], 2)
    return profile


def _motion_windows(samples: list[dict[str, float]], k: float = 0.8) -> list[dict[str, float]]:
    """Cluster above-threshold samples into ``[{start, end, peak}]`` windows."""
    if not samples:
        return []
    values = [s["motion"] for s in samples]
    mean = sum(values) / len(values)
    variance = sum((v - mean) ** 2 for v in values) / len(values)
    std = variance ** 0.5
    threshold = mean + k * std
    step = samples[1]["time"] - samples[0]["time"] if len(samples) > 1 else 0.2
    windows: list[dict[str, float]] = []
    current: dict[str, float] | None = None
    for sample in samples:
        if sample["motion"] >= threshold:
            if current and sample["time"] - current["end"] <= step * 1.5:
                current["end"] = sample["time"]
                current["peak"] = max(current["peak"], sample["motion"])
            else:
                if current:
                    windows.append(current)
                current = {"start": sample["time"], "end": sample["time"], "peak": sample["motion"]}
    if current:
        windows.append(current)
    return windows


def plan_sample_times(
    samples: list[dict[str, float]],
    duration: float,
    *,
    base_fps: float = 1.0,
    dense_extra: int = 6,
    max_frames: int = 12,
    min_gap: float = 0.12,
) -> list[dict[str, Any]]:
    """Merge a uniform base grid with extra samples on the highest-motion
    frames. Returns ``[{"time": s, "dense": bool}]`` sorted by time.

    预算分配是这里的核心，且**时间覆盖优先于运动加密**。早先的写法在超预算时按
    「dense 优先」截断，于是开头一段剧烈运动就能把中后段的均匀锚点全部逐出——
    模型拿到的 12 帧全落在前 3 秒，片尾发生什么它根本没看到，却仍会对全片下结论。
    现在先把至少 2/3 的预算留给均匀锚点，运动帧只能填补剩余空位。
    """
    base_times: list[float] = []

    # 先建完整的均匀基础网格。超预算时对它**等间隔降采样**，
    # 绝不按时间截断——截断等于直接放弃视频后半段。
    if duration > 0 and base_fps > 0:
        count = max(2, int(round(duration * base_fps)))
        for index in range(count):
            base_times.append(round((index + 0.5) * duration / count, 3))

    if not base_times or max_frames <= 0:
        return []

    def evenly_spaced(values: list[float], count: int) -> list[float]:
        """在 values 上等间隔取 count 个（含首尾）。"""
        if count >= len(values):
            return list(values)
        if count <= 1:
            return [values[len(values) // 2]]
        return [
            values[round(index * (len(values) - 1) / (count - 1))]
            for index in range(count)
        ]

    dense_budget = min(dense_extra, max(0, max_frames // 3)) if samples else 0
    base_budget = min(
        len(base_times),
        max_frames if max_frames < 3 else max(2, max_frames - dense_budget),
    )
    selected: dict[float, bool] = {t: False for t in evenly_spaced(base_times, base_budget)}

    # 在运动最强的时刻加密。min_gap 防止加密帧与锚点几乎重合（那等于白花一帧）。
    for sample in sorted(samples, key=lambda s: s["motion"], reverse=True)[:dense_extra]:
        if len(selected) >= max_frames:
            break
        t = round(float(sample["time"]), 3)
        if 0 <= t < duration and all(abs(t - existing) >= min_gap for existing in selected):
            selected[t] = True

    # 加密帧可能与锚点撞车而没用满配额，用之前被降采样掉的网格点补齐。
    # 白扔可用的帧预算是纯损失。
    for t in base_times:
        if len(selected) >= max_frames:
            break
        if all(abs(t - existing) >= min_gap for existing in selected):
            selected[t] = False

    return [{"time": t, "dense": dense} for t, dense in sorted(selected.items())]


def sample_frames_motion_aware(
    video_path: Path,
    output_dir: Path,
    metadata: dict[str, Any],
    *,
    base_fps: float = 1.0,
    max_frames: int = 12,
    scale_width: int = 512,
) -> dict[str, Any]:
    """Motion-aware keyframe extraction.

    Falls back to uniform ``extract_keyframes`` when the motion probe or the
    selective extraction fails, so callers always get frames.
    """
    duration = float(metadata.get("duration_seconds") or 0)
    fps = float(metadata.get("fps") or 0)

    samples: list[dict[str, float]] = []
    try:
        samples = motion_profile(video_path)
    except Exception:  # noqa: BLE001 - motion signal is best-effort
        samples = []

    if not samples or duration <= 0 or fps <= 0:
        frames = extract_keyframes(video_path, output_dir, fps=1.5, max_frames=max_frames)
        return {
            "frames": [{"path": path, "time": None, "dense": False} for path in frames],
            "paths": frames,
            "motion_samples": samples,
            "windows": [],
            "dense_count": 0,
            "strategy": "uniform_fallback",
        }

    plan = plan_sample_times(samples, duration, base_fps=base_fps, max_frames=max_frames)
    frame_numbers = sorted({max(0, int(round(item["time"] * fps))) for item in plan})

    output_dir.mkdir(parents=True, exist_ok=True)
    for old in output_dir.glob("frame_*.jpg"):
        old.unlink()
    select_expr = "+".join(f"eq(n\\,{n})" for n in frame_numbers)
    completed = _run(
        [
            "ffmpeg",
            "-y",
            "-i",
            str(video_path),
            "-vf",
            f"select='{select_expr}',scale={scale_width}:-1",
            "-vsync",
            "0",
            "-frames:v",
            str(len(frame_numbers)),
            str(output_dir / "frame_%03d.jpg"),
        ]
    )
    frame_paths = sorted(output_dir.glob("frame_*.jpg"))
    if completed.returncode != 0 or not frame_paths:
        frames = extract_keyframes(video_path, output_dir, fps=1.5, max_frames=max_frames)
        return {
            "frames": [{"path": path, "time": None, "dense": False} for path in frames],
            "paths": frames,
            "motion_samples": samples,
            "windows": _motion_windows(samples),
            "dense_count": 0,
            "strategy": "uniform_fallback",
        }

    # Extracted frames are in temporal order == sorted(frame_numbers); zip back
    # to the plan (also time-sorted) to recover each frame's time / dense flag.
    plan_by_time = sorted(plan, key=lambda item: item["time"])
    descriptors: list[dict[str, Any]] = []
    for path, number, planned in zip(frame_paths, frame_numbers, plan_by_time):
        descriptors.append(
            {
                "path": path,
                "time": round(number / fps, 3),
                "dense": bool(planned["dense"]),
            }
        )
    return {
        "frames": descriptors,
        "paths": frame_paths,
        "motion_samples": samples,
        "windows": _motion_windows(samples),
        "dense_count": sum(1 for d in descriptors if d["dense"]),
        "strategy": "motion_aware",
    }
