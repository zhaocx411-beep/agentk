"""部署验收：把质检链路依赖的每个外部条件在目标环境里跑一遍。

## 它替代不了什么

这里验的是**外部依赖**，不是判定质量。全绿只说明"每一件我们依赖的东西在这个环境都
能用"，不说明报告结论对。结论对不对要靠一次真实质检（见文件末尾的下一步提示）。

## 为什么把这些凑在一起

质检失败时，症状几乎总是同一句被包了好几层的报错，而根因可能在八个完全不同的地方：
身份、传输通道、对象存储上传、网关取素材、多模态模型、文本模型、音频入口、ffmpeg、
runtime 写权限。逐个手工试要半小时且容易漏，漏掉的那个就是下次线上失败的原因。

每一项都设计成**失败时能指到人**：报错文案按类型翻成"该找谁"，而不是把平台原话原样
丢出来让人猜。

## 已经踩过并被这个脚本挡住的坑

* 凭证只注入到服务进程，WebConsole 的 shell 和 PID 1 都没有 → 见 container_env。
* 生产不开放 HTTP 固定网关，只有 KESS gRPC 通，而两者失败时报错一模一样。
* 独立音频进不进 mediaUrls 是按环境开的，4a 依赖它，失败时只显示
  ``Child Workflow execution failed``。
* 构建期会以 root 建出 runtime 子目录，容器跑哪个 uid 由平台定 → 写权限要单独验。

跑法（容器内）：

    python3 /home/web_server/aigc-eval/bin/verify_deployment.py

全绿退出码 0，任一项失败退出码 1。
"""

from __future__ import annotations

import json
import os
import shutil
import struct
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request
import zlib
from pathlib import Path

from container_env import announce_identity

# 纯色图问"什么颜色"、正弦音问"什么声音"：都必须是不看/不听就答不出的信息，
# 才能把"网关真取到了素材"和"网关只是收下了请求"分开。
PROBE_COLOR = (255, 0, 0)
COLOR_WORDS = ("红", "red", "赤")
COLOR_PROMPT = "这张图片是什么颜色？只回答颜色名，不要其他内容。"
AUDIO_PROMPT = "这段素材里有声音吗？如果有，描述它的音高与时长。只回答一句话。"
SINE = "sine=frequency=440:duration=3"

# SKIP 与 FAIL 必须分开：容器专属的几项在本地无从验证，把它们记成 FAIL 会让
# 本地跑永远是红的，红久了就没人看了。只有 FAIL 影响退出码。
results: list[tuple[str, str, str]] = []


def record(name: str, status: bool | str, note: str = "") -> bool:
    state = status if isinstance(status, str) else ("PASS" if status else "FAIL")
    results.append((name, state, note))
    print(f"  {state}  {name}" + (f" — {note}" if note else ""))
    return state == "PASS"


def is_sync_ceiling(error: Exception) -> bool:
    """区分「环境不通」和「这个脚本的调用方式撞了同步上限」。

    脚本用的是同步直调，受网关 300s 上限约束；流水线走 Temporal 工作流里的
    invoke_capability，不受这个限制。所以撞上 300s 只能说明模型当前排队慢，
    不能推出流水线不通——判成 FAIL 会把人引去查根本没坏的东西。
    """
    text = str(error)
    return "同步直调上限" in text or "被网关切断" in text


def explain(error: Exception) -> str:
    """把平台错误翻成"该找谁"。每类的处理人完全不同。"""
    text = str(error)
    lowered = text.lower()
    # 超时必须第一个判：它的文案里带着能力名 understand_media，"media" 一撞就会
    # 被误判成素材问题，而两者的处理方向正好相反（等更久 vs 换素材形态）。
    if is_sync_ceiling(error):
        return (
            "撞上网关 300s 同步上限。流水线不走这条同步路径，所以这一项不判环境不通，"
            f"要靠真实质检确认。原文：{text[:160]}"
        )
    if "超时" in text or "timeout" in lowered or "timed out" in lowered:
        return f"等待超时，不代表调用被拒——可能是模型排队。原文：{text[:200]}"
    if any(word in lowered for word in ("unauthorized", "permission", "denied")):
        return (
            f"身份被拒。服务名 {os.environ.get('KWS_SERVICE_NAME', '?')} 需要在这个环境"
            f"登记 KSN 并绑 Service/App，找 AgentK 维护方。原文：{text[:300]}"
        )
    if any(word in lowered for word in ("not available", "not found", "no such model")):
        return f"模型或能力在这个环境没开放。原文：{text[:300]}"
    if any(word in lowered for word in ("unsupported", "invalid url", "fetch", "download")):
        return f"素材被拒或取不到（类型不支持 / 网关到不了这个桶）。原文：{text[:300]}"
    return text[:400]


def solid_png(rgb: tuple[int, int, int], size: int = 64) -> bytes:
    """手写纯色 PNG，不依赖 ffmpeg——少一个外部依赖就少一种失败模式。"""
    raw = (b"\x00" + bytes(rgb) * size) * size

    def chunk(tag: bytes, data: bytes) -> bytes:
        body = tag + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)

    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, 2, 0, 0, 0))
        + chunk(b"IDAT", zlib.compress(raw))
        + chunk(b"IEND", b"")
    )


def ffmpeg(args: list[str]) -> None:
    done = subprocess.run(
        ["ffmpeg", "-hide_banner", "-loglevel", "error", "-y", *args],
        capture_output=True,
        text=True,
        check=False,
    )
    if done.returncode != 0:
        raise RuntimeError(done.stderr[-300:])


# --------------------------------------------------------------------------- #
# 各项检查。每个函数只管一件事，失败不影响后面的项：一次跑完拿到全景，
# 比修一个跑一遍快得多。
# --------------------------------------------------------------------------- #


def check_ffmpeg() -> None:
    print("\n[1] ffmpeg（预检、抽帧、证据帧、音轨提取全靠它）")
    for tool in ("ffmpeg", "ffprobe"):
        path = shutil.which(tool)
        if not path:
            record(f"{tool} 在位", False, "镜像里没装，抽帧与预检会全线失败")
            continue
        done = subprocess.run([tool, "-hide_banner", "-version"], capture_output=True, text=True, check=False)
        first = done.stdout.splitlines()[0] if done.stdout else ""
        record(f"{tool} 在位", done.returncode == 0, first[:60])


def check_runtime_writable() -> None:
    print("\n[2] runtime 写权限（报告、抽帧中间产物都往这里写）")
    # 构建期以 root 建出这些目录，而容器跑哪个 uid 由平台定。不验这一下，
    # 表现是服务起得来、质检跑到写报告那步才 Permission denied。
    from aigc_eval.paths import REPORT_ROOT, TRACE_ROOT, WORK_ROOT

    for label, directory in (
        ("work", WORK_ROOT),
        ("reports", REPORT_ROOT),
        ("traces", TRACE_ROOT),
    ):
        try:
            directory.mkdir(parents=True, exist_ok=True)
            probe = directory / ".verify_probe"
            probe.write_text("ok")
            probe.unlink()
        except OSError as error:
            record(f"runtime/{label} 可写", False, str(error))
        else:
            record(f"runtime/{label} 可写", True)


def in_container() -> bool:
    """容器里才有 kcsize 分配的端口；本地跑时端口与双进程两项无从验证。"""
    return bool(os.environ.get("VIDEO_JUDGE_PORT") or os.environ.get("AUTO_PORT0"))


def check_health() -> None:
    print("\n[3] 健康检查端点（平台按字节精确比对返回体）")
    port = os.environ.get("VIDEO_JUDGE_PORT") or os.environ.get("AUTO_PORT0") or ""
    if not port:
        record("/healthz 正确应答", "SKIP", "本地环境没有平台分配的端口")
        return
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{port}/healthz", timeout=10) as response:
            body = response.read().decode()
            code = response.status
    except (urllib.error.URLError, TimeoutError, OSError) as error:
        record(f"/healthz 可达（端口 {port}）", False, str(error))
        return
    record(f"/healthz 返回 200（端口 {port}）", code == 200, f"code={code}")
    # 期望值必须逐字节相等：平台做的是字符串比对，多一个空格就不过。
    record('/healthz 体是紧凑 {"status":"ok"}', body == '{"status":"ok"}', repr(body))


def check_worker_alive() -> None:
    print("\n[4] 双进程（worker 一停，能力就从目录消失，而健康检查仍绿）")
    if not in_container() or not Path("/proc").is_dir():
        record("worker 与 HTTP 都在跑", "SKIP", "本地环境，双进程由 start.sh 才拉起")
        return
    running = {"worker": False, "http": False}
    for pid in (p for p in os.listdir("/proc") if p.isdigit()):
        try:
            cmdline = Path(f"/proc/{pid}/cmdline").read_text().replace("\0", " ")
        except OSError:
            continue
        if "worker.py" in cmdline:
            running["worker"] = True
        elif "aigc_eval" in cmdline and "-m" in cmdline:
            running["http"] = True
    record("temporal worker 进程在跑", running["worker"])
    record("http 进程在跑", running["http"])


def check_identity(config: object) -> None:
    print("\n[5] 身份与传输通道")
    import asyncio

    from agentk.api.v1.service_pb import WhoAmIRequest
    from agentk.client import Options
    from agentk.client.grpc import create_client as create_grpc
    from agentk.client.http import create_client as create_http

    chosen = config.transport  # type: ignore[attr-defined]
    print(f"  配置选定：{chosen}（http 时连 {config.base_url}）")  # type: ignore[attr-defined]
    ok_by_kind: dict[str, bool] = {}
    for kind, factory, options in (
        ("http", create_http, Options(endpoint=config.base_url)),  # type: ignore[attr-defined]
        # grpc 必须留空 endpoint 才会触发 KESS 发现 klingai-agentk-api。
        ("grpc", create_grpc, Options()),
    ):
        try:
            identity = asyncio.run(factory(options).who_am_i(WhoAmIRequest()))
        except Exception as error:  # noqa: BLE001
            ok_by_kind[kind] = False
            print(f"    {kind:5} FAIL {type(error).__name__}: {str(error)[:120]}")
        else:
            ok_by_kind[kind] = True
            print(f"    {kind:5} OK   {identity}")
    if not record(f"选定的 {chosen} 通道可用", ok_by_kind.get(chosen, False)):
        others = [k for k, ok in ok_by_kind.items() if ok]
        if others:
            print(
                f"       注意：{'、'.join(others)} 通道是通的 —— 这是通道选择问题，"
                "不是授权问题，改 config.default_agentk_transport"
            )


def check_blob(config: object) -> str | None:
    print("\n[6] 对象存储（上传 + 容器侧回读）")
    from aigc_eval.providers.blob_store import BlobUnavailable, shared_blob_store

    try:
        store = shared_blob_store()
    except BlobUnavailable as error:
        record("blob 已配置", False, str(error))
        return None
    print(f"  {store.config.endpoint} / {store.config.bucket}")
    try:
        url = store.put_bytes(solid_png(PROBE_COLOR), suffix=".png")
    except Exception as error:  # noqa: BLE001
        record("上传成功", False, str(error)[:200])
        return None
    record("上传成功", True)
    try:
        with urllib.request.urlopen(url, timeout=20) as response:
            body = response.read()
    except (urllib.error.URLError, TimeoutError, OSError) as error:
        record("容器侧回读", False, str(error))
        return url
    record("容器侧回读内容一致", body.startswith(b"\x89PNG"), f"{len(body)} 字节")
    return url


def ask_model(config: object, model: str, url: str, prompt: str) -> str:
    from aigc_eval.providers.agentk_transport import transport_for
    from aigc_eval.providers.model_call import (
        UNDERSTAND_MEDIA_CAPABILITY,
        raw_text_from_output,
    )

    output = transport_for(config).invoke(
        UNDERSTAND_MEDIA_CAPABILITY,
        {"model": model, "mediaUrls": [url], "prompt": prompt},
        # 跟流水线用同一个上限。写死一个更短的值，会把"模型排队慢"报成
        # "环境不通"，而这两件事的处理方式完全相反。
        timeout=config.timeout_seconds,  # type: ignore[attr-defined]
    )
    return raw_text_from_output(UNDERSTAND_MEDIA_CAPABILITY, output or {})


def check_vision(config: object, url: str) -> None:
    print("\n[7] 多模态模型 + 网关取素材")
    for label, model in (
        ("主判模型（4b 等视觉判定）", config.model),  # type: ignore[attr-defined]
        ("音频模型（4a/4c）", config.audio_model),  # type: ignore[attr-defined]
    ):
        try:
            text = ask_model(config, model, url, COLOR_PROMPT)
        except Exception as error:  # noqa: BLE001
            state = "WARN" if is_sync_ceiling(error) else "FAIL"
            record(f"{label} {model}", state, explain(error))
            continue
        hit = any(word in text.lower() for word in COLOR_WORDS)
        record(
            f"{label} {model}",
            hit,
            repr(text[:60]) if hit else f"没答出颜色，说明没真取到图：{text[:60]!r}",
        )


def check_text_model(config: object) -> None:
    print("\n[8] 文本模型（拆解提示词那类无素材调用）")
    from aigc_eval.providers.agentk_transport import transport_for
    from aigc_eval.providers.model_call import (
        GENERATE_TEXT_CAPABILITY,
        raw_text_from_output,
    )

    try:
        output = transport_for(config).invoke(
            GENERATE_TEXT_CAPABILITY,
            {"model": config.chat_model, "prompt": "回答 OK 两个字母，不要其他内容。"},  # type: ignore[attr-defined]
            timeout=config.timeout_seconds,  # type: ignore[attr-defined]
        )
    except Exception as error:  # noqa: BLE001
        record(f"generate_text {config.chat_model}", False, explain(error))  # type: ignore[attr-defined]
        return
    text = raw_text_from_output(GENERATE_TEXT_CAPABILITY, output or {})
    # 空串也要判失败：调用没抛异常但什么都没回，正是这一步要抓的。
    record(f"generate_text {config.chat_model}", bool(text), repr(text[:40]))  # type: ignore[attr-defined]


def check_audio_entry(config: object) -> None:
    print("\n[9] 音频入口（4a 参考音频对照依赖它；按环境开放）")
    from aigc_eval.providers.blob_store import shared_blob_store

    if not shutil.which("ffmpeg"):
        record("音频入口", False, "没有 ffmpeg，造不出探测素材")
        return
    store = shared_blob_store()
    with tempfile.TemporaryDirectory() as tmp:
        work = Path(tmp)
        try:
            ffmpeg(["-f", "lavfi", "-i", SINE, "-c:a", "libmp3lame", str(work / "a.mp3")])
            # 对照组：带音轨的 mp4。视频音轨一直进得去，用它把变量分开——
            # mp3 不通而 mp4 通 ⇒ 是音频入口没开，不是模型或桶的问题。
            ffmpeg(
                [
                    "-f",
                    "lavfi",
                    "-i",
                    "color=c=black:s=320x240:r=10",
                    "-f",
                    "lavfi",
                    "-i",
                    SINE,
                    "-shortest",
                    "-c:v",
                    "libx264",
                    "-pix_fmt",
                    "yuv420p",
                    "-c:a",
                    "aac",
                    str(work / "a.mp4"),
                ]
            )
        except RuntimeError as error:
            record("音频入口", False, f"ffmpeg 造素材失败：{error}")
            return

        outcome: dict[str, bool] = {}
        for label, name in (("独立音频 mp3（4a 走这条）", "a.mp3"), ("带音轨 mp4（对照组）", "a.mp4")):
            path = work / name
            url = store.put_bytes(path.read_bytes(), suffix=path.suffix)
            try:
                text = ask_model(config, config.audio_model, url, AUDIO_PROMPT)  # type: ignore[attr-defined]
            except Exception as error:  # noqa: BLE001
                state = "WARN" if is_sync_ceiling(error) else "FAIL"
                outcome[name] = record(label, state, explain(error))
                continue
            outcome[name] = record(label, True, repr(text[:80]))

        if not outcome.get("a.mp3") and outcome.get("a.mp4"):
            print("       独立音频进不去、视频音轨可以 ⇒ 这个环境音频入口没放开。")
            print("       4a 需要给音频套一层静态画面再送（已验证可行）。")


def main() -> int:
    print("[0] 借平台身份（凭证只注入到服务进程）")
    announce_identity()
    from aigc_eval.config import load_agentk_config

    config = load_agentk_config()
    print(
        f"  服务名 {os.environ.get('KWS_SERVICE_NAME') or '(未注入)'}"
        f" | 环境 {os.environ.get('KWS_SERVICE_STAGE') or '(未注入)'}"
    )

    check_ffmpeg()
    check_runtime_writable()
    check_health()
    check_worker_alive()
    check_identity(config)
    url = check_blob(config)
    if url:
        check_vision(config, url)
    else:
        record("多模态模型", False, "没有可用素材 URL，先修对象存储")
    check_text_model(config)
    check_audio_entry(config)

    print("\n" + "=" * 60)
    failed = [name for name, state, _ in results if state == "FAIL"]
    warned = [name for name, state, _ in results if state == "WARN"]
    skipped = [name for name, state, _ in results if state == "SKIP"]
    for name, state, note in results:
        print(f"  {state}  {name}" + (f"  ({note[:70]})" if state != "PASS" else ""))
    print("=" * 60)
    print(
        json.dumps(
            {
                "checks": len(results),
                "failed": len(failed),
                "warned": len(warned),
                "skipped": len(skipped),
            },
            ensure_ascii=False,
        )
    )
    if failed:
        print(f"\n{len(failed)} 项未通过：{'、'.join(failed)}")
        print("先修这些，再谈上线。")
        return 1
    if skipped:
        print(f"\n跳过 {len(skipped)} 项（只在容器内可验）：{'、'.join(skipped)}")
    if warned:
        print(f"\n{len(warned)} 项没结论：{'、'.join(warned)}")
        print("这些撞了同步直调的 300s 上限，而流水线走工作流不受此限，")
        print("所以既不能算通也不能算不通 —— 必须用一次真实质检来判。")
    print("\n没有 FAIL：能独立验的外部条件在这个环境都可用。")
    print("注意这个脚本验的是依赖，不是判定质量；结论对不对要看真实质检。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
