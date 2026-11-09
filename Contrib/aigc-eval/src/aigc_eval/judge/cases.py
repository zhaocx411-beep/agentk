"""案例发现与定位：把 case_id 解析成磁盘上的一组素材。

`CaseFiles` 是整条判定链的输入契约。case 有两种来源，都收敛到同一个结构：
- 身份寻址：`<仓库上一级>/{I2VA*,T2VA*}/<case>/`，由 `get_case_files` 解析；
- 内容寻址：上传流产生的 slug 目录，由 `dataset/upload_case.py` 构造。
"""
from __future__ import annotations

import hashlib
import json
import mimetypes
import re
from pathlib import Path
from typing import Any
from urllib.parse import quote

from aigc_eval.judge.layout import PROJECT_ROOT
from aigc_eval.judge.media_parts import reference_kind
from aigc_eval.judge.settings import COLLECTION_PREFIXES, CURATED_CASES


def _collection_focus(collection: str) -> str:
    """集合名 → 默认 focus 文案。

    V2VA 加进来之前这是个二元条件表达式，散落在 `discover_cases` 与报告组装两处；
    加第三个分支时必须收口，否则两边容易只改一处（表现是列表页显示 V2V、报告页
    却写 I2V 生成）。
    """
    if collection.startswith("V2VA"):
        return "V2V 视频编辑"
    if collection.startswith("T2VA"):
        return "T2V 生成"
    return "I2V 生成"


def _slug(case_id: str) -> str:
    """Filesystem-safe, collision-free key for runtime artifacts.

    Collection names contain CJK; a plain sanitize would collapse them, so we
    append a short hash of the full composite id to guarantee uniqueness.
    """
    base = re.sub(r"[^0-9A-Za-z_.-]", "_", case_id)[:48]
    digest = hashlib.md5(case_id.encode("utf-8")).hexdigest()[:6]
    return f"{base}_{digest}"


def _collection_dirs() -> list[Path]:
    """Top-level I2VA*/T2VA* folders that actually contain cases (bounded,
    single-level listing — not a recursive scan)."""
    dirs: list[Path] = []
    for child in sorted(PROJECT_ROOT.iterdir()):
        if not child.is_dir() or not child.name.startswith(COLLECTION_PREFIXES):
            continue
        try:
            if next(child.glob("*/prompt.json"), None) is not None:
                dirs.append(child)
        except OSError:
            continue
    return dirs


class CaseFiles:
    def __init__(
        self,
        case_id: str,
        collection: str,
        case_name: str,
        case_dir: Path,
        prompt_path: Path,
        video_path: Path,
        seedance_path: Path | None,
        input_image_path: Path | None,
        reference_assets: list[dict[str, Any]] | None = None,
    ):
        self.case_id = case_id  # composite "collection/case_name"
        self.collection = collection
        self.case_name = case_name
        self.slug = _slug(case_id)
        self.case_dir = case_dir
        self.prompt_path = prompt_path
        self.video_path = video_path  # Kling (the analyzed video)
        self.seedance_path = seedance_path
        self.input_image_path = input_image_path
        self.reference_assets = reference_assets or []


def _resolve_case_dir(case_id: str) -> Path:
    if ".." in case_id or case_id.startswith("/") or "\\" in case_id:
        raise ValueError("非法 case_id")
    collections = _collection_dirs()
    if "/" in case_id:
        collection, case_name = case_id.split("/", 1)
        candidate = (PROJECT_ROOT / collection / case_name).resolve()
    else:
        # Backward compatible: a bare case name → find the collection holding it.
        candidate = None
        for col in collections:
            maybe = col / case_id
            if (maybe / "prompt.json").exists():
                candidate = maybe.resolve()
                break
        if candidate is None:
            raise FileNotFoundError(f"未找到案例：{case_id}")
    allowed = {col.resolve() for col in collections}
    if candidate.parent not in allowed:
        raise ValueError("非法 case_id")
    if not candidate.is_dir():
        raise FileNotFoundError(f"未找到案例：{case_id}")
    return candidate


def get_case_files(case_id: str) -> CaseFiles:
    case_dir = _resolve_case_dir(case_id)
    collection = case_dir.parent.name
    case_name = case_dir.name
    norm_id = f"{collection}/{case_name}"
    prompt_path = case_dir / "prompt.json"
    kling = sorted(case_dir.glob("Kling*.mp4"))
    seedance = sorted([*case_dir.glob("seedance*.mp4"), *case_dir.glob("Seedance*.mp4")])
    if not prompt_path.exists() or not kling:
        raise FileNotFoundError("案例缺少 prompt.json 或 Kling 视频")
    images = sorted([*case_dir.glob("*.jpg"), *case_dir.glob("*.jpeg"), *case_dir.glob("*.png")])
    input_image = images[0] if images else None
    references: list[dict[str, Any]] = []
    if input_image:
        references.append(
            {
                "reference_id": "input_image",
                "path": str(input_image),
                "expected_role": "I2V 首帧 / 主体参考",
                "required": True,
            }
        )
    # 额外参考图（若有）一并作为一致性参考。
    for index, image in enumerate(images[1:], start=1):
        references.append(
            {
                "reference_id": f"ref_{index:02d}",
                "path": str(image),
                "expected_role": "参考素材",
                "required": True,
            }
        )
    return CaseFiles(
        case_id=norm_id,
        collection=collection,
        case_name=case_name,
        case_dir=case_dir,
        prompt_path=prompt_path,
        video_path=kling[0],
        seedance_path=seedance[0] if seedance else None,
        input_image_path=input_image,
        reference_assets=references,
    )


_CASES_CACHE: list[dict[str, str]] | None = None


def discover_cases(refresh: bool = False) -> list[dict[str, str]]:
    global _CASES_CACHE
    if _CASES_CACHE is not None and not refresh:
        return _CASES_CACHE
    cases: list[dict[str, str]] = []
    for col in _collection_dirs():
        for case_dir in sorted(p for p in col.iterdir() if p.is_dir()):
            try:
                names = {f.name for f in case_dir.iterdir()}
            except OSError:
                continue
            if "prompt.json" not in names:
                continue
            if not any(n.startswith("Kling") and n.endswith(".mp4") for n in names):
                continue
            case_name = case_dir.name
            has_img = any(n.lower().endswith((".jpg", ".jpeg", ".png")) for n in names)
            has_seedance = any(n.lower().startswith("seedance") and n.endswith(".mp4") for n in names)
            metadata = CURATED_CASES.get(case_name, {})
            cases.append(
                {
                    "case_id": f"{col.name}/{case_name}",
                    "collection": col.name,
                    "case_name": case_name,
                    "label": metadata.get("label", case_name),
                    "focus": metadata.get("focus", _collection_focus(col.name)),
                    "curated": "true" if case_name in CURATED_CASES else "false",
                    "mode_hint": "I2V" if has_img else "T2V",
                    "has_seedance": "true" if has_seedance else "false",
                }
            )
    cases.sort(key=lambda item: (item["collection"], item["curated"] != "true", item["label"]))
    _CASES_CACHE = cases
    return cases


def list_collections() -> list[dict[str, Any]]:
    """Collections with their case counts, for the UI's first-level selector."""
    grouped: dict[str, int] = {}
    for case in discover_cases():
        grouped[case["collection"]] = grouped.get(case["collection"], 0) + 1
    return [{"collection": name, "count": count} for name, count in grouped.items()]


def case_info(case_id: str) -> dict[str, Any]:
    """Lightweight case metadata + media URLs for the pre-run preview."""
    cf = get_case_files(case_id)
    prompt = load_prompt(cf)
    encoded = quote(cf.case_id, safe="")
    return {
        "case_id": cf.case_id,
        "collection": cf.collection,
        "case_name": cf.case_name,
        "mode": detect_mode(cf),
        "prompt": prompt,
        "has_input_image": cf.input_image_path is not None,
        "has_seedance": cf.seedance_path is not None,
        "media": {
            "kling_url": f"./api/case-media?case_id={encoded}&asset=video",
            "seedance_url": f"./api/case-media?case_id={encoded}&asset=seedance" if cf.seedance_path else None,
            "input_url": f"./api/case-media?case_id={encoded}&asset=input_image" if cf.input_image_path else None,
        },
    }


def load_prompt(case_files: CaseFiles) -> dict[str, str]:
    prompt = json.loads(case_files.prompt_path.read_text(encoding="utf-8"))
    return {
        "zh": prompt.get("prompt_zh", ""),
        "en": prompt.get("prompt_en", ""),
        "gold_label": prompt.get("gold_label", ""),
        "gold_note": prompt.get("gold_note", ""),
    }


def source_video_of(case_files: CaseFiles) -> Path | None:
    """定位 V2V 的源视频，也就是「改动前」的那一段。

    源视频有两条来路：上传流会显式挂在 `source_video_path` 上；身份寻址的 case 则把它
    混在 `reference_assets` 里，只能按素材类型认。两条都要认，否则 V2V 的三个原生子判据
    拿不到基线便无法核验。
    """
    direct = getattr(case_files, "source_video_path", None)
    if direct and Path(direct).exists():
        return Path(direct)
    for reference in case_files.reference_assets or []:
        if (
            reference_kind(reference) == "video"
            and reference.get("path")
            and Path(str(reference["path"])).exists()
        ):
            return Path(str(reference["path"]))
    return None


def detect_mode(case_files: CaseFiles, mode_override: str | None = None) -> str:
    """有参考视频 → V2V；否则有参考图 → I2V；都没有 → T2V。

    **V2V 优先于 I2V**：V2V case 也可能附带参考图（换装参考、风格参考），但只要给了
    源视频，判据结构就是「相对源视频改了什么」，而不是「像不像那张图」——套 I2V 的参考
    关系会失真，且 V2V 三个原生子判据拿不到源视频便无法核验。

    纯音频参考不改变模式；其内容与风格要求归指令遵循，不构成独立生成范式。
    """
    if mode_override in {"T2V", "I2V", "V2V"}:
        return mode_override
    if source_video_of(case_files) is not None:
        return "V2V"
    if any(reference_kind(r) == "image" for r in (case_files.reference_assets or [])):
        return "I2V"
    return "I2V" if case_files.input_image_path else "T2V"


def media_path(case_id: str, asset: str, ref_id: str | None = None) -> tuple[Path, str]:
    case_files = get_case_files(case_id)
    if asset in ("video", "kling"):
        path = case_files.video_path
    elif asset in ("seedance", "seedance_video"):
        if not case_files.seedance_path:
            raise FileNotFoundError("该案例无 seedance 视频")
        path = case_files.seedance_path
    elif asset == "input_image" and case_files.input_image_path:
        path = case_files.input_image_path
    elif asset == "reference":
        match = next((r for r in case_files.reference_assets if r["reference_id"] == ref_id), None)
        if not match:
            raise FileNotFoundError("参考图不存在")
        path = Path(match["path"])
    else:
        raise FileNotFoundError("媒体不存在")
    return path, mimetypes.guess_type(path.name)[0] or "application/octet-stream"
