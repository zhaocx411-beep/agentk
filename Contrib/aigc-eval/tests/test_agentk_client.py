"""AgentK 通道的离线联调测试。

假服务不是手搓的 HTTP handler，而是**官方生成的 Connect server stub**
（``AgentKServiceWSGIApplication``）。这一点是刻意的：客户端用 SDK 生成的
stub 发请求、服务端用同一份 proto 生成的 stub 收请求，两边能对上就说明
传输、编码（默认二进制 protobuf 而非 JSON）、过程路径全都合契约——手写
假服务只能证明"我们和自己的假设一致"。

验证六件最容易出错、且线上出错代价最高的事：

1. 走的是 ``InvokeCapability`` 直调，不再建 Session / 起 Execution / 轮询；
2. 送出去的 input 形状合契约（有素材 → understand_media 的
   model/mediaUrls/prompt；无素材 → generate_text，且不带 mediaUrls）；
3. 素材超过 understand_media 的 maxItems=10 时正确降载而不是整条失败；
4. 同一份字节只上传一次（一次质检要对同一段视频发起 8~9 次调用）；
5. 多线程共用一个 client 时，同步桥不串包也不死锁；
6. 错误分流按 Connect 错误码而不是错误信息措辞——网关切断可重试、
   能力自身的业务错误不重试（重试只是白烧一次模型调用）；
7. 音频按阶段走两条路：非音频阶段完全送不进去、只在提示词里标明"这次听不到"，
   音频阶段换 ``audio_model`` 并把音频本体送进 mediaUrls，且提示词措辞跟着换——
   措辞没换的话音频送到了也会被提示词自己压制掉。

只依赖已装好的 SDK，直接 ``uv run python tests/test_agentk_client.py`` 运行。
"""

from __future__ import annotations

import base64
import json
import os
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from socketserver import ThreadingMixIn
from wsgiref.simple_server import WSGIRequestHandler, WSGIServer, make_server

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

# 身份头由 SDK 在建客户端时读环境变量生成，必须在 import/建客户端之前设好。
SERVICE_NAME = "klingai-agentk-aigc-eval-api"
os.environ["KWS_SERVICE_NAME"] = SERVICE_NAME
os.environ.pop("KWS_SERVICE_CREDENTIAL", None)

from agentk.api.v1.service_connect import (  # noqa: E402
    AgentKServiceSync,
    AgentKServiceWSGIApplication,
)
from agentk.api.v1.service_pb import (  # noqa: E402
    InvokeCapabilityRequest,
    InvokeCapabilityResponse,
)
from protobuf.wkt import Value  # noqa: E402

from aigc_eval.config import AgentKConfig  # noqa: E402
from aigc_eval.providers.agentk import AgentKClient  # noqa: E402
from aigc_eval.providers.agentk_transport import (  # noqa: E402
    AgentKCallError,
    AgentKTransport,
)
from aigc_eval.providers.blob_store import BlobConfig, BlobStore  # noqa: E402


# 假模型的回答：故意包在 markdown 代码块里，顺带验证 extract_json_object
# 的修复级联在 AgentK 通道下依然生效（understand_media 没有 JSON mode）。
CANNED_ANSWER = """```json
{"verdict": "violated", "severity": 2, "evidence": "第 3 帧手指数量异常"}
```"""


class State:
    def __init__(self) -> None:
        self.invocations: list[tuple[str, dict]] = []
        self.headers: list[dict[str, str]] = []
        self.puts: list[str] = []
        self.lock = threading.Lock()
        # 第 7 组用：裸 Connect 服务按这个脚本逐次应答。每项是 (code, message)
        # 表示回错误，None 表示回成功；脚本空了就一直回成功。
        self.script: list[tuple[str, str] | None] = []
        self.scripted_calls = 0


STATE = State()


class FakeAgentK(AgentKServiceSync):
    """只实现 InvokeCapability。

    其余过程沿用 Protocol 的默认实现——它们会回 UNIMPLEMENTED，所以一旦
    provider 又退回 Session/Execution 那套流程，测试会直接失败而不是静默通过。
    """

    def invoke_capability(self, request: InvokeCapabilityRequest, ctx) -> InvokeCapabilityResponse:
        payload = request.input.to_python() if request.input is not None else {}
        with STATE.lock:
            STATE.invocations.append((request.capability_id, payload))
            STATE.headers.append(
                {
                    name.lower(): value
                    for name, value in ctx.request_headers.items()
                    if name.lower() in ("kws-bin", "kwscredential", "authorization")
                }
            )
        if request.capability_id.endswith("generate_text"):
            # textgen 未带 outputSchema 时 result 就是模型原文（实测直调
            # 返回 {'result': '收到'}）。
            output = {"result": CANNED_ANSWER}
        else:
            output = {
                "text": CANNED_ANSWER,
                "model": "gemini-3.1-pro-preview",
                "params": {"delivery": "inline"},
            }
        return InvokeCapabilityResponse(output=Value.from_python(output))


class _ThreadingWSGIServer(ThreadingMixIn, WSGIServer):
    daemon_threads = True


class _QuietWSGIHandler(WSGIRequestHandler):
    def log_message(self, *args) -> None:
        pass


class ScriptedHandler(BaseHTTPRequestHandler):
    """按脚本逐次回错误或成功的裸 Connect 服务，专供第 7 组错误分流用。

    为什么不复用上面那个官方 server stub：``ConnectWSGIApplication`` 的错误
    响应在 wsgiref 下发不出去（服务端不写响应、客户端一直等到超时，实测
    确认；成功路径正常）。而 Connect 的错误应答本身就是协议规定的
    「非 200 + ``{"code","message"}`` JSON」，裸 HTTP 完全能如实复刻——实测
    官方客户端 0.00s 就解析成对应的 ConnectError。所以这里刻意用最笨的方式，
    避免把库的一个缺陷伪装成我们的错误分流通过了。
    """

    protocol_version = "HTTP/1.1"

    def log_message(self, *args) -> None:
        pass

    def do_POST(self) -> None:  # noqa: N802
        length = int(self.headers.get("Content-Length") or 0)
        self.rfile.read(length)
        with STATE.lock:
            STATE.scripted_calls += 1
            failure = STATE.script.pop(0) if STATE.script else None

        if failure is not None:
            body = json.dumps({"code": failure[0], "message": failure[1]}).encode()
            content_type = "application/json"
            status = 503 if failure[0] == "unavailable" else 500
        else:
            # 成功应答就是序列化后的响应消息，Content-Type 回显客户端用的编码
            # （官方客户端默认二进制 protobuf，不是 JSON）。
            body = InvokeCapabilityResponse(
                output=Value.from_python(
                    {"text": CANNED_ANSWER, "model": "gemini-3.1-pro-preview", "params": {}}
                )
            ).to_binary()
            content_type = self.headers.get("Content-Type") or "application/proto"
            status = 200

        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


class BlobHandler(BaseHTTPRequestHandler):
    """假对象存储：任意 PUT 都接受，只记账。"""

    def log_message(self, *args) -> None:
        pass

    def do_PUT(self) -> None:  # noqa: N802
        length = int(self.headers.get("Content-Length") or 0)
        self.rfile.read(length)
        with STATE.lock:
            STATE.puts.append(self.path)
        self.send_response(200)
        self.send_header("Content-Length", "0")
        self.end_headers()


def build_client(
    agentk_url: str, blob_url: str, workdir: Path
) -> tuple[AgentKClient, Path, list[str]]:
    """造一个指向假服务的 client，外加一段假视频和 16 张内容各异的假帧。"""
    video = workdir / f"clip_{len(STATE.puts)}.mp4"
    video.write_bytes(b"FAKE-MP4-BYTES" * 64)
    frames = [
        base64.b64encode(f"FAKE-JPEG-{i}".encode() * 32).decode("ascii") for i in range(16)
    ]
    blob = BlobStore(BlobConfig(endpoint=blob_url, bucket="judge", cdn_base=""))
    config = AgentKConfig(
        base_url=agentk_url,
        model="gemini-3.1-pro-preview",
        chat_model="deepseek-v4-pro",
        service_name=SERVICE_NAME,
        timeout_seconds=20.0,
    )
    # 显式给 transport：shared_transport 按 endpoint 缓存，测试里每个 client
    # 各自持有才能独立关掉，不把事件循环线程泄漏到别的用例。
    transport = AgentKTransport(agentk_url)
    return AgentKClient(config, blob=blob, transport=transport), video, frames


def main() -> int:
    agentk_server = make_server(
        "127.0.0.1", 0, AgentKServiceWSGIApplication(FakeAgentK()),
        server_class=_ThreadingWSGIServer, handler_class=_QuietWSGIHandler,
    )
    threading.Thread(target=agentk_server.serve_forever, daemon=True).start()
    agentk_url = f"http://127.0.0.1:{agentk_server.server_address[1]}"

    blob_server = ThreadingHTTPServer(("127.0.0.1", 0), BlobHandler)
    threading.Thread(target=blob_server.serve_forever, daemon=True).start()
    blob_url = f"http://127.0.0.1:{blob_server.server_address[1]}"

    scripted_server = ThreadingHTTPServer(("127.0.0.1", 0), ScriptedHandler)
    threading.Thread(target=scripted_server.serve_forever, daemon=True).start()
    scripted_url = f"http://127.0.0.1:{scripted_server.server_address[1]}"

    failures: list[str] = []
    clients: list[AgentKClient] = []

    def check(label: str, condition: bool, detail: object = "") -> None:
        if condition:
            print(f"  PASS  {label}")
        else:
            print(f"  FAIL  {label} {detail}")
            failures.append(label)

    with tempfile.TemporaryDirectory() as tmp:
        workdir = Path(tmp)
        client, video, frames = build_client(agentk_url, blob_url, workdir)
        clients.append(client)

        parts = [
            {"text": "请按 rubric 逐项判定。"},
            {"video_path": str(video), "fps": 0.5, "video_tokens_total": 16000},
            {"text": "以下是 16 张高清关键帧："},
            *[{"inline_data": {"mime_type": "image/jpeg", "data": f}} for f in frames],
        ]

        print("\n[1] 单次调用：直调形状 + 素材降载")
        result = client.generate_json(
            parts,
            system_instruction="你是 AIGC 视频质检专家。",
            stage="unified_scan",
            title="合并扫描",
        )
        check("返回值被正确解析出 JSON", result.get("verdict") == "violated", result)
        check("severity 透传", result.get("severity") == 2)

        capability, payload = STATE.invocations[0]
        check("直调 understand_media", capability == "agentk.understand_media", capability)
        check("input 只含契约字段", set(payload) == {"model", "mediaUrls", "prompt"},
              sorted(payload))
        check("model 落在 understand_media enum 内",
              payload["model"] == "gemini-3.1-pro-preview")
        media = payload["mediaUrls"]
        check(f"mediaUrls 降载到上限 10（实收 {len(media)}）", len(media) == 10)
        check("mediaUrls 全部是 URL", all(u.startswith("http://") for u in media))

        prompt = payload["prompt"]
        check("system_instruction 已并入 prompt 开头",
              prompt.startswith("你是 AIGC 视频质检专家。"))
        check("视频锚点在位", "【素材 1｜视频】" in prompt)
        check("素材清单已附", "【素材清单】" in prompt)
        check("降载已在 prompt 里显式告知模型", "【素材裁剪说明】" in prompt)
        check("JSON 输出约束已附", "【输出格式】" in prompt)

        check("降载事实已记账", bool(client.media_notes) and client.media_notes[0]["kept"] == 10,
              client.media_notes)
        usage = client.usage[0]
        check("token 缺失被打标而非伪装成 0 成本", usage.get("tokens_unavailable") is True)
        check("usage 记录实际送出的素材数", usage.get("media_sent") == 10)

        uploads_after_first = len(STATE.puts)
        check(f"首次上传 10 个对象（实际 {uploads_after_first}）", uploads_after_first == 10)

        print("\n[2] 二次调用：内容寻址去重，且没有多余 RPC")
        client.generate_json(parts, system_instruction="你是 AIGC 视频质检专家。")
        check("重复素材零重传", len(STATE.puts) == uploads_after_first,
              f"新增 {len(STATE.puts) - uploads_after_first}")
        check("两次调用只发了两个 RPC（无建会话、无轮询）",
              len(STATE.invocations) == 2, len(STATE.invocations))

        # 质检流程里「拆解提示词要求」等环节没有素材，而 understand_media 的
        # mediaUrls 是 minItems=1。这类调用必须自动改派 generate_text，
        # 否则整条 case 在第 3 步就断掉 —— 这曾是真实存在的阻塞性缺陷。
        print("\n[3] 纯文本调用改派 generate_text")
        parsed_text = client.generate_json([{"text": "只有文字"}])
        capability, payload = STATE.invocations[-1]
        check("改调 generate_text", capability == "agentk.generate_text", capability)
        check("未携带 mediaUrls 字段", "mediaUrls" not in payload, sorted(payload))
        check("用的是 chat 家族模型", payload["model"] == "deepseek-v4-pro", payload["model"])
        check("result 字符串被正常解析", parsed_text.get("verdict") == "violated", parsed_text)
        check("纯文本调用记账 media_sent=0", client.usage[-1]["media_sent"] == 0)

        print("\n[4] 边界：素材不超限时不应触发降载")
        client2, video2, frames2 = build_client(agentk_url, blob_url, workdir)
        clients.append(client2)
        client2.generate_json(
            [
                {"video_path": str(video2)},
                *[{"inline_data": {"mime_type": "image/jpeg", "data": f}} for f in frames2[:5]],
            ]
        )
        _, payload2 = STATE.invocations[-1]
        check("6 件素材全部保留", len(payload2["mediaUrls"]) == 6, len(payload2["mediaUrls"]))
        check("未触发降载说明", "【素材裁剪说明】" not in payload2["prompt"])
        check("未产生降载记账", client2.media_notes == [])

        # judge_pipeline 用 ThreadPoolExecutor 并发跑扫描分片。直调不再有
        # 「一个会话只能一个活跃执行」的约束，但所有线程共用同一条事件循环，
        # 所以要验证同步桥在并发下不串包、不死锁。
        print("\n[5] 并发：多线程共用 client 与同一条事件循环")
        client3, video3, frames3 = build_client(agentk_url, blob_url, workdir)
        clients.append(client3)
        payload_parts = [
            {"video_path": str(video3)},
            {"inline_data": {"mime_type": "image/jpeg", "data": frames3[0]}},
        ]
        errors: list[str] = []
        outcomes: list[dict] = []

        def worker() -> None:
            try:
                outcomes.append(client3.generate_json(payload_parts))
            except Exception as error:  # noqa: BLE001
                errors.append(str(error))

        threads = [threading.Thread(target=worker) for _ in range(4)]
        for thread in threads:
            thread.start()
        for thread in threads:
            thread.join()

        check("4 个并发调用全部成功", not errors, errors[:2])
        check("4 个结果都解析出 JSON",
              len(outcomes) == 4 and all(o.get("severity") == 2 for o in outcomes),
              len(outcomes))

        print("\n[6] 鉴权：只发 kws-bin，不发 Bearer")
        # 服务名的 kws-bin 是 protobuf 定长编码，跨语言基准值见 test_kws_identity。
        expected = "ChxrbGluZ2FpLWFnZW50ay1haWdjLWV2YWwtYXBp"
        check("每个请求都带 kws-bin",
              bool(STATE.headers) and all(h.get("kws-bin") == expected for h in STATE.headers),
              STATE.headers[:1])
        check("不再发 Bearer", not any("authorization" in h for h in STATE.headers))

        print("\n[7] 错误分流：按 Connect 错误码而非错误信息措辞")
        client4, video4, frames4 = build_client(scripted_url, blob_url, workdir)
        clients.append(client4)
        one_part = [
            {"video_path": str(video4)},
            {"inline_data": {"mime_type": "image/jpeg", "data": frames4[0]}},
        ]
        gateway_cut = ("unavailable", "Gateway Timeout")
        # 平台把 capability 内部的业务错误（模型不可用、参数不合法）都塞进
        # unknown，实测措辞就是这样。
        business = ("unknown", 'text capability "x" is not available')

        # 退避会真的 sleep，这里把重试等待压到 0，别让测试白等 6 秒。
        client4._backoff_seconds = staticmethod(lambda error, attempt: 0.0)  # type: ignore[method-assign]

        with STATE.lock:
            STATE.script = [gateway_cut]
            STATE.scripted_calls = 0
        client4.generate_json(one_part, max_attempts=2)
        check("网关切断后自动重试并成功", STATE.scripted_calls == 2, STATE.scripted_calls)

        with STATE.lock:
            STATE.script = [gateway_cut]
            STATE.scripted_calls = 0
        try:
            client4.generate_json(one_part, max_attempts=1)
            raised: Exception | None = None
        except Exception as error:  # noqa: BLE001
            raised = error
        check("网关切断被判为可重试", isinstance(raised, AgentKCallError) and raised.retryable,
              repr(raised))
        check("错误信息点明 300s 同步上限", "300s" in str(raised), str(raised)[:200])

        # 重试业务错误只是把同一份素材再烧一遍，所以必须不重试。
        with STATE.lock:
            STATE.script = [business] * 4
            STATE.scripted_calls = 0
        try:
            client4.generate_json(one_part, max_attempts=4)
            raised = None
        except Exception as error:  # noqa: BLE001
            raised = error
        check("业务错误直接上抛", isinstance(raised, AgentKCallError), repr(raised))
        check("业务错误不重试（只发了 1 个 RPC）", STATE.scripted_calls == 1,
              STATE.scripted_calls)
        check("业务错误保留平台原文便于排查",
              "not available" in str(raised), str(raised)[:200])

        # 非音频阶段（这里是 observe，走主模型）：音频一点也送不进去。
        #
        # 不是因为平台不收——它 2026-09-01 已经收了——而是主模型上约一半的调用会悄悄
        # 把音频在服务端转成文字再交给模型，只有音色/语调/音质/配乐拿不到，报告里
        # 看不出任何异常。送了还不如不送：不送时提示词会明确写"听不到"，判据老实记
        # not_observable；送了则可能拿一份没有听觉依据的结论当真。
        # 音频阶段走的是另一条路，见下一组。
        print("\n[8] 非音频阶段：音频不进 mediaUrls，只在提示词里标明听不到")
        client5, video5, frames5 = build_client(agentk_url, blob_url, workdir)
        clients.append(client5)
        audio = workdir / "reference_voice.mp3"
        audio.write_bytes(b"FAKE-MP3-BYTES" * 64)
        audio_parts = [
            {"text": "参考素材【Audio 1】（audio，声音参考）："},
            {"audio_path": str(audio)},
            {"inline_data": {"mime_type": "image/jpeg", "data": frames5[0]}},
        ]
        with STATE.lock:
            before = len(STATE.invocations)
            uploads_before = len(STATE.puts)
        client5.generate_json(audio_parts, stage="observe")

        called = [cap for cap, _ in STATE.invocations[before:]]
        check("只调 understand_media，不再有转写这一跳", called == ["agentk.understand_media"],
              called)
        check("音频没有被上传到 blob", len(STATE.puts) == uploads_before + 1,
              len(STATE.puts) - uploads_before)

        media_payload = STATE.invocations[before][1]
        check("mediaUrls 里只有那张图，音频不在其中",
              len(media_payload["mediaUrls"]) == 1, media_payload["mediaUrls"])
        asr_prompt = media_payload["prompt"]
        # 音频存在这件事必须说出来。不说的话模型会把音频要求当成没有要求，
        # 相关子判据被判成 pass 而不是 not_observable——不报错、也看不出来。
        check("提示词点出了这段音频的存在", "reference_voice.mp3" in asr_prompt)
        check("说明紧跟在素材标签之后",
              asr_prompt.index("【Audio 1】")
              < asr_prompt.index("【参考音频 reference_voice.mp3】"))
        check("明说了拿不到任何内容", "拿不到这段音频的任何内容" in asr_prompt)
        check("听感判不了这件事写进了 prompt", "not_observable" in asr_prompt)

        # 音频阶段（4c 复听）：换 audio_model，音频本体进 mediaUrls。
        #
        # 这一组盯三件互相牵连的事，缺一条整个音频维度就白做：模型换没换、音频有没有
        # 真送、以及提示词的措辞有没有跟着换。第三条最隐蔽——措辞若还停在"你只能拿到
        # 文字，听感一律记 not_observable"，音频送到了也会被提示词自己压制掉，
        # 而表现只是音频判据依旧未核实，看不出是措辞的锅。
        print("\n[9] 音频阶段：换 audio_model，音频本体进 mediaUrls，措辞跟着换")
        with STATE.lock:
            before3 = len(STATE.invocations)
        client5.generate_json(audio_parts, stage="audio")

        audio_stage_payload = STATE.invocations[before3][1]
        called3 = [cap for cap, _ in STATE.invocations[before3:]]
        check("音频阶段只调 understand_media", called3 == ["agentk.understand_media"],
              called3)
        check("音频阶段换成 audio_model",
              audio_stage_payload["model"] == "gemini-3.1-flash-lite",
              audio_stage_payload["model"])
        check("主模型没被带跑（非音频阶段仍是 pro）",
              media_payload["model"] == "gemini-3.1-pro-preview",
              media_payload["model"])
        check("音频本体进了 mediaUrls（图 + 音频共 2 件）",
              len(audio_stage_payload["mediaUrls"]) == 2,
              audio_stage_payload["mediaUrls"])
        check("送的是 .mp3 地址",
              any(url.endswith(".mp3") for url in audio_stage_payload["mediaUrls"]),
              audio_stage_payload["mediaUrls"])
        audio_stage_prompt = audio_stage_payload["prompt"]
        check("措辞换成「你可以直接听到」",
              "直接听到" in audio_stage_prompt, audio_stage_prompt[-400:])
        check("不再告诉模型「拿不到任何内容」",
              "拿不到这段音频的任何内容" not in audio_stage_prompt)
        # 没有转写就没有逐句时间轴。不写这句，模型会照着习惯报"第 3.2 秒"，
        # 而那个数字实测偏差约 1 秒——一个看起来精确、其实编的证据。
        check("交代了时间只能给大致范围",
              "不要谎报精确到秒" in audio_stage_prompt, audio_stage_prompt[-400:])
        check("瞬态缺陷的边界写进了 prompt（flash-lite 实测 0/8 察觉非语音事件）",
              "瞬态" in audio_stage_prompt, audio_stage_prompt[-400:])

    for item in clients:
        if item._transport is not None:
            item._transport.close()
    agentk_server.shutdown()
    blob_server.shutdown()
    scripted_server.shutdown()
    print()
    if failures:
        print(f"FAILED: {len(failures)} 项 — {failures}")
        return 1
    print("ALL PASSED")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
