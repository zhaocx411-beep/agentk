"""探测当前环境的 ``understand_media`` 到底收不收**独立音频文件**。

## 为什么需要它

4a 参考音频对照把音频本体放进 ``mediaUrls``。这个入口是平台 2026-09-01 才放开的，
在那之前 ``mediaUrls`` 只收视频和图片（README「独立音频文件：套一层视频就能进」记了
当时的绕法）。**放开与否是按环境的**，而失败时我们看到的只有一句被 Temporal 外壳包住
的 ``Child Workflow execution failed``，看不出到底是音频入口没开、模型没开放，还是
素材取不到——这三种的处理人完全不同。

所以这个探针直接问平台，把原话打出来，并且用一个**对照组**把变量分开：

* ``.mp3`` / ``.wav`` 独立音频 —— 4a 实际走的那条路
* 带音轨的 ``.mp4`` —— 已知可用的路（视频音轨一直进得去）

mp4 通而独立音频不通 ⇒ 就是音频入口没放开，套一层静态画面即可绕过（绕法已验证，见
``scripts/probe_audio_passthrough.py --wrap-audio``）。
两者都不通 ⇒ 问题不在音频，去查模型开放范围或网关取素材。

素材用 ffmpeg 现场生成（镜像里有），不依赖任何 case 文件。

跑法（容器内）：

    python3 /home/web_server/aigc-eval/bin/audio_entry_probe.py
"""

from __future__ import annotations

import subprocess
import sys
import tempfile
from pathlib import Path

from container_env import announce_identity

from aigc_eval.config import load_agentk_config
from aigc_eval.providers.agentk_transport import transport_for
from aigc_eval.providers.blob_store import shared_blob_store
from aigc_eval.providers.model_call import (
    UNDERSTAND_MEDIA_CAPABILITY,
    raw_text_from_output,
)

# 问一句"听到了什么"。答得出内容说明真收进去了；报错则原样打出平台措辞。
PROBE_PROMPT = "这段素材里有声音吗？如果有，描述它的音高与时长。只回答一句话。"

# 三秒 440Hz 正弦。够短（上传快）、又足够让模型说出"单音"这种可核对的描述。
_FFMPEG_SINE = "sine=frequency=440:duration=3"


def _run(args: list[str]) -> None:
    result = subprocess.run(args, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise RuntimeError(f"ffmpeg 失败：{result.stderr[-400:]}")


def build_assets(workdir: Path) -> list[tuple[str, Path]]:
    """造三件素材：mp3、wav、以及带同一条音轨的 mp4（对照组）。"""
    mp3 = workdir / "probe.mp3"
    wav = workdir / "probe.wav"
    mp4 = workdir / "probe.mp4"
    _run(
        [
            "ffmpeg",
            "-hide_banner",
            "-loglevel",
            "error",
            "-y",
            "-f",
            "lavfi",
            "-i",
            _FFMPEG_SINE,
            "-c:a",
            "libmp3lame",
            str(mp3),
        ]
    )
    _run(
        [
            "ffmpeg",
            "-hide_banner",
            "-loglevel",
            "error",
            "-y",
            "-f",
            "lavfi",
            "-i",
            _FFMPEG_SINE,
            "-c:a",
            "pcm_s16le",
            str(wav),
        ]
    )
    # 黑底静态画面 + 同一条音轨。这一件代表"视频音轨"那条已知可用的路。
    _run(
        [
            "ffmpeg",
            "-hide_banner",
            "-loglevel",
            "error",
            "-y",
            "-f",
            "lavfi",
            "-i",
            "color=c=black:s=320x240:r=10",
            "-f",
            "lavfi",
            "-i",
            _FFMPEG_SINE,
            "-shortest",
            "-c:v",
            "libx264",
            "-pix_fmt",
            "yuv420p",
            "-c:a",
            "aac",
            str(mp4),
        ]
    )
    return [("独立音频 mp3", mp3), ("独立音频 wav", wav), ("带音轨的 mp4（对照组）", mp4)]


def main() -> int:
    announce_identity()
    config = load_agentk_config()
    print(f"  通道     {config.transport}（http 时连 {config.base_url}）")
    print(f"  音频模型 {config.audio_model}")

    store = shared_blob_store()
    print(f"  桶       {store.config.endpoint} / {store.config.bucket}")

    with tempfile.TemporaryDirectory() as tmp:
        assets = build_assets(Path(tmp))
        print("\n[逐件探测]")
        outcomes: dict[str, bool] = {}
        for label, path in assets:
            url = store.put_bytes(path.read_bytes(), suffix=path.suffix)
            size_kb = path.stat().st_size / 1024
            print(f"\n  {label}  ({size_kb:.0f} KB)")
            print(f"    url  {url}")
            try:
                output = transport_for(config).invoke(
                    UNDERSTAND_MEDIA_CAPABILITY,
                    {
                        "model": config.audio_model,
                        "mediaUrls": [url],
                        "prompt": PROBE_PROMPT,
                    },
                    timeout=120,
                )
            except Exception as error:  # noqa: BLE001 - 要的就是平台原话
                print(f"    FAIL {type(error).__name__}: {error}")
                outcomes[label] = False
                continue
            text = raw_text_from_output(UNDERSTAND_MEDIA_CAPABILITY, output or {})
            print(f"    OK   {text[:200]!r}")
            outcomes[label] = True

    print("\n[结论]")
    standalone = [ok for label, ok in outcomes.items() if "独立音频" in label]
    control = outcomes.get("带音轨的 mp4（对照组）", False)
    if any(standalone):
        print("  这个环境的 understand_media 收独立音频，4a 的失败不是入口问题。")
        print("  去查模型开放范围、素材大小、或那次调用具体的平台报错。")
        return 0
    if control:
        # 这正是 2026-09-01 之前的状态。绕法已验证可行，只是当时决定不用。
        print("  独立音频进不去，但视频音轨可以 —— 这个环境的音频入口没放开。")
        print("  4a 需要给音频套一层静态画面再送（scripts/probe_audio_passthrough.py")
        print("  --wrap-audio 验证过这条路可行）。")
        return 1
    print("  三件都失败，问题不在音频入口：先看上面的报错，多半是模型没开放")
    print("  或网关取不到这个桶。跑 smoke_check.py 可以把后两者分开。")
    return 1


if __name__ == "__main__":
    sys.exit(main())
