"""部署后冒烟：在容器里验证「我们这侧的配置」与「平台那侧的授权」都对得上。

健康检查绿了只说明 HTTP 端口活着。质检真正依赖的三件事它一件都没碰：

1. 天琴服务名在**当前环境的网关**上有没有模型调用权限。能力注册走 Temporal、
   模型调用走网关，是两套独立授权——worker 在能力目录里出现，不代表网关认它。
2. 我们配的 blob 桶，**网关那侧**能不能取。质检把素材传上去后只交 URL，取不到
   的表现是模型侧报素材错误，而不是上传失败，所以光验上传是验不出来的。
3. 我们写死的那几个模型在这个环境开不开放。三个里两个是 preview。

这三件事共用同一条链路，所以合成一次调用验：上传一张纯色图，让模型回答它是什么
颜色。答对了，说明网关认了我们的身份、模型可用、且它成功取到了我们的桶。任一环
断掉都会在这一步失败，脚本按错误文本区分是哪一环，避免又一轮「先怀疑代码」。

跑法（容器内）：

    python3 /home/web_server/aigc-eval/bin/smoke_check.py

全过退出码 0，任一项失败退出码 1，可直接挂在上线流程里。
"""

from __future__ import annotations

import os
import struct
import sys
import urllib.error
import urllib.request
import zlib

from container_env import announce_identity

from aigc_eval.config import load_agentk_config
from aigc_eval.providers.agentk_transport import transport_for
from aigc_eval.providers.blob_store import BlobUnavailable, shared_blob_store
from aigc_eval.providers.model_call import (
    GENERATE_TEXT_CAPABILITY,
    UNDERSTAND_MEDIA_CAPABILITY,
    raw_text_from_output,
)

# 图里填什么颜色是随便挑的，但**必须是模型不看图就答不出的信息**：问「这是什么颜色」
# 而不是「这是不是一张图」，才能把「网关取到了素材」和「网关只是收下了请求」分开。
PROBE_COLOR = (255, 0, 0)
PROBE_COLOR_WORDS = ("红", "red", "赤")
PROBE_PROMPT = "这张图片是什么颜色？只回答颜色名，不要其他内容。"


def solid_png(rgb: tuple[int, int, int], size: int = 64) -> bytes:
    """手写一张纯色 PNG。

    不用 ffmpeg 生成：ffmpeg 在镜像里是给抽帧用的，冒烟脚本多一个外部依赖就多一种
    「工具本身坏了」的失败模式，而这里要的东西手写只有十行。
    """
    row = b"\x00" + bytes(rgb) * size  # 每行前缀 0 = 不做行间预测
    raw = row * size

    def chunk(tag: bytes, data: bytes) -> bytes:
        body = tag + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)

    header = struct.pack(">IIBBBBB", size, size, 8, 2, 0, 0, 0)  # 8bit truecolor
    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", header)
        + chunk(b"IDAT", zlib.compress(raw))
        + chunk(b"IEND", b"")
    )


def explain(error: Exception) -> str:
    """把平台错误翻成「该找谁」。

    这三类的处理人完全不同：授权要找 AgentK 侧加白，模型不可用要换模型或申请开放，
    取不到素材是我们的桶配错或网络不通。混在一起看就会把平台问题当成代码问题查。
    """
    text = str(error)
    lowered = text.lower()
    if "unauthorized" in lowered or "permission" in lowered or "denied" in lowered:
        service = os.environ.get("KWS_SERVICE_NAME", "(未注入)")
        return (
            f"网关拒绝了身份 {service}。这个服务名需要在当前环境的 AgentK 网关上单独加白，"
            f"staging 通过不代表 prod 通过。原文：{text}"
        )
    if "model" in lowered and ("not" in lowered or "invalid" in lowered or "支持" in text):
        return f"模型在当前环境不可用（我们依赖的三个里两个是 preview）。原文：{text}"
    if any(word in lowered for word in ("fetch", "download", "media", "url")):
        return (
            "网关取不到我们上传的素材 URL。桶本身能上传不代表网关那侧可达，"
            f"需要确认这个环境该用哪个 endpoint/bucket。原文：{text}"
        )
    return text


def check_identity() -> bool:
    config = load_agentk_config()
    stage = os.environ.get("KWS_SERVICE_STAGE", "")
    print(f"  服务名   KWS_SERVICE_NAME = {os.environ.get('KWS_SERVICE_NAME') or '(未注入)'}")
    print(f"  环境     KWS_SERVICE_STAGE = {stage or '(未注入)'}")
    print(f"  通道     {config.transport}（http 时连 {config.base_url}）")
    # 凭证只做展示，不作判据：本机联调只靠服务名就能通，容器里也未必注入这个变量。
    # 身份到底认不认，唯一可靠的判断是后面那几次真实调用。
    if not config.service_credential:
        print("  凭证     KWS_SERVICE_CREDENTIAL 未注入（正常，身份可由运行环境提供）")
    if os.environ.get("AGENTK_BASE_URL"):
        print("  ! 检测到手工设置的 AGENTK_BASE_URL，它会覆盖推导结果并可能导致未认证")
    if not config.service_name:
        print("  FAIL KWS_SERVICE_NAME 未注入，模型调用必然被拒")
        return False
    return True


def check_blob() -> str | None:
    """验上传与容器侧回读，返回可交给网关的 URL。"""
    try:
        store = shared_blob_store()
    except BlobUnavailable as error:
        print(f"  FAIL blob 未配置：{error}")
        return None
    print(f"  endpoint {store.config.endpoint}")
    print(f"  bucket   {store.config.bucket}")
    try:
        url = store.put_bytes(solid_png(PROBE_COLOR), suffix=".png")
    except Exception as error:  # noqa: BLE001 - 冒烟脚本要把任何失败原样报出来
        print(f"  FAIL 上传失败：{error}")
        return None
    print(f"  上传 OK  {url}")
    try:
        with urllib.request.urlopen(url, timeout=20) as response:
            body = response.read()
    except (urllib.error.URLError, TimeoutError) as error:
        print(f"  FAIL 容器侧回读失败：{error}")
        return None
    if not body.startswith(b"\x89PNG"):
        print(f"  FAIL 回读内容不是刚传上去的 PNG（{len(body)} 字节）")
        return None
    print(f"  回读 OK  {len(body)} 字节，内容一致")
    return url


def check_understand(url: str, model: str, label: str) -> bool:
    print(f"  {label}：{model}")
    try:
        output = transport_for(load_agentk_config()).invoke(
            UNDERSTAND_MEDIA_CAPABILITY,
            {"model": model, "mediaUrls": [url], "prompt": PROBE_PROMPT},
            timeout=120,
        )
    except Exception as error:  # noqa: BLE001
        print(f"  FAIL {explain(error)}")
        return False
    # 两个能力的输出键不同（understand_media 回 text、generate_text 回 result），
    # 收敛只允许有一处实现，所以这里复用 judge 侧同一个函数而不是自己取键。
    text = raw_text_from_output(UNDERSTAND_MEDIA_CAPABILITY, output or {})
    print(f"  回答     {text!r}")
    if not any(word in text.lower() for word in PROBE_COLOR_WORDS):
        # 调用通了但答错色：网关认了身份、模型也在，但它没真看到图。
        print("  FAIL 模型没答出图片颜色，说明它没能取到素材（授权与模型本身没问题）")
        return False
    return True


def check_generate_text(model: str) -> bool:
    """chat_model 单独验：它走 ChatClient 查表，和多模态模型是两套开放范围。"""
    print(f"  文本模型 {model}")
    try:
        output = transport_for(load_agentk_config()).invoke(
            GENERATE_TEXT_CAPABILITY,
            {"model": model, "prompt": "回答 OK 两个字母，不要其他内容。"},
            timeout=60,
        )
    except Exception as error:  # noqa: BLE001
        print(f"  FAIL {explain(error)}")
        return False
    text = raw_text_from_output(GENERATE_TEXT_CAPABILITY, output or {})
    print(f"  回答     {text!r}")
    if not text:
        # 空串要判失败：调用没抛异常但什么都没回，说明模型侧有问题，
        # 而这一步的意义正是在上线前发现它。
        print("  FAIL 调用成功但没有返回任何文本")
        return False
    return True


def main() -> int:
    # 必须在建任何客户端之前借身份：凭证只注入到服务进程，WebConsole 的 shell 没有。
    announce_identity()
    config = load_agentk_config()
    results: list[tuple[str, bool]] = []

    print("[1/4] 身份与网关")
    results.append(("身份与网关", check_identity()))

    print("\n[2/4] 对象存储（上传 + 容器侧回读）")
    url = check_blob()
    results.append(("对象存储", url is not None))

    print("\n[3/4] 多模态模型 + 网关取素材")
    if url is None:
        print("  SKIP 没有可用素材 URL，前一步先修")
        results.append(("多模态模型", False))
    else:
        main_ok = check_understand(url, config.model, "主判模型")
        audio_ok = check_understand(url, config.audio_model, "音频模型")
        results.append(("多模态模型", main_ok and audio_ok))

    print("\n[4/4] 文本模型")
    results.append(("文本模型", check_generate_text(config.chat_model)))

    print("\n=== 结论 ===")
    for name, ok in results:
        print(f"  {'PASS' if ok else 'FAIL'}  {name}")
    failed = [name for name, ok in results if not ok]
    if failed:
        print(f"\n{len(failed)} 项未通过，先别对外提供服务：{'、'.join(failed)}")
        return 1
    print("\n全部通过，这个环境可以跑真实质检了。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
