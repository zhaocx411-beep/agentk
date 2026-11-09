"""历史质检记录的索引与删除测试。

在一个临时 runtime 目录里造出完整的目录形态（results / uploads / work /
reports / traces + 顶层 contact 图），因为这里要验的几件事都会**静默**出错：

- 缓存文件名反解错了，列表只会少几条，不报错；
- 标题回退链断了，纯素材质检的记录会变成一串哈希，仍然「能用」；
- 路径校验漏了，`id=../../x` 会读到 results 之外的文件；
- 跨 slug 的素材引用（从历史记录改一改重跑时是原地引用的）没识别出来，
  删旧记录会把新记录的视频删掉，只有点开回放才发现 404。
"""

from __future__ import annotations

import json
import shutil
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from aigc_eval import history, paths  # noqa: E402

FAILURES: list[str] = []


def check(label: str, ok: bool, detail: object = "") -> None:
    print(f"{'PASS' if ok else 'FAIL'}  {label}" + (f" — {detail}" if not ok else ""))
    if not ok:
        FAILURES.append(label)


def point_paths_at(root: Path) -> None:
    """把 paths 的锚点整体指向临时目录，并让 history 模块跟着走。"""
    paths.RUNTIME_ROOT = root
    paths.RESULTS_ROOT = root / "results"
    paths.WORK_ROOT = root / "work"
    paths.REPORT_ROOT = root / "reports"
    paths.TRACE_ROOT = root / "traces"
    paths.UPLOAD_ROOT = root / "uploads"
    for name in ("results", "work", "reports", "traces", "uploads"):
        (root / name).mkdir(parents=True, exist_ok=True)
    history.TITLES_PATH = root / "history_titles.json"
    history._SUMMARY_CACHE.clear()


def write_record(
    root: Path,
    *,
    slug: str,
    engine: str = "agentk",
    prompt: dict | str | None = None,
    video_name: str = "clip.mp4",
    run_id: str = "",
    score: int = 88,
    status: str = "pass",
    label: str = "通过",
    borrow_from: str | None = None,
) -> str:
    """造一条完整记录，返回它的 id。borrow_from 模拟「历史重跑」的原地引用。"""
    owner = borrow_from or slug
    assets = root / "uploads" / owner / "assets"
    assets.mkdir(parents=True, exist_ok=True)
    video = assets / f"01_{video_name}"
    video.write_bytes(b"fake-video-bytes")

    (root / "uploads" / slug).mkdir(parents=True, exist_ok=True)
    (root / "uploads" / slug / "manifest.json").write_text(
        json.dumps({"case_id": slug, "slug": slug, "video": str(video), "references": []}),
        encoding="utf-8",
    )

    run_id = run_id or f"20260101T000000_{slug}_abcd1234"
    (root / "work" / f"{slug}__upload").mkdir(parents=True, exist_ok=True)
    (root / "reports" / f"{run_id}.html").write_text("<html></html>", encoding="utf-8")
    (root / "traces" / f"{run_id}.json").write_text("{}", encoding="utf-8")
    (root / f"{slug}__upload_contact.jpg").write_bytes(b"jpg")

    record_id = f"{slug}__upload__{engine}__auto__v41"
    (root / "results" / f"{record_id}.json").write_text(
        json.dumps({
            "score_total": score,
            "final": {"status": status, "label": label, "summary": "摘要"},
            "prompt": prompt if prompt is not None else {"zh": "", "en": ""},
            "media": {"video_url": f"./api/upload_media?slug={slug}&asset=video",
                      "reference_urls": []},
            "llm_trace": {"run_id": run_id},
        }, ensure_ascii=False),
        encoding="utf-8",
    )
    return record_id


def main() -> int:
    root = Path(tempfile.mkdtemp(prefix="aigc_hist_test_"))
    try:
        point_paths_at(root)

        # --- 列表与标题回退 --------------------------------------------------
        with_prompt = write_record(
            root, slug="up_1111111111111111",
            prompt={"zh": "一只猫在窗台上\n第二行不该出现在标题里", "en": "a cat"},
        )
        no_prompt = write_record(root, slug="up_2222222222222222", video_name="生成结果.mp4")
        en_only = write_record(root, slug="up_3333333333333333", prompt={"zh": "", "en": "only en"})

        items = {item["id"]: item for item in history.list_history()}
        check("三条记录全部进列表", len(items) == 3, len(items))
        check("有 Prompt 时标题取首行",
              items[with_prompt]["title"] == "一只猫在窗台上", items[with_prompt]["title"])
        check("无 Prompt 时回退到视频原文件名（剥掉 NN_ 前缀）",
              items[no_prompt]["title"] == "生成结果.mp4", items[no_prompt]["title"])
        check("zh 为空时回退 en", items[en_only]["title"] == "only en", items[en_only]["title"])
        check("倒序：最后写入的排最前",
              list(items)[0] == en_only, list(items)[0])

        # --- 重命名 ----------------------------------------------------------
        history.set_title(no_prompt, "  自定义   名字  ")
        renamed = {i["id"]: i for i in history.list_history()}[no_prompt]
        check("重命名后标题生效且空白归一",
              renamed["title"] == "自定义 名字" and renamed["title_custom"], renamed["title"])
        history.set_title(no_prompt, "")
        reverted = {i["id"]: i for i in history.list_history()}[no_prompt]
        check("清空标题回到自动推导",
              reverted["title"] == "生成结果.mp4" and not reverted["title_custom"],
              reverted["title"])

        # --- 单条读取与路径校验 ----------------------------------------------
        record = history.load_record(with_prompt)
        check("restore 带上 prompt", record["restore"]["prompt"].startswith("一只猫"))
        check("restore 标记可重跑", record["restore"]["rerunnable"] is True)
        check("restore 带上视频大小", record["restore"]["video"]["size"] == 16,
              record["restore"]["video"]["size"])

        for bad in ("../../etc/passwd", "up_1111111111111111", "", "a/b", "x__y"):
            try:
                history.load_record(bad)
                check(f"非法 id 被拒：{bad!r}", False, "居然读到了")
            except (ValueError, FileNotFoundError):
                check(f"非法 id 被拒：{bad!r}", True)

        # --- 删除：同 slug 换通道跑的另一条还在，素材必须留下 ------------------
        mock_twin = write_record(root, slug="up_1111111111111111", engine="mock",
                                 run_id="20260101T000001_up_1111111111111111_beef")
        result = history.delete_record(mock_twin, with_assets=True)
        check("删掉换通道那条时素材被保留",
              result["assets_kept_shared"] and (root / "uploads" / "up_1111111111111111").exists(),
              result)
        check("同 run 的报告与轨迹被删",
              not (root / "reports" / "20260101T000001_up_1111111111111111_beef.html").exists())

        # --- 删除：跨 slug 借用素材（历史重跑）时不能删素材 --------------------
        borrower = write_record(root, slug="up_4444444444444444",
                                borrow_from="up_3333333333333333")
        owner_id = en_only
        result = history.delete_record(owner_id, with_assets=True)
        check("被别的 slug 借用素材时保留 uploads 目录",
              result["assets_kept_shared"]
              and (root / "uploads" / "up_3333333333333333" / "assets").exists(),
              result)
        check("借用方的视频文件仍在",
              (root / "uploads" / "up_3333333333333333" / "assets" / "01_clip.mp4").exists())
        check("借用方记录仍可读", history.load_record(borrower)["restore"]["video"]["available"])

        # --- 删除：无人引用时清干净 ------------------------------------------
        result = history.delete_record(borrower, with_assets=True)
        check("独占记录删除后 uploads 目录消失",
              not result["assets_kept_shared"]
              and not (root / "uploads" / "up_4444444444444444").exists(), result)
        check("work 目录与 contact 图一并清掉",
              not (root / "work" / "up_4444444444444444__upload").exists()
              and not (root / "up_4444444444444444__upload_contact.jpg").exists())
        check("删除后不再出现在列表",
              all(i["id"] != borrower for i in history.list_history()))

        # --- 坏掉的缓存文件不该让整个列表挂掉 --------------------------------
        (root / "results" / "up_5555555555555555__upload__agentk__auto__v41.json").write_text(
            "{ 不是 JSON", encoding="utf-8")
        (root / "results" / "完全不合规的名字.json").write_text("{}", encoding="utf-8")
        try:
            survivors = history.list_history()
            check("坏文件被跳过而非抛错", len(survivors) == 2, len(survivors))
        except Exception as error:  # noqa: BLE001
            check("坏文件被跳过而非抛错", False, error)
    finally:
        shutil.rmtree(root, ignore_errors=True)

    print()
    if FAILURES:
        print(f"FAILED: {len(FAILURES)} 项 — {FAILURES}")
        return 1
    print("ALL PASSED")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
