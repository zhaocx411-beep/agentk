"""历史质检记录：把 `runtime/results/` 反向读成一份可浏览的清单。

没有单独的历史数据库 —— 每次跑完的完整报告本来就落在 `runtime/results/`，
文件名就是缓存键（`<slug>__<source>__<engine>__<mode>__<CACHE_VERSION>`），
所以历史列表是对这批文件的**只读投影**，不引入第二份真相。

三件事值得知道：

1. 报告 JSON 单份 250KB+，列表页不能每次把全部文件解析一遍。这里按
   (文件名, mtime, size) 记忆化摘要，只有文件真的变了才重新解析。
2. 标题优先取用户手改的名字，其次 Prompt 首行，再次视频原文件名 —— 因为
   Prompt 非必填，纯素材质检的记录没有任何文字可用，只剩文件名能认人。
3. 同一批素材可能有多条记录（换通道跑、mock 与 agentk 并存），它们共享
   `uploads/<slug>/` 下的素材。删除时必须确认没有别的记录还在引用，
   否则删掉素材会让另一条记录的视频回放变成 404。
"""
from __future__ import annotations

import json
import re
import shutil
import threading
from pathlib import Path
from typing import Any
from urllib.parse import quote

from aigc_eval import paths
from aigc_eval.judge.native8 import summarize_native8

# 缓存键的形状由 judge/result_cache.py 决定；这里按同一形状反解。
# 严格白名单同时充当路径校验 —— id 来自请求，绝不能带 / 或 ..
_ID_RE = re.compile(r"[0-9A-Za-z_.-]+")
_KEY_RE = re.compile(
    r"^(?P<slug>[0-9A-Za-z_.-]+?)__(?P<source>[0-9A-Za-z_.-]+)"
    r"__(?P<engine>[0-9A-Za-z_.-]+)__(?P<mode>[0-9A-Za-z_.-]+)__(?P<version>v\d+)$"
)
# multipart 落盘时给文件名加了 `NN_` 序号前缀，回显标题要剥掉。
_ASSET_PREFIX_RE = re.compile(r"^\d{2}_")

TITLES_PATH = paths.RUNTIME_ROOT / "history_titles.json"

_TITLES_LOCK = threading.Lock()
_SUMMARY_LOCK = threading.Lock()
# name -> (mtime, size, summary)
_SUMMARY_CACHE: dict[str, tuple[float, int, dict[str, Any]]] = {}

MAX_TITLE_LEN = 80


# ---------------------------------------------------------------------------
# 自定义标题：单文件 JSON，键是记录 id
# ---------------------------------------------------------------------------


def _load_titles() -> dict[str, str]:
    if not TITLES_PATH.exists():
        return {}
    try:
        data = json.loads(TITLES_PATH.read_text(encoding="utf-8"))
    except Exception:  # noqa: BLE001 - 坏文件等同于「没有自定义标题」
        return {}
    return {str(k): str(v) for k, v in data.items()} if isinstance(data, dict) else {}


def set_title(record_id: str, title: str) -> str:
    """写入或清空一条记录的自定义标题；返回归一后的标题（空串表示回到自动推导）。"""
    record_id = _validate_id(record_id)
    title = re.sub(r"\s+", " ", str(title or "")).strip()[:MAX_TITLE_LEN]
    with _TITLES_LOCK:
        titles = _load_titles()
        if title:
            titles[record_id] = title
        else:
            titles.pop(record_id, None)
        paths.RUNTIME_ROOT.mkdir(parents=True, exist_ok=True)
        TITLES_PATH.write_text(json.dumps(titles, ensure_ascii=False, indent=2), encoding="utf-8")
    return title


# ---------------------------------------------------------------------------
# 摘要解析
# ---------------------------------------------------------------------------


def _validate_id(record_id: str) -> str:
    record_id = str(record_id or "")
    if not _ID_RE.fullmatch(record_id) or not _KEY_RE.match(record_id):
        raise ValueError("非法记录 id")
    return record_id


def _prompt_text(prompt: Any) -> str:
    """报告里的 prompt 既可能是多语言 dict，也可能是老报告的纯字符串。"""
    if isinstance(prompt, str):
        return prompt
    if isinstance(prompt, dict):
        for key in ("zh", "prompt_zh", "en", "prompt_en"):
            value = prompt.get(key)
            if isinstance(value, str) and value.strip():
                return value
    return ""


def _first_line(text: str, limit: int = MAX_TITLE_LEN) -> str:
    for line in str(text or "").splitlines():
        line = line.strip()
        if line:
            return line[:limit]
    return ""


def _file_size(path: Path) -> int | None:
    try:
        return path.stat().st_size
    except OSError:
        return None


def _asset_name(path_text: str) -> str:
    name = Path(str(path_text or "")).name
    return _ASSET_PREFIX_RE.sub("", name)


def _manifest(slug: str) -> dict[str, Any]:
    manifest_path = paths.UPLOAD_ROOT / slug / "manifest.json"
    if not manifest_path.exists():
        return {}
    try:
        data = json.loads(manifest_path.read_text(encoding="utf-8"))
    except Exception:  # noqa: BLE001
        return {}
    return data if isinstance(data, dict) else {}


def _summarize(path: Path) -> dict[str, Any]:
    """把一份完整报告压成列表要用的那十几个字段。"""
    key = _KEY_RE.match(path.stem)
    if key is None:
        raise ValueError(f"无法解析的缓存文件名：{path.name}")
    report = json.loads(path.read_text(encoding="utf-8"))

    slug = key.group("slug")
    source = key.group("source")
    media = report.get("media") if isinstance(report.get("media"), dict) else {}
    refs = [r for r in (media.get("reference_urls") or []) if isinstance(r, dict)]
    trace = report.get("llm_trace") if isinstance(report.get("llm_trace"), dict) else {}

    prompt_text = _prompt_text(report.get("prompt"))
    manifest = _manifest(slug)
    video_name = _asset_name(manifest.get("video") or "")
    native8 = summarize_native8(report)

    return {
        "id": path.stem,
        "slug": slug,
        "source": source,
        "engine": key.group("engine"),
        "mode": key.group("mode"),
        "run_id": trace.get("run_id") or None,
        # 列表页只认「查出多少 / 有多少没看清」这两个量级：单看前者会把
        # 一条都没核实的记录显示成干净的。侧边栏圆点对认不出的状态不上色。
        "analysis_status": native8["analysis_status"],
        "violated_subcriteria": native8["violated_subcriteria"],
        "unverified_dimensions": native8["unverified_dimensions"],
        "review_required_count": native8["review_required_count"],
        # 有图参考才算 I2VA，与上传接口返回的 mode 徽标同一口径
        "mode_badge": "I2VA" if any(r.get("kind") == "image" for r in refs) else "T2VA",
        "prompt_preview": _first_line(prompt_text, 140),
        "auto_title": _first_line(prompt_text) or video_name or slug,
        "video_name": video_name,
        "ref_count": len(refs),
        "video_url": media.get("video_url") or None,
        "has_report": bool(report.get("report_html_path") or trace.get("run_id")),
    }


def _summary_for(path: Path) -> dict[str, Any] | None:
    """记忆化解析。文件没变就直接复用上次的摘要。"""
    try:
        stat = path.stat()
    except OSError:
        return None
    with _SUMMARY_LOCK:
        cached = _SUMMARY_CACHE.get(path.name)
        if cached and cached[0] == stat.st_mtime and cached[1] == stat.st_size:
            return dict(cached[2])
    try:
        summary = _summarize(path)
    except Exception:  # noqa: BLE001 - 坏掉的单份缓存不该让整个列表挂掉
        return None
    summary["ts"] = stat.st_mtime
    with _SUMMARY_LOCK:
        _SUMMARY_CACHE[path.name] = (stat.st_mtime, stat.st_size, dict(summary))
    return summary


def list_history(limit: int = 200) -> list[dict[str, Any]]:
    """按最近一次跑的时间倒序列出历史记录。"""
    if not paths.RESULTS_ROOT.exists():
        return []
    files = sorted(
        paths.RESULTS_ROOT.glob("*.json"),
        key=lambda p: p.stat().st_mtime if p.exists() else 0,
        reverse=True,
    )
    titles = _load_titles()
    items: list[dict[str, Any]] = []
    for path in files[: max(1, limit)]:
        summary = _summary_for(path)
        if summary is None:
            continue
        custom = titles.get(summary["id"])
        summary["title"] = custom or summary["auto_title"]
        summary["title_custom"] = bool(custom)
        items.append(summary)
    return items


# ---------------------------------------------------------------------------
# 单条记录：回看与重跑
# ---------------------------------------------------------------------------


def _result_path(record_id: str) -> Path:
    path = (paths.RESULTS_ROOT / f"{record_id}.json").resolve()
    # id 已过白名单，这一步是纵深防御：确保结果落在 results/ 内
    if paths.RESULTS_ROOT.resolve() not in path.parents:
        raise ValueError("非法记录 id")
    if not path.exists():
        raise FileNotFoundError("记录不存在或已被删除")
    return path


def load_record(record_id: str) -> dict[str, Any]:
    """返回 {report, restore}：report 直接喂给前端的报告渲染，
    restore 是把左栏输入还原成当时那次提交所需的最小信息。"""
    record_id = _validate_id(record_id)
    path = _result_path(record_id)
    report = json.loads(path.read_text(encoding="utf-8"))
    report["from_cache"] = True

    summary = _summary_for(path) or {}
    titles = _load_titles()
    summary["title"] = titles.get(record_id) or summary.get("auto_title") or record_id
    summary["title_custom"] = record_id in titles

    slug = summary.get("slug") or _KEY_RE.match(record_id).group("slug")  # type: ignore[union-attr]
    manifest = _manifest(slug)
    media = report.get("media") if isinstance(report.get("media"), dict) else {}
    ref_urls = {
        str(r.get("reference_id")): r
        for r in (media.get("reference_urls") or [])
        if isinstance(r, dict)
    }

    references = []
    for ref in manifest.get("references") or []:
        if not isinstance(ref, dict):
            continue
        label = str(ref.get("reference_id") or ref.get("label") or "")
        source_path = Path(str(ref.get("path") or ""))
        references.append({
            "label": label,
            "kind": ref.get("kind") or "image",
            "name": _asset_name(ref.get("path") or ""),
            "expected_role": ref.get("expected_role") or "",
            "available": source_path.exists(),
            "size": _file_size(source_path),
            "url": (ref_urls.get(label) or {}).get("url")
            or f"./api/upload_media?slug={quote(slug, safe='')}"
               f"&asset=reference&ref_id={quote(label, safe='')}",
        })

    video_path = Path(str(manifest.get("video") or ""))
    return {
        "item": summary,
        "report": report,
        "restore": {
            "slug": slug,
            "prompt": _prompt_text(report.get("prompt")),
            "video": {
                "name": _asset_name(manifest.get("video") or "") or "待质检视频",
                "url": media.get("video_url")
                or f"./api/upload_media?slug={quote(slug, safe='')}&asset=video",
                "available": video_path.exists(),
                "size": _file_size(video_path),
            },
            "references": references,
            # 素材齐全才能原地重跑；缺文件时前端只回看，不给「开始质检」
            "rerunnable": video_path.exists() and all(r["available"] for r in references),
        },
    }


def history_case_sources(slug: str) -> tuple[Path, list[dict[str, Any]]]:
    """按 slug 从 manifest 还原 (视频路径, 参考素材)，供「改一改重跑」复用磁盘素材。

    与离线数据集那条分支同构：素材已在服务器上，浏览器只回传 slug，
    不必把几十 MB 再传一遍。
    """
    if not re.fullmatch(r"up_[0-9a-f]{16}", slug or ""):
        raise ValueError("非法 slug")
    manifest = _manifest(slug)
    if not manifest:
        raise FileNotFoundError(f"历史素材不存在：{slug}")
    video_path = Path(str(manifest.get("video") or ""))
    if not video_path.exists():
        raise FileNotFoundError("历史视频文件已被清理，无法重跑")
    references: list[dict[str, Any]] = []
    for ref in manifest.get("references") or []:
        if not isinstance(ref, dict):
            continue
        path = Path(str(ref.get("path") or ""))
        if not path.exists():
            raise FileNotFoundError(f"历史参考素材已被清理：{path.name}")
        references.append({
            "path": str(path),
            "label": ref.get("reference_id") or ref.get("label"),
            "kind": ref.get("kind"),
            "expected_role": ref.get("expected_role"),
        })
    return video_path, references


# ---------------------------------------------------------------------------
# 删除
# ---------------------------------------------------------------------------


def _slug_referenced_elsewhere(slug: str, exclude_id: str) -> bool:
    """同一 slug 是否还有别的记录在用（换通道跑会产生多条）。"""
    for path in paths.RESULTS_ROOT.glob(f"{slug}__*.json"):
        if path.stem != exclude_id:
            return True
    return False


def _assets_borrowed_by_others(slug: str) -> bool:
    """别的 slug 的 manifest 是否指向本 slug 目录下的文件。

    「从历史记录改一改重跑」时素材是原地引用而非复制的，新记录的 manifest
    会指回旧 slug 的 assets/。只查同 slug 的结果文件就漏掉了这种跨 slug 引用，
    删掉旧记录会让新记录的视频回放和证据图静默变成 404。
    """
    own_dir = (paths.UPLOAD_ROOT / slug).resolve()
    if not paths.UPLOAD_ROOT.exists():
        return False
    for manifest_path in paths.UPLOAD_ROOT.glob("*/manifest.json"):
        if manifest_path.parent.resolve() == own_dir:
            continue
        try:
            data = json.loads(manifest_path.read_text(encoding="utf-8"))
        except Exception:  # noqa: BLE001
            continue
        candidates = [data.get("video")]
        candidates += [r.get("path") for r in (data.get("references") or []) if isinstance(r, dict)]
        for candidate in candidates:
            if not candidate:
                continue
            try:
                resolved = Path(str(candidate)).resolve()
            except OSError:
                continue
            if own_dir == resolved or own_dir in resolved.parents:
                return True
    return False


def delete_record(record_id: str, *, with_assets: bool = True) -> dict[str, Any]:
    """删一条记录。with_assets 时连带清掉该次的素材与中间产物。

    共享素材（同 slug 的其他记录还在）一律保留 —— 删掉会让那条记录的
    视频回放和证据图直接失效。
    """
    record_id = _validate_id(record_id)
    path = _result_path(record_id)
    summary = _summary_for(path) or {}
    slug = summary.get("slug") or _KEY_RE.match(record_id).group("slug")  # type: ignore[union-attr]
    source = summary.get("source") or "upload"
    run_id = summary.get("run_id")

    removed: list[str] = []

    def drop_file(target: Path) -> None:
        if target.exists() and target.is_file():
            target.unlink()
            removed.append(target.name)

    def drop_tree(target: Path) -> None:
        if target.exists() and target.is_dir():
            shutil.rmtree(target, ignore_errors=True)
            removed.append(f"{target.name}/")

    path.unlink()
    removed.append(path.name)
    with _SUMMARY_LOCK:
        _SUMMARY_CACHE.pop(path.name, None)
    set_title(record_id, "")

    # 报告与轨迹按 run_id 命名，一次运行独占，可以无条件删。
    if run_id and re.fullmatch(r"[0-9A-Za-z_.-]+", run_id):
        drop_file(paths.REPORT_ROOT / f"{run_id}.html")
        drop_file(paths.TRACE_ROOT / f"{run_id}.json")

    # 中间产物按 slug 独占，同 slug 没有别的记录就能删；
    # 素材还要再确认没有被别的 slug 借用（历史重跑会原地引用）。
    shared = _slug_referenced_elsewhere(slug, record_id)
    if with_assets and not shared:
        drop_tree(paths.WORK_ROOT / f"{slug}__{source}")
        drop_file(paths.RUNTIME_ROOT / f"{slug}__{source}_contact.jpg")
        borrowed = _assets_borrowed_by_others(slug)
        if not borrowed:
            drop_tree(paths.UPLOAD_ROOT / slug)
        shared = borrowed

    return {
        "ok": True,
        "id": record_id,
        "removed": removed,
        "assets_kept_shared": bool(with_assets and shared),
    }
