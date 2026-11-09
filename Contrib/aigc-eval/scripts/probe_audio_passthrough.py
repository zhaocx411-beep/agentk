"""探针：`agentk.understand_media` 收下一段 mp4 时，视频的音轨有没有进到模型里。

## 为什么需要这个脚本

`understand_media` 的 schema 只收「视频/图片」URL，音频文件送上去会被拒。项目因此
一直假定"音频进不了模型"，把听感类子判据一律记 `not_observable`。但 mp4 本身是带
音轨的——如果平台只是把视频 URL 透传给 Gemini（原生支持带声音的视频），那音轨其实
已经进去了，那个假定就是错的。这个脚本用可判定的实验回答这件事。

结论（2026-09-01，staging，gemini-3.1-pro-preview）：**音轨进了模型**。见 README
「音频听感缺口」一节。这个脚本留在库里是为了让结论可复现——平台行为会变，改口径
之前应该先重跑它。

## 实验怎么设计的

要点是**视听解耦**：让答案只存在于音轨里，画面提供不了任何线索。

* 画面用 ffmpeg 的 testsrc2 测试图案——没有人、没有口型、没有字幕，读唇和 OCR
  都拿不到东西。用真人说话视频反而更弱：模型可以从口型猜内容。
* 音轨由本脚本合成，真值因此是已知且精确的：一句与画面无关的中文台词，加上若干
  声 1000Hz 纯音调，出现在指定秒数。
* 提示词里**不注入任何转写文本**，也绕开整条 judge 流水线——流水线会送进
  transcribe_audio 的结果，那样问什么都答得上来，测不出音轨本身有没有被透传。
* 提示词明确允许模型说"听不到"，并声明猜错比承认听不到更糟，避免它顺着问题编。

## 实测结论：人声可靠，非人声事件的计数与定位不可靠

三轮（两轮真人画面 + 一轮测试图案，台词相同）：

    轮次  画面        台词复述      哔声真值            哔声回答
    1     真人说话    逐字命中 6/6  3 声 @ 3/6/9 *      2 声 @ 6, 9
    2     真人说话    逐字命中 6/6  3 声 @ 7/9.5/12     3 声 @ 7, 9, 11
    3     测试图案    逐字命中 6/6  3 声 @ 7/9.5/12     4 声 @ 7, 9, 11, 13

    * 第 1 轮的 3.0s 那声哔落在台词区间（1.0~5.36s）内，被人声掩蔽。

台词 3/3 逐字正确，其中「香蕉**牌**」这个字 transcribe_audio 反而听错成「香蕉**排**」
——提示词里没有任何转写文本，所以这条只能来自模型自己听。这是"音轨进了模型"最硬的
证据。

但哔声的**数量与时间都不可靠**：数量三轮分别答 2 / 3 / 4（真值都是 3），时间偏差约
1 秒。真值经 ffmpeg silencedetect 客观核对过（有声区间 1.01~5.31 / 7.00~7.30 /
9.50~9.80 / 12.00~12.30），确认是模型答错而非探针做错。

所以能下的结论是"音频进得去、人声内容可判"，**不是**"听感类判据现在都能精确判"。
逐帧级口型对齐、爆音计数这类要求精确定位的判据，需要各自单独验证。

## 第二个模式：--wrap-audio

`mediaUrls` 不收 .mp3/.wav，所以独立的参考音频只能转写。但既然视频音轨进得去，给音频
套一层静态画面就能绕过 schema 限制。`--wrap-audio <文件>` 验的是这条：把指定音频包进
黑底视频送进去，问一些转写答不出来的问题（语调、配乐、音质），看拿不拿得回听感。

**结论：拿得回。** 用真实参考音频（229KB / 14.2s）试，模型认出了女声播音腔与 TTS 合成
特征、判定无背景音乐是纯人声干音、并对音质给出了判断。它的听辨还比 whisper 准：转写把
「银发诵初心，新声贺华诞」听成「銀髮宋初心新生賀華旦」，模型听对了。

**但音质结论跨轮不稳定**：同一个文件两轮，一轮说"音质较差、明显的数字压缩失真、金属
音色"，另一轮说"非常清晰干净，没有明显底噪、爆音、削波或压缩失真"。语调那项两轮都指向
TTS 合成（措辞从"极其明显"弱化到"有些像"），方向一致但强度飘。所以 audio_signal_quality
这类判据在接进链路前需要想清楚怎么应对这种抖动。

## 第三个模式：--raw-audio

把音频文件**直送** mediaUrls，用来随时复核平台 schema 有没有放开。三种结果必须区分，
第二种最危险：

1. **报错被拒** —— 旧行为（"cannot determine a supported media type"），至少是响亮的失败；
2. **收下了但模型没听到** —— 2026-09-01 实测就是这种：调用成功返回，模型明确说"未接收到
   音频文件或音频流，仅收到文本提示"。音频被**静默丢掉**，没有任何报错。同一时段的对照
   组（同一个文件套视频送）能完整听到，所以问题确定在直送这条路上；
3. 收下且听到了 —— schema 真放开了。

第二种比第一种更坏：只看"调用成功"就把参考音频接进 mediaUrls，线上会安静地少判一个
维度，且没有任何信号提示。所以这个模式的判定读的是模型自己答的 can_hear_audio，
不是有没有抛异常。

用法（需要 KWS_SERVICE_NAME 与对象存储环境变量，见 README「本地怎么跑」）：

    uv run --no-sync python scripts/probe_audio_passthrough.py
    uv run --no-sync python scripts/probe_audio_passthrough.py --wrap-audio ref.mp3
"""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

from aigc_eval.config import load_agentk_config
from aigc_eval.providers.agentk_transport import shared_transport, transport_for
from aigc_eval.providers.blob_store import shared_blob_store
from aigc_eval.providers.model_call import UNDERSTAND_MEDIA_CAPABILITY

# 台词刻意选一句与任何画面都无关、且不太可能被模型"合理猜测"出来的胡话。
TRUTH_PHRASE = "香蕉牌拖拉机第七号，蓝色的月亮在唱歌"
_PHRASE_KEYWORDS = ("香蕉", "拖拉机", "第七号", "蓝色", "月亮", "唱歌")

# 默认哔声位置全部排在台词（1.0s 起、约 4.4s 长）之后，避开掩蔽。
DEFAULT_BEEPS = (7.0, 9.5, 12.0)

VIDEO_SECONDS = 14.4
SPEECH_START_SECONDS = 1.0

_PROMPT = """你收到一段视频。请**只依据音频轨道**回答下面的问题，不要依据画面推测。

1. 你能听到这段视频的声音吗？
2. 如果能听到，请逐字写出你听到的人声说话内容。
3. 音轨里有几声电子"哔"声（单一频率的纯音调）？分别出现在第几秒？

如果你收到的素材里没有音频、或者你无法处理音频，请如实说明，**不要猜测、不要编造**。
猜错比承认听不到更糟。

只返回如下 JSON，不要其它文字：
{"can_hear_audio": true/false, "speech_text": "...", "beep_count": 0,
 "beep_times_seconds": [], "note": "..."}"""


class ProbeUnavailable(RuntimeError):
    """探针没法构造（缺 ffmpeg / say）。这不是实验结论，只是环境不满足。"""


def _run(command: list[str]) -> None:
    result = subprocess.run(command, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise ProbeUnavailable(
            f"命令失败：{' '.join(command[:3])}…\n{result.stderr.strip()[:500]}"
        )


def synth_speech(dest: Path) -> Path | None:
    """用 macOS 的 say 合成台词；没有 say 就返回 None，退化成只测哔声。

    只测哔声仍然能回答"音轨进没进模型"，只是少了"逐字复述"这个最直观的证据。
    """
    if shutil.which("say") is None:
        return None
    # 中文发音人缺失时用默认发音人：我们比对的是词，读音标准不标准无关。
    voices = subprocess.run(
        ["say", "-v", "?"], capture_output=True, text=True, check=False
    ).stdout
    args = ["say", "-o", str(dest)]
    if "Tingting" in voices:
        args = ["say", "-v", "Tingting", "-o", str(dest)]
    _run([*args, TRUTH_PHRASE])
    return dest if dest.exists() else None


def build_probe_video(work: Path, beeps: tuple[float, ...]) -> tuple[Path, bool]:
    """合成探针视频，返回 (路径, 是否含台词)。

    画面是 testsrc2 测试图案：视觉上不含任何可用于推断音频的信息，这样模型答对
    就只能是听到了。
    """
    if shutil.which("ffmpeg") is None:
        raise ProbeUnavailable("需要 ffmpeg（brew install ffmpeg）")

    speech = synth_speech(work / "speech.aiff")
    inputs: list[str] = []
    filters: list[str] = []
    labels: list[str] = []

    if speech is not None:
        inputs += ["-i", str(speech)]
        delay = int(SPEECH_START_SECONDS * 1000)
        filters.append(
            f"[{len(labels)}:a]aformat=channel_layouts=stereo,"
            f"adelay={delay}|{delay}[sp]"
        )
        labels.append("[sp]")

    for index, at in enumerate(beeps):
        inputs += ["-f", "lavfi", "-i", "sine=f=1000:d=0.30"]
        delay = int(at * 1000)
        stream = len(labels)
        filters.append(
            f"[{stream}:a]aformat=channel_layouts=stereo,"
            f"adelay={delay}|{delay},volume=0.6[b{index}]"
        )
        labels.append(f"[b{index}]")

    # apad 把音轨补到整段时长。不补的话 amix 只出到最后一声哔，随后 -shortest
    # 会把画面一起截短，最后那声哔正好压在视频结尾边界上，模型容易漏。
    filters.append(
        f"{''.join(labels)}amix=inputs={len(labels)}:duration=longest:normalize=0,apad[mix]"
    )
    audio = work / "probe_audio.wav"
    _run(
        ["ffmpeg", "-y", "-v", "error", *inputs,
         "-filter_complex", "; ".join(filters),
         "-map", "[mix]", "-t", str(VIDEO_SECONDS), "-ar", "44100", str(audio)]
    )

    video = work / "probe.mp4"
    _run(
        ["ffmpeg", "-y", "-v", "error",
         "-f", "lavfi", "-i",
         f"testsrc2=size=640x360:rate=25:duration={VIDEO_SECONDS}",
         "-i", str(audio),
         "-map", "0:v:0", "-map", "1:a:0",
         "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "128k",
         "-shortest", str(video)]
    )
    return video, speech is not None


# 刻意问转写给不了的东西：转写只有文字，说不出语速、语调、音质、配乐。
_AUDIO_QUESTIONS = """1. 你能听到声音吗？听到的人声说了什么？（逐字）
2. 说话人的语调、语速、情绪听起来如何？是播音腔还是日常口语？
3. 有没有背景音乐或音效？如果有，描述风格。
4. 音质如何？有没有底噪、爆音、削波、压缩失真？

听不到就如实说"听不到"，不要编造。只返回 JSON：
{"can_hear_audio": true/false, "speech_text": "...", "delivery": "...",
 "background_music": "...", "audio_quality": "...", "note": "..."}"""

_WRAP_PROMPT = (
    "这段视频的画面是无意义的纯黑底，**请只依据音频轨道**回答：\n\n" + _AUDIO_QUESTIONS
)

_RAW_PROMPT = "这是一段音频素材，请回答：\n\n" + _AUDIO_QUESTIONS


def wrap_audio_as_video(audio: Path, work: Path) -> Path:
    """把音频套一层纯黑静态画面，变成 understand_media 收得下的 mp4。

    画面刻意无意义：这样任何听感结论都只可能来自音轨，不会是看图看出来的。
    """
    if shutil.which("ffmpeg") is None:
        raise ProbeUnavailable("需要 ffmpeg（brew install ffmpeg）")
    probe = subprocess.run(
        ["ffprobe", "-v", "error", "-show_entries", "format=duration",
         "-of", "csv=p=0", str(audio)],
        capture_output=True, text=True, check=False,
    )
    duration = probe.stdout.strip()
    if not duration:
        raise ProbeUnavailable(f"读不出音频时长：{audio}")
    dest = work / "wrapped.mp4"
    _run(
        ["ffmpeg", "-y", "-v", "error",
         "-f", "lavfi", "-i", f"color=c=black:s=320x240:r=10:d={duration}",
         "-i", str(audio),
         "-map", "0:v:0", "-map", "1:a:0",
         "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
         "-shortest", str(dest)]
    )
    return dest


def run_raw_mode(audio: Path, config: object, timeout: float) -> int:
    """--raw-audio：把音频文件**直送** mediaUrls，不套视频。

    存在的意义是随时复核平台 schema 有没有放开。历史上这条路是被拒的
    （"cannot determine a supported media type (declared audio/mpeg)"），而且平台
    把这个错标成可重试，会在服务端一路重试到 capability 超时（实测 315~346s）才回
    错误——所以这里默认给足超时，别把"被拒"误读成"网络问题"。

    **有四种结果，必须区分开**，中间两种都会被"调用成功"掩盖：

    1. 报错被拒——最早的行为（"cannot determine a supported media type"），至少是
       响亮的失败；
    2. 收下了，音频被静默丢掉——模型说"未接收到音频"，且拿不到任何台词；
    3. **收下了，但在服务端被转写成文字再注入提示词**——2026-09-01 平台放开音频入口
       后实测就是这一种。调用成功，模型能给出准确台词，却明说"仅接收到了文本输入
       （包含时间戳和文字内容），未接收到实际的音频文件或音频信号"。
       判别方法：送一段**带非语音声音**的音频（纯音调、音效）。转写里不会有它们，
       所以模型答得出台词、答不出音效，就说明拿到的是文字而不是声音；
    4. 收下且真的听到了——原生音频入口，听感类判据可以真判。

    第 3 种最容易误判成"支持了"：接口通、内容对、还有时间戳，看起来完全正常，但
    音色、语调、音质、配乐一概拿不到，而报告不会有任何异常。
    所以判定既不能只看有没有抛异常，也不能只看台词对不对，必须看模型自己说的
    can_hear_audio，并结合"有没有答出非语音的声音"。

    **务必连跑十次以上再下结论。** 2026-09-01 平台放开音频入口当天，28 次直送里 15 次
    走第 4 种、13 次退回第 3 种，同一个文件在两种行为间来回跳，且发版后一小时比例没
    改善。结果还成簇出现（连续几次原生、再连续几次文字），像实例级粘连，所以连跑
    三五次也可能整簇落在同一种行为上、给出偏向性的结论。
    只跑一次一定会得出与事实相反的结论——我们先后得出过两次相反的结论，就是这么来的。
    """
    url = shared_blob_store().put_file(audio)
    print(f"已直接上传音频（未套视频）：{url}")
    print(f"送进 mediaUrls 的就是这个 {audio.suffix} 地址\n")
    try:
        output = transport_for(config).invoke(  # type: ignore[attr-defined]
            UNDERSTAND_MEDIA_CAPABILITY,
            {"model": config.model, "mediaUrls": [url], "prompt": _RAW_PROMPT},  # type: ignore[attr-defined]
            timeout=timeout,
        )
    except Exception as error:  # noqa: BLE001 - 被拒是这个模式的合法结果之一
        print("【understand_media 拒收】")
        print(f"  {error}")
        print("\n  → schema 仍未放开音频直送；独立音频要么转写，要么套一层视频")
        return 1
    raw = str(output.get("text") or output) if isinstance(output, dict) else str(output)
    print("【understand_media 没报错，返回如下】")
    print(raw)

    # 关键：调用成功 ≠ 音频到了模型。读模型自己的答复来区分四种结果。
    try:
        start, end = raw.index("{"), raw.rindex("}")
        answer = json.loads(raw[start : end + 1])
    except (ValueError, json.JSONDecodeError):
        print("\n  → 回答不是预期 JSON，无法判定，请人工读上面的原文")
        return 1

    if answer.get("can_hear_audio"):
        print("\n  → 原生音频入口通了：独立音频不必再套视频，听感类判据可以真判")
        return 0

    # 模型说听不到，但台词却是对的 → 它拿到的是服务端转写出来的文字。
    # 这里不做语义比对，只看台词字段是否有实质内容：本函数的提示词里没有任何
    # 转写文本，所以凡是给得出台词，就只能来自平台注入。
    speech = str(answer.get("speech_text") or "").strip()
    got_text = bool(speech) and not any(
        mark in speech for mark in ("听不到", "无法判断", "未接收", "没有")
    )
    if got_text:
        print(
            "\n  → **看起来支持了，其实没有**：音频在服务端被转写成文字再注入提示词，\n"
            "     模型拿到的是文本而不是声音（它自己也这么说）。台词对、还带时间戳，\n"
            "     所以接口层面完全看不出问题，但音色/语调/音质/配乐一概拿不到。\n"
            "     要拿听感仍然只能 --wrap-audio（套视频），或推平台开原生音频入口。"
        )
        return 1
    print(
        "\n  → 音频被静默丢掉：调用成功，但模型既没听到也没拿到台词。\n"
        "     比报错更坏——接进链路会安静地少判一个维度。"
    )
    return 1


def run_wrap_mode(audio: Path, work: Path, config: object, timeout: float) -> int:
    """--wrap-audio：验证独立音频包进视频后能否被真的听到。

    这里曾经先打印一遍 transcribe_audio 的结果作对照。那条通路 2026-09-03 已下线
    （prod 上挂着不返回，见 agentk_audio 的模块说明），对照随之删掉。
    """
    wrapped = wrap_audio_as_video(audio, work)
    print(f"已把 {audio.name}（{audio.stat().st_size / 1e3:.0f}KB）套进黑底视频"
          f"（{wrapped.stat().st_size / 1e3:.0f}KB）")
    url = shared_blob_store().put_file(wrapped)
    print(f"已上传：{url}\n")

    output = transport_for(config).invoke(  # type: ignore[attr-defined]
        UNDERSTAND_MEDIA_CAPABILITY,
        {"model": config.model, "mediaUrls": [url], "prompt": _WRAP_PROMPT},  # type: ignore[attr-defined]
        timeout=timeout,
    )
    raw = str(output.get("text") or output) if isinstance(output, dict) else str(output)
    print("【understand_media（音频包进视频后）】")
    print(raw)
    print(
        "\n  → 若上面答出了语调/配乐/音质，说明包装这条路不只是能送进去，"
        "而是真的拿回了听感"
    )
    return 0


def ask_model(url: str, model: str, endpoint: str, timeout: float) -> str:
    output = shared_transport(endpoint).invoke(
        UNDERSTAND_MEDIA_CAPABILITY,
        {"model": model, "mediaUrls": [url], "prompt": _PROMPT},
        timeout=timeout,
    )
    if isinstance(output, dict):
        return str(output.get("text") or output.get("result") or output)
    return str(output)


def verdict(raw: str, beeps: tuple[float, ...], with_speech: bool) -> bool:
    """打印判定并返回"音轨进了模型"是否成立。

    两个独立证据任一成立即判成立：台词命中 ≥2 个关键词，或哔声数量答对。
    两者都不可能靠看画面得到，也不太可能猜中。
    """
    try:
        start, end = raw.index("{"), raw.rindex("}")
        answer = json.loads(raw[start : end + 1])
    except (ValueError, json.JSONDecodeError):
        print("回答不是 JSON，按原文人工判读", file=sys.stderr)
        return False

    speech = str(answer.get("speech_text") or "")
    hits = [word for word in _PHRASE_KEYWORDS if word in speech]
    count_ok = answer.get("beep_count") == len(beeps)

    print("\n【判定】")
    print(f"  自称能听到音频：{answer.get('can_hear_audio')}")
    if with_speech:
        print(f"  台词命中关键词：{hits or '无'}（真值「{TRUTH_PHRASE}」）")
    print(f"  哔声数量：答 {answer.get('beep_count')} / 真值 {len(beeps)}")
    print(f"  哔声时间：答 {answer.get('beep_times_seconds')} / 真值 {list(beeps)}")

    passed = len(hits) >= 2 or count_ok
    print(
        "\n  → 音轨**进了**模型：听感类子判据可以真判，不该一律记 not_observable"
        if passed
        else "\n  → 音轨**没进**模型：音频判据只能一律记 not_observable（转写那条退路已下线）"
    )
    return passed


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--beeps", default=",".join(str(b) for b in DEFAULT_BEEPS),
        help="哔声出现的秒数，逗号分隔。改动时注意避开台词区间（1.0~5.4s）",
    )
    parser.add_argument(
        "--wrap-audio", metavar="FILE", default="",
        help="改跑另一个模式：把这个独立音频文件包进黑底视频送进 understand_media，"
             "验证能否绕过 mediaUrls 不收音频的限制并真的听到",
    )
    parser.add_argument(
        "--raw-audio", metavar="FILE", default="",
        help="把音频文件直送 mediaUrls（不套视频），用于复核平台 schema 是否已放开音频",
    )
    parser.add_argument("--timeout", type=float, default=300.0)
    parser.add_argument(
        "--keep", metavar="DIR", default="",
        help="保留合成的探针视频到指定目录，便于人工试听核对真值",
    )
    options = parser.parse_args()
    beeps = tuple(float(x) for x in options.beeps.split(",") if x.strip())

    config = load_agentk_config()
    if not config.service_name:
        print("需要 export KWS_SERVICE_NAME（见 README）", file=sys.stderr)
        return 2

    work = Path(options.keep) if options.keep else Path(tempfile.mkdtemp(prefix="audio_probe_"))
    work.mkdir(parents=True, exist_ok=True)

    if options.raw_audio:
        audio = Path(options.raw_audio)
        if not audio.exists():
            print(f"音频文件不存在：{audio}", file=sys.stderr)
            return 2
        print(f"模型：{config.model}\n")
        return run_raw_mode(audio, config, options.timeout)

    if options.wrap_audio:
        audio = Path(options.wrap_audio)
        if not audio.exists():
            print(f"音频文件不存在：{audio}", file=sys.stderr)
            return 2
        print(f"模型：{config.model}\n")
        try:
            return run_wrap_mode(audio, work, config, options.timeout)
        except ProbeUnavailable as error:
            print(f"探针无法构造：{error}", file=sys.stderr)
            return 2

    try:
        video, with_speech = build_probe_video(work, beeps)
    except ProbeUnavailable as error:
        print(f"探针无法构造：{error}", file=sys.stderr)
        return 2

    print(f"模型：{config.model}")
    print(f"真值：台词={'「' + TRUTH_PHRASE + '」' if with_speech else '（无 say，跳过台词）'}")
    print(f"      哔声 {len(beeps)} 声 @ {list(beeps)}")
    print("画面：testsrc2 测试图案（视觉上无从推断音频）\n")

    url = shared_blob_store().put_file(video)
    print(f"探针已上传：{url}\n")
    raw = ask_model(url, config.model, config.base_url, options.timeout)
    print("【understand_media 原始回答】")
    print(raw)

    passed = verdict(raw, beeps, with_speech)
    if options.keep:
        print(f"\n探针视频保留在 {video}")
    return 0 if passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
