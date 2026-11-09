"""钉住 prepare 阶段对 ``referenceAudioUrls`` 与 ``mode`` 两个入参的处理。

这两个字段是为了让 studio 能整体迁到 capability 上才加的，但它们各自修的都是
契约本身的洞：

- 没有 ``referenceAudioUrls``，带配音的用例根本没法把参考音频送进来，而
  ``audio_quality`` 是八个评测维度之一；
- 没有 ``mode``，就只能靠素材自动判定，压不出"强制按某个模式判"的行为。

全程离线：``resolve_media`` 对绝对本地路径是拷贝而非下载，所以不碰网络、不花模型钱。
真正要防的回归有两个——音频被当成参考图（后缀兜底值曾经写死 ``.jpg``），
以及 ``mode`` 传了却没往下透（曾经写死 ``None``）。
"""

from __future__ import annotations

import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent
sys.path.insert(0, str(REPO / "worker"))
sys.path.insert(0, str(REPO / "src"))

from abi import VideoJudgeInput
from stage_activities import _do_prepare

PASS, FAIL = 0, 0


def check(ok: bool, label: str) -> None:
    global PASS, FAIL
    if ok:
        PASS += 1
        print(f"PASS  {label}")
    else:
        FAIL += 1
        print(f"FAIL  {label}")


def _sample_video() -> Path:
    """随便拿一条本机已有的样例视频；没有就跳过整个测试。"""
    for path in sorted((REPO / "runtime" / "uploads").glob("*/assets/*.mp4")):
        return path
    raise SystemExit("SKIP  本机没有样例视频（runtime/uploads/*/assets/*.mp4），跳过")


def _make_audio(dest: Path) -> Path:
    """本地合成一段静音 mp3，不下载任何素材。"""
    subprocess.run(
        [
            "ffmpeg", "-y", "-loglevel", "error",
            "-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono",
            "-t", "2", str(dest),
        ],
        check=True,
    )
    return dest


def _prepare(video: Path, **overrides: object) -> dict:
    args = VideoJudgeInput(
        videoUrl=str(video),
        prompt="一个人在说话",
        **overrides,  # type: ignore[arg-type]
    )
    return _do_prepare(args, lambda _event: None, run_key="test_prepare")


def _references(state: dict) -> list[dict]:
    return list((state.get("caseFiles") or {}).get("reference_assets") or [])


def main() -> None:
    video = _sample_video()
    tmp = Path(tempfile.mkdtemp())
    audio = _make_audio(tmp / "ref.mp3")

    print("[1] 参考音频进得来，且认成音频而不是参考图")
    state = _prepare(video, referenceAudioUrls=[str(audio)])
    refs = _references(state)
    kinds = [r.get("kind") for r in refs]
    check(len(refs) == 1, f"参考素材恰好一条（实际 {len(refs)}）")
    check(kinds == ["audio"], f"kind 是 audio（实际 {kinds}）")
    check(
        all(Path(str(r.get("path"))).suffix == ".mp3" for r in refs),
        "落地后缀仍是 .mp3（兜底值若写死 .jpg，这里会变成 .jpg）",
    )

    print("\n[2] 纯音频参考不改变生成模式")
    check(state.get("resolvedMode") == "T2V", f"仍是 T2V（实际 {state.get('resolvedMode')}）")

    print("\n[3] mode 覆盖能透到底")
    forced = _prepare(video, mode="V2V")
    check(forced.get("resolvedMode") == "V2V", f"强制 V2V 生效（实际 {forced.get('resolvedMode')}）")

    print("\n[4] 不给 mode 时仍按素材自动判定")
    auto = _prepare(video)
    check(auto.get("resolvedMode") == "T2V", f"自动判定 T2V（实际 {auto.get('resolvedMode')}）")

    print("\n[5] mode 参与缓存寻址，否则强制模式会命中另一模式的旧报告")
    check(
        forced.get("cachePath") != auto.get("cachePath"),
        "不同 mode 的缓存路径不同",
    )

    print()
    if FAIL:
        raise SystemExit(f"{FAIL} FAILED / {PASS} passed")
    print(f"ALL PASSED ({PASS})")


main()
