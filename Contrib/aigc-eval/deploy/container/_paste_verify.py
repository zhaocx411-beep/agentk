import asyncio
import os
import shutil
import struct
import subprocess
import sys
import tempfile
import urllib.request
import zlib
from pathlib import Path

sys.path.insert(0, "/home/web_server/aigc-eval/bin")
R = []


def rec(n, s, note=""):
    s = s if isinstance(s, str) else ("PASS" if s else "FAIL")
    R.append((n, s, note))
    print(f"  {s}  {n}" + (f" — {note}" if note else ""))
    return s == "PASS"


def ceiling(e):
    return "同步直调上限" in str(e) or "被网关切断" in str(e)


def why(e):
    t = str(e)
    low = t.lower()
    if ceiling(e):
        return f"撞网关300s同步上限；流水线走工作流不受此限，需真实质检确认。{t[:140]}"
    if "超时" in t or "timeout" in low:
        return f"等待超时，可能是模型排队。{t[:160]}"
    if any(w in low for w in ("unauthorized", "permission", "denied")):
        return f"身份被拒，需在本环境登记KSN，找AgentK维护方。{t[:200]}"
    if any(w in low for w in ("not available", "not found", "no such model")):
        return f"模型/能力未开放。{t[:200]}"
    if any(w in low for w in ("unsupported", "invalid url", "fetch", "download")):
        return f"素材被拒或取不到。{t[:200]}"
    return t[:300]


def png(rgb=(255, 0, 0), n=64):
    raw = (b"\x00" + bytes(rgb) * n) * n

    def ch(tag, d):
        b = tag + d
        return struct.pack(">I", len(d)) + b + struct.pack(">I", zlib.crc32(b) & 0xFFFFFFFF)

    return (
        b"\x89PNG\r\n\x1a\n"
        + ch(b"IHDR", struct.pack(">IIBBBBB", n, n, 8, 2, 0, 0, 0))
        + ch(b"IDAT", zlib.compress(raw))
        + ch(b"IEND", b"")
    )


def ff(args):
    d = subprocess.run(
        ["ffmpeg", "-hide_banner", "-loglevel", "error", "-y", *args],
        capture_output=True,
        text=True,
        check=False,
    )
    if d.returncode:
        raise RuntimeError(d.stderr[-200:])


try:
    from container_env import announce_identity

    print("[0] 借服务进程身份")
    announce_identity()
except Exception as e:
    print(f"[0] 借身份失败：{e}")

from aigc_eval.config import load_agentk_config
from aigc_eval.providers.agentk_transport import transport_for
from aigc_eval.providers.model_call import (
    GENERATE_TEXT_CAPABILITY,
    UNDERSTAND_MEDIA_CAPABILITY,
    raw_text_from_output,
)

C = load_agentk_config()
print(
    f"  服务名 {os.environ.get('KWS_SERVICE_NAME') or '(无)'} | "
    f"环境 {os.environ.get('KWS_SERVICE_STAGE') or '(无)'} | 通道 {C.transport}"
)

print("\n[1] ffmpeg")
for t in ("ffmpeg", "ffprobe"):
    p = shutil.which(t)
    rec(f"{t} 在位", bool(p), "" if p else "镜像里没装，抽帧与预检会全线失败")

print("\n[2] runtime 写权限")
from aigc_eval.paths import REPORT_ROOT, TRACE_ROOT, WORK_ROOT

for lb, d in (("work", WORK_ROOT), ("reports", REPORT_ROOT), ("traces", TRACE_ROOT)):
    try:
        d.mkdir(parents=True, exist_ok=True)
        q = d / ".vp"
        q.write_text("ok")
        q.unlink()
        rec(f"runtime/{lb} 可写", True)
    except OSError as e:
        rec(f"runtime/{lb} 可写", False, str(e))

print("\n[3] 健康检查端点")
port = os.environ.get("VIDEO_JUDGE_PORT") or os.environ.get("AUTO_PORT0") or ""
if not port:
    rec("/healthz 正确应答", "SKIP", "没有平台分配的端口")
else:
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{port}/healthz", timeout=10) as r:
            body, code = r.read().decode(), r.status
        rec(f"/healthz 返回200(端口{port})", code == 200, f"code={code}")
        rec('/healthz 体为紧凑{"status":"ok"}', body == '{"status":"ok"}', repr(body))
    except Exception as e:
        rec(f"/healthz 可达(端口{port})", False, str(e))

print("\n[4] 双进程")
if not Path("/proc").is_dir():
    rec("worker与HTTP都在跑", "SKIP", "非容器环境")
else:
    run = {"w": False, "h": False}
    for pid in [p for p in os.listdir("/proc") if p.isdigit()]:
        try:
            cl = Path(f"/proc/{pid}/cmdline").read_text().replace("\0", " ")
        except OSError:
            continue
        if "worker.py" in cl:
            run["w"] = True
        elif "aigc_eval" in cl and "-m" in cl:
            run["h"] = True
    rec("temporal worker 在跑", run["w"])
    rec("http 进程在跑", run["h"])

print("\n[5] 身份与传输通道")
from agentk.api.v1.service_pb import WhoAmIRequest
from agentk.client import Options
from agentk.client.grpc import create_client as mk_grpc
from agentk.client.http import create_client as mk_http

ok = {}
for kind, mk, op in (
    ("http", mk_http, Options(endpoint=C.base_url)),
    ("grpc", mk_grpc, Options()),
):
    try:
        idn = asyncio.run(mk(op).who_am_i(WhoAmIRequest()))
        ok[kind] = True
        print(f"    {kind:5} OK   {idn}")
    except Exception as e:
        ok[kind] = False
        print(f"    {kind:5} FAIL {type(e).__name__}: {str(e)[:110]}")
if not rec(f"选定的 {C.transport} 通道可用", ok.get(C.transport, False)):
    alt = [k for k, v in ok.items() if v]
    if alt:
        print(f"       注意：{'、'.join(alt)} 是通的 ⇒ 通道选择问题，非授权问题")

print("\n[6] 对象存储")
from aigc_eval.providers.blob_store import BlobUnavailable, shared_blob_store

URL = None
try:
    st = shared_blob_store()
    print(f"  {st.config.endpoint} / {st.config.bucket}")
    URL = st.put_bytes(png(), suffix=".png")
    rec("上传成功", True)
    try:
        with urllib.request.urlopen(URL, timeout=20) as r:
            b = r.read()
        rec("容器侧回读一致", b.startswith(b"\x89PNG"), f"{len(b)}字节")
    except Exception as e:
        rec("容器侧回读", False, str(e))
except BlobUnavailable as e:
    rec("blob 已配置", False, str(e))
except Exception as e:
    rec("上传成功", False, str(e)[:200])


def ask(model, url, prompt):
    out = transport_for(C).invoke(
        UNDERSTAND_MEDIA_CAPABILITY,
        {"model": model, "mediaUrls": [url], "prompt": prompt},
        timeout=C.timeout_seconds,
    )
    return raw_text_from_output(UNDERSTAND_MEDIA_CAPABILITY, out or {})


print("\n[7] 多模态模型 + 网关取素材")
CP = "这张图片是什么颜色？只回答颜色名，不要其他内容。"
if not URL:
    rec("多模态模型", False, "没有素材URL，先修对象存储")
else:
    for lb, m in (("主判模型", C.model), ("音频模型", C.audio_model)):
        try:
            tx = ask(m, URL, CP)
        except Exception as e:
            rec(f"{lb} {m}", "WARN" if ceiling(e) else "FAIL", why(e))
            continue
        hit = any(w in tx.lower() for w in ("红", "red", "赤"))
        rec(f"{lb} {m}", hit, repr(tx[:60]) if hit else f"没答出颜色，未真取到图：{tx[:60]!r}")

print("\n[8] 文本模型")
try:
    out = transport_for(C).invoke(
        GENERATE_TEXT_CAPABILITY,
        {"model": C.chat_model, "prompt": "回答 OK 两个字母，不要其他内容。"},
        timeout=C.timeout_seconds,
    )
    tx = raw_text_from_output(GENERATE_TEXT_CAPABILITY, out or {})
    rec(f"generate_text {C.chat_model}", bool(tx), repr(tx[:40]))
except Exception as e:
    rec(f"generate_text {C.chat_model}", "WARN" if ceiling(e) else "FAIL", why(e))

print("\n[9] 音频入口（4a 依赖）")
AP = "这段素材里有声音吗？如果有，描述它的音高与时长。只回答一句话。"
SINE = "sine=frequency=440:duration=3"
if not shutil.which("ffmpeg"):
    rec("音频入口", False, "没有ffmpeg，造不出素材")
else:
    with tempfile.TemporaryDirectory() as tmp:
        w = Path(tmp)
        try:
            ff(["-f", "lavfi", "-i", SINE, "-c:a", "libmp3lame", str(w / "a.mp3")])
            ff([
                "-f", "lavfi", "-i", "color=c=black:s=320x240:r=10",
                "-f", "lavfi", "-i", SINE, "-shortest", "-c:v", "libx264",
                "-pix_fmt", "yuv420p", "-c:a", "aac", str(w / "a.mp4"),
            ])
            oc = {}
            for lb, nm in (("独立音频mp3(4a走这条)", "a.mp3"), ("带音轨mp4(对照组)", "a.mp4")):
                p = w / nm
                u = shared_blob_store().put_bytes(p.read_bytes(), suffix=p.suffix)
                try:
                    tx = ask(C.audio_model, u, AP)
                    oc[nm] = rec(lb, True, repr(tx[:80]))
                except Exception as e:
                    oc[nm] = rec(lb, "WARN" if ceiling(e) else "FAIL", why(e))
            if not oc.get("a.mp3") and oc.get("a.mp4"):
                print("       独立音频进不去而视频音轨可以 ⇒ 本环境音频入口未放开")
        except RuntimeError as e:
            rec("音频入口", False, f"ffmpeg造素材失败：{e}")

print("\n" + "=" * 58)
F = [n for n, s, _ in R if s == "FAIL"]
W = [n for n, s, _ in R if s == "WARN"]
S = [n for n, s, _ in R if s == "SKIP"]
for n, s, note in R:
    print(f"  {s}  {n}" + (f"  ({note[:66]})" if s != "PASS" else ""))
print("=" * 58)
print(f"共{len(R)}项 FAIL={len(F)} WARN={len(W)} SKIP={len(S)}")
if F:
    print(f"\n未通过：{'、'.join(F)}\n先修这些。")
if S:
    print(f"\n跳过（仅容器内可验）：{'、'.join(S)}")
if W:
    print(f"\n没结论：{'、'.join(W)}")
    print("撞了同步直调300s上限，流水线走工作流不受此限，必须用真实质检判定。")
if not F:
    print("\n没有 FAIL：能独立验的外部条件均可用。结论质量仍须真实质检确认。")
sys.exit(1 if F else 0)
