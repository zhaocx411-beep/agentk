"""走 AgentK 平台的媒体理解客户端。

对外契约与 ``GeminiClient`` 完全一致——只暴露
``generate_json(parts, system_instruction=..., ...) -> dict``。judge_pipeline 里
的各个 stage 类都是鸭子类型依赖这个方法，所以质检判定逻辑无需任何改动。

内部把一次模型调用翻译成一次 ``InvokeCapability`` 直调（官方 Client SDK，
传输与鉴权都在 agentk_transport.py 里）：有素材走 ``agentk.understand_media``，
没素材走 ``agentk.generate_text``。传输层的边界（网关 300s 同步上限、
鉴权只认环境变量里的服务名）见那个模块的文档。

与直连 Gemini 的三个结构性差异，都在这里被吸收掉：

1. ``understand_media`` 只收 HTTP URL，不收 base64 → 媒体先经 blob_store 上传。
2. 没有 systemInstruction 字段 → 并入 prompt。
3. 没有 responseMimeType=application/json → 靠提示词约束 + 复用
   ``extract_json_object`` 的修复级联兜底。
4. **独立音频文件**只有走 audio_model 的阶段才送得进 ``mediaUrls``；其他模型上
   这段音频完全到不了模型，提示词里必须明说"这次听不到"。视频**自带的音轨**不受
   此限，它随 mp4 一起进了模型。两种情形的措辞相反，见 agentk_audio.py。
"""

from __future__ import annotations

import base64
import json
import time
from pathlib import Path
from typing import Any

from aigc_eval.config import AUDIO_STAGES, UNDERSTAND_MEDIA_MODELS, AgentKConfig
from aigc_eval.providers.agentk_audio import (
    AUDIBLE_AUDIO_NOTE,
    INAUDIBLE_AUDIO_NOTE,
)
from aigc_eval.providers.agentk_transport import (
    AgentKCallError,
    AgentKTransport,
    transport_for,
)
from aigc_eval.providers.blob_store import BlobStore, BlobUnavailable, shared_blob_store
from aigc_eval.providers.json_parse import extract_json_object
from aigc_eval.providers.media_io import part_modalities
from aigc_eval.providers.model_call import (
    GENERATE_TEXT_CAPABILITY,
    UNDERSTAND_MEDIA_CAPABILITY,
    ModelOutcome,
    ModelRequest,
    ModelResponse,
)
from aigc_eval.trace_store import TraceSession

# inline_data 的 base64 要落地成文件才能上传，后缀按 MIME 反查。
_SUFFIX_BY_MIME = {
    "image/jpeg": ".jpg",
    "image/png": ".png",
    "image/webp": ".webp",
    "image/gif": ".gif",
    "video/mp4": ".mp4",
    "video/quicktime": ".mov",
    "video/webm": ".webm",
    "audio/mpeg": ".mp3",
    "audio/wav": ".wav",
    "audio/mp4": ".m4a",
    "audio/aac": ".aac",
    "audio/flac": ".flac",
    "audio/ogg": ".ogg",
}

_JSON_HINT = (
    "\n\n【输出格式】只输出一个 JSON 对象，不要输出任何解释文字、前后缀或 "
    "markdown 代码块标记。"
)

class AgentKClient:
    # 素材件数上限作为**能力声明**对外暴露，而不只是内部裁剪阈值。
    # `_select_media` 的降载是最后一道安全网，但它发生在提示词已经写好之后：
    # 提示词说「以下 16 张高清帧」而实际只到 9 张，模型会把缺失的 7 张当成
    # 「看不到」而不是「没给」，于是凭空多出一批 not_observable。所以调用方必须
    # 能在组装素材**之前**读到上限，按上限规划帧数，让提示词与载荷始终一致。
    supports_video = True

    def __init__(
        self,
        config: AgentKConfig,
        trace: TraceSession | None = None,
        on_event: Any | None = None,
        blob: BlobStore | None = None,
        transport: AgentKTransport | None = None,
        invoker: Any | None = None,
    ):
        if not config.configured:
            raise ValueError(
                "AgentK 未配置：需要 KWS_SERVICE_NAME（天琴服务名，如 "
                "klingai-agentk-aigc-eval-api）"
            )
        if config.model not in UNDERSTAND_MEDIA_MODELS:
            raise ValueError(
                f"AGENTK_UNDERSTAND_MODEL={config.model} 不在 understand_media 支持的模型内："
                f"{'、'.join(UNDERSTAND_MEDIA_MODELS)}"
            )
        # 音频模型同样要过 enum：它虽有默认值，但 AGENTK_AUDIO_MODEL 能覆盖，
        # 而填错的表现是每次音频调用都被平台拒——放到构造时炸掉便宜得多。
        if config.audio_model not in UNDERSTAND_MEDIA_MODELS:
            raise ValueError(
                f"AGENTK_AUDIO_MODEL={config.audio_model} 不在 understand_media 支持的模型内："
                f"{'、'.join(UNDERSTAND_MEDIA_MODELS)}"
            )
        self.config = config
        self.max_media = max(1, config.max_media)
        self.trace = trace
        self.on_event = on_event
        self.usage: list[dict[str, Any]] = []
        self._blob = blob
        self._transport = transport
        self._invoker: Any = invoker
        # 记录降载与上传情况，写进报告便于回溯为什么某次判定素材变少了。
        self.media_notes: list[dict[str, Any]] = []

    # ---------- 基础设施 ----------

    @property
    def blob(self) -> BlobStore:
        if self._blob is None:
            self._blob = shared_blob_store()
        return self._blob

    @property
    def transport(self) -> AgentKTransport:
        # 延迟取：mock 或提前失败的场景不该建出连接和事件循环线程。
        if self._transport is None:
            self._transport = transport_for(self.config)
        return self._transport

    @property
    def invoker(self) -> Any:
        """真正把请求发出去的那一段。

        默认是 ``HttpInvoker``（studio 与批处理走的同步直调）。worker 路径不会走到
        这里——那条路上 invoke 发生在 workflow 里，``model_relay.SuspendingClient``
        在 ``build_request`` 之后就把请求抛出去了，永远不会调 ``generate_json``。
        测试可以注入一个假 invoker，比假整个 client 干净得多。
        """
        if self._invoker is None:
            self._invoker = HttpInvoker(
                self.transport, timeout=self.config.timeout_seconds
            )
        return self._invoker

    # ---------- 素材翻译 ----------

    @staticmethod
    def _media_kind(part: dict[str, Any]) -> str | None:
        """判断一个 part 是哪种媒体；不是媒体则返回 None。"""
        if part.get("video_path"):
            return "video"
        if part.get("audio_path"):
            return "audio"
        for key in ("inline_data", "inlineData"):
            blob = part.get(key)
            if isinstance(blob, dict) and blob.get("data"):
                mime = str(blob.get("mime_type") or blob.get("mimeType") or "")
                if mime.startswith("video/"):
                    return "video"
                if mime.startswith("audio/"):
                    return "audio"
                return "image"
        for key in ("file_data", "fileData"):
            blob = part.get(key)
            if isinstance(blob, dict) and (blob.get("file_uri") or blob.get("fileUri")):
                return "remote"
        return None

    def _select_media(self, kinds: list[tuple[int, str]]) -> tuple[set[int], dict[str, Any] | None]:
        """在 mediaUrls 上限内挑选素材。

        understand_media 的 schema 写死 maxItems=10，而合并扫描默认送「整段视频 +
        16 张高清帧」共 17 件，必然超限。这里不让整条 case 失败，而是保留全部
        视频/音频（信息密度最高），对图片做均匀采样——均匀采样比截断前 N 张更能
        保住时间轴覆盖度。降载事实会写进 notice、usage 和 trace，可审计。
        """
        cap = max(1, self.config.max_media)
        if len(kinds) <= cap:
            return {index for index, _ in kinds}, None

        priority = [index for index, kind in kinds if kind in ("video", "audio", "remote")]
        images = [index for index, kind in kinds if kind == "image"]
        keep = set(priority[:cap])
        budget = cap - len(keep)
        if budget > 0 and images:
            if len(images) <= budget:
                keep.update(images)
            else:
                # 均匀取样，含首尾。取整可能撞车导致少填，之后按顺序补齐，
                # 保证真的用满预算——白扔掉可用的素材配额是纯损失。
                step = (len(images) - 1) / (budget - 1) if budget > 1 else 0
                picked: set[int] = {images[round(i * step)] for i in range(budget)}
                for index in images:
                    if len(picked) >= budget:
                        break
                    picked.add(index)
                keep.update(picked)
        note = {
            "reason": "media_cap",
            "cap": cap,
            "requested": len(kinds),
            "kept": len(keep),
            "dropped_images": len(images) - len(keep.intersection(images)),
        }
        return keep, note

    def model_for(self, stage: str) -> str:
        """这个阶段该用哪个 understand_media 模型。

        音频阶段单独换模型不是调优，是那三个子判据能否成立的前提：pro 上约一半的
        音频调用会悄悄退回服务端转写（详见 config.AgentKConfig.audio_model）。
        """
        return (
            self.config.audio_model
            if (stage or "").strip() in AUDIO_STAGES
            else self.config.model
        )

    def _upload_part(self, part: dict[str, Any], kind: str) -> str:
        if kind == "remote":
            for key in ("file_data", "fileData"):
                blob = part.get(key)
                if isinstance(blob, dict):
                    uri = blob.get("file_uri") or blob.get("fileUri")
                    if uri:
                        return str(uri)
            raise RuntimeError("file_data 缺少 file_uri")
        if part.get("video_path"):
            return self.blob.put_file(Path(str(part["video_path"])))
        if part.get("audio_path"):
            return self.blob.put_file(Path(str(part["audio_path"])))
        for key in ("inline_data", "inlineData"):
            blob = part.get(key)
            if isinstance(blob, dict) and blob.get("data"):
                mime = str(blob.get("mime_type") or blob.get("mimeType") or "image/jpeg")
                suffix = _SUFFIX_BY_MIME.get(mime, ".bin")
                return self.blob.put_bytes(base64.b64decode(blob["data"]), suffix=suffix)
        raise RuntimeError(f"无法识别的媒体 part：{list(part)}")

    # ---------- 音频措辞 ----------

    def _audio_chunk(self, audio_path: Path, *, native: bool) -> str:
        """独立音频文件 → 一段说明这次能不能听见的文字。

        ``native`` 说的是这段音频**本体是否进了 mediaUrls**（由调用这次的模型决定，
        见 build_request）。两种措辞的方向相反，选错都是静默错误：说成听得见会让模型
        编一个没有听觉依据的结论，说成听不见会让它放弃一个手上就有的能力。

        听不见时也必须说出"有这么一段音频"。不提的话模型会把音频要求当成没有要求，
        相关子判据被判成 pass 而不是 not_observable——一个不报错、看不出来的错误结论。
        """
        note = AUDIBLE_AUDIO_NOTE if native else INAUDIBLE_AUDIO_NOTE
        return f"【参考音频 {audio_path.name}】\n{note}"

    def _translate(
        self,
        parts: list[dict[str, Any]],
        system_instruction: str | None,
        *,
        audio_native: bool,
    ) -> tuple[list[str], str, dict[str, Any] | None]:
        """把 Gemini 形状的 parts 拆成 (mediaUrls, prompt, 降载说明)。

        understand_media 把媒体和文字彻底分开，原本「文字—图—文字—图」的交错
        语义会丢失。这里在 prompt 里按顺序插入「【素材 N】」锚点并附素材清单，
        让模型仍然能把某段描述和某件素材对应起来。

        ``audio_native`` 决定独立音频文件走哪条路：进 mediaUrls（模型真听得到），
        还是只留一句"这次听不到"的说明。由 ``build_request`` 按这次调用用哪个模型
        算出来——不是本方法能判断的事，也不该由它猜。差别见 agentk_audio 的模块说明。
        """
        kinds: list[tuple[int, str]] = []
        for index, part in enumerate(parts):
            if not isinstance(part, dict):
                continue
            kind = self._media_kind(part)
            if kind:
                kinds.append((index, kind))

        if not audio_native:
            # 这次调用的模型不能可靠地拿到音频本体（见 config 里 audio_model 的说明），
            # 所以音频根本不上传。它因此也不占素材配额，降载只对视频与图片生效。
            kinds = [(index, kind) for index, kind in kinds if kind != "audio"]

        keep, note = self._select_media(kinds)

        media_urls: list[str] = []
        manifest: list[str] = []
        chunks: list[str] = []
        if system_instruction:
            chunks.append(system_instruction.strip())

        for index, part in enumerate(parts):
            if not isinstance(part, dict):
                continue
            kind = self._media_kind(part)
            if kind is None:
                text = part.get("text")
                if isinstance(text, str) and text.strip():
                    chunks.append(text)
                continue
            if kind == "audio" and not audio_native:
                # 紧跟在上一条文本之后：那条文本正是素材标签（"参考素材【Audio 1】…"），
                # 说明必须挨着它，模型才知道这段话说的是哪一件素材。
                chunks.append(
                    self._audio_chunk(Path(str(part["audio_path"])), native=False)
                )
                continue
            if index not in keep:
                continue
            if kind == "audio":
                chunks.append(
                    self._audio_chunk(Path(str(part["audio_path"])), native=True)
                )
            url = self._upload_part(part, kind)
            media_urls.append(url)
            ordinal = len(media_urls)
            label = {"video": "视频", "audio": "音频", "image": "图片", "remote": "素材"}[kind]
            chunks.append(f"【素材 {ordinal}｜{label}】")
            manifest.append(f"{ordinal}. {label}")

        if manifest:
            chunks.append(
                "【素材清单】按以下顺序送入模型，正文中的「【素材 N】」与之一一对应：\n"
                + "\n".join(manifest)
            )
        if note:
            chunks.append(
                f"【素材裁剪说明】受平台单次调用素材上限（{note['cap']} 件）限制，"
                f"本次从 {note['requested']} 件中均匀保留 {note['kept']} 件。"
                "请只依据实际收到的素材作判断，不要臆测被省略的内容。"
            )
        if self.config.enforce_json_hint:
            chunks.append(_JSON_HINT.strip())

        return media_urls, "\n\n".join(chunks), note

    # ---------- build：组装请求 ----------

    def build_request(
        self,
        parts: list[dict[str, Any]],
        *,
        system_instruction: str | None = None,
        stage: str = "agentk_call",
        title: str = "AgentK understand_media",
        digest: str = "",
        attempt: int = 1,
    ) -> ModelRequest:
        """组装素材、上传对象存储、拼提示词，产出一个可直接发出去的请求描述符。

        这一段是纯粹的"我们自己的活"：翻译 parts、上传 blob、写提示词锚点。它必须留在
        activity 里（要读文件、要发 HTTP 上传），只有它产出的描述符才跨到 workflow。

        「拆解提示词」这类环节没有素材，而 understand_media 的 mediaUrls 是
        minItems=1，走不通。这类调用改派 generate_text——它的 model 走 ChatClient
        查表，所以用 chat_model 而不是 understand_media 的多模态模型。
        """
        # 模型先定、素材后拼：音频本体能不能进 mediaUrls 取决于用哪个模型，
        # 反过来则无从判断（见 config 里 audio_model 的说明）。
        model = self.model_for(stage)
        media_urls, prompt, note = self._translate(
            parts, system_instruction, audio_native=model == self.config.audio_model
        )
        if note:
            self.media_notes.append({"stage": stage, "title": title, **note})
            self._notice(
                f"{title}：素材超过平台单次上限，{note['requested']} 件裁剪为 {note['kept']} 件"
            )
        if media_urls:
            capability = UNDERSTAND_MEDIA_CAPABILITY
            payload: dict[str, Any] = {
                "model": model,
                "mediaUrls": media_urls,
                "prompt": prompt,
            }
        else:
            capability = GENERATE_TEXT_CAPABILITY
            payload = {"model": self.config.chat_model, "prompt": prompt}
        return ModelRequest(
            capabilityId=capability,
            payload=payload,
            digest=digest,
            stage=stage,
            title=title,
            attempt=attempt,
        )

    # ---------- consume：解析响应 ----------

    def consume_response(
        self,
        request: ModelRequest,
        response: ModelResponse,
        *,
        parts: list[dict[str, Any]],
        system_instruction: str | None = None,
        temperature: float = 0.1,
        started: float | None = None,
    ) -> ModelOutcome:
        """解析响应、记 usage 与 trace，报告这次到底成没成。

        失败时**不抛**，返回 ``ok=False``。是否重试由上层按 ``max_attempts`` 决定：
        解析失败意味着要重新发起一次模型调用，而在 worker 路径上响应已经落进
        workflow 历史，Temporal 不会替你重来，必须由 workflow 显式再调一次。

        失败时也不写 trace。终态失败的 trace 由 ``record_failure`` 写，因为"这是不是
        最后一次"只有上层知道；在这里写会让每次重试都留下一条 error 步骤，报告里
        凭空多出几条失败记录。
        """
        started = time.perf_counter() if started is None else started
        attempt = request.attempt
        titled = request.title if attempt == 1 else f"{request.title}（重试 {attempt}）"

        if response.error is not None:
            # 传输/平台侧失败：今天这条路上不 append usage（usage 是在拿到输出之后才
            # 记的），保持一致，否则成本报告里会多出没发生的调用。
            return ModelOutcome(
                ok=False,
                error=response.error,
                errorCode=response.errorCode,
                retryable=response.errorRetryable,
            )

        text_only = request.capabilityId == GENERATE_TEXT_CAPABILITY
        self.usage.append(
            {
                "stage": request.stage,
                "title": titled,
                "attempt": attempt,
                "ok": True,
                "elapsed_ms": response.elapsedMs,
                # 平台没回 model 时的兜底要按阶段取，否则音频调用会被记成主模型，
                # 成本报告和"这条结论是谁给的"都会归错账。
                "model": str(
                    response.model
                    or (
                        self.config.chat_model
                        if text_only
                        else self.model_for(request.stage)
                    )
                ),
                **part_modalities(parts),
                # understand_media 的返回里没有 token 用量
                # （CapabilityOutput 只有 text/model/params），
                # 所以这里给 0 并打标，避免下游把缺失当成真实的零成本。
                "prompt_tokens": 0,
                "output_tokens": 0,
                "thoughts_tokens": 0,
                "total_tokens": 0,
                "prompt_tokens_by_modality": {},
                "tokens_unavailable": True,
                "media_sent": len(request.payload.get("mediaUrls") or []),
                "agentk_params": response.params or {},
            }
        )

        raw_text = response.rawText
        if not raw_text:
            return ModelOutcome(
                ok=False, error="AgentK 返回空文本", retryable=True
            )
        try:
            parsed = extract_json_object(raw_text)
        except Exception as error:  # noqa: BLE001 - 解析失败要重发，不是终态
            return ModelOutcome(
                ok=False,
                error=str(error),
                retryable=self._is_retryable(error),
            )

        if self.trace is not None:
            self.trace.record_step(
                stage=request.stage,
                title=titled,
                system_instruction=system_instruction,
                parts=parts,
                raw_output=raw_text,
                parsed_output=parsed,
                started_at=started,
                # understand_media 不接受 temperature；记录调用方的期望值
                # 只为对齐另外两个 provider 的 trace 形状。
                temperature=temperature,
                source="agentk",
            )
        return ModelOutcome(ok=True, parsed=parsed)

    def record_failure(
        self,
        request: ModelRequest,
        *,
        parts: list[dict[str, Any]],
        system_instruction: str | None,
        temperature: float,
        detail: str,
        raw_text: str | None,
        started: float,
        max_attempts: int,
    ) -> None:
        """终态失败时补一条 trace。形状与原来 generate_json 的 except 分支一致。"""
        if self.trace is None:
            return
        self.trace.record_step(
            stage=request.stage,
            title=request.title,
            system_instruction=system_instruction,
            parts=parts,
            raw_output=raw_text or None,
            parsed_output=None,
            error=f"{detail}（尝试 {request.attempt}/{max_attempts}）",
            started_at=started,
            temperature=temperature,
            source="agentk",
        )

    # ---------- 重试策略 ----------

    @staticmethod
    def _is_retryable(error: Exception) -> bool:
        """与 GeminiClient 保持同一套口径：解析失败和瞬时故障值得重试，
        永久性的客户端错误不重试。上传不可用属于配置问题，直接抛给调用方
        让它回退到别的 provider。

        传输层故障不在这里按错误信息 match：``AgentKCallError`` 自带
        ``retryable``（由 Connect 错误码判定）。原先靠 "HTTP 429" 之类的措辞
        分流，换一次传输层就会静默失效。
        """
        if isinstance(error, BlobUnavailable):
            return False
        if isinstance(error, AgentKCallError):
            return error.retryable
        if isinstance(error, (ValueError, json.JSONDecodeError)):
            return True
        if isinstance(error, TimeoutError):
            return True
        message = str(error)
        return "空文本" in message or "输出形状异常" in message

    @staticmethod
    def _backoff_seconds(code: str, attempt: int) -> float:
        """退避时长。入参从异常改成错误码：切成三段之后，做重试决定的地方手里
        只有 ``ModelOutcome``，没有原始异常对象了。口径一个字没变。"""
        if code in ("resource_exhausted", "unavailable"):
            return min(45.0, 6.0 * attempt)
        return min(8.0, 2.0 * attempt)

    @staticmethod
    def _retry_reason(code: str) -> str:
        if code == "resource_exhausted":
            return "限流"
        if code == "unavailable":
            return "服务不可用"
        if code == "deadline_exceeded":
            return "执行超时"
        return "输出解析失败"

    def _notice(self, detail: str, level: str = "warn") -> None:
        if self.on_event is None:
            return
        try:
            self.on_event({"type": "notice", "level": level, "detail": detail})
        except Exception:  # noqa: BLE001 - 通知失败不该影响判定
            pass

    # ---------- 对外接口 ----------

    def generate_json(
        self,
        parts: list[dict[str, Any]],
        *,
        system_instruction: str | None = None,
        temperature: float = 0.1,
        stage: str = "agentk_call",
        title: str = "AgentK understand_media",
        max_attempts: int = 4,
        max_output_tokens: int | None = None,
    ) -> dict[str, Any]:
        # max_output_tokens 收下但用不上：understand_media 的输入只有
        # model / mediaUrls / prompt，平台没有暴露输出长度控制。参数保留是因为
        # 各 stage 按鸭子类型共用同一个方法签名，少一个键就会 TypeError；而这里
        # 若改成静默丢弃以外的行为（比如报错），走 agentk 的 4b 直接不可用。
        #
        # 代价是真实的：4b 要一次吐出八维 × 28 子判据，输出长度靠平台默认值兜着，
        # 一旦被截断只能表现为「JSON 不闭合」，无法像直连那样抬预算规避。
        _ = max_output_tokens
        started = time.perf_counter()
        invoker = self.invoker

        # 上传只做一次：重试是为了让模型重新解码，素材没有变化，
        # 而且 blob 是内容寻址的，重复调用也不会真的重传。
        request = self.build_request(
            parts,
            system_instruction=system_instruction,
            stage=stage,
            title=title,
        )
        outcome = ModelOutcome(ok=False, error="generate_json 未产生结果")
        raw_text = ""

        for attempt in range(1, max_attempts + 1):
            attempted = request.model_copy(update={"attempt": attempt})
            response = invoker.invoke(attempted)
            raw_text = response.rawText or raw_text
            outcome = self.consume_response(
                attempted,
                response,
                parts=parts,
                system_instruction=system_instruction,
                temperature=temperature,
                started=started,
            )
            if outcome.ok and outcome.parsed is not None:
                return outcome.parsed
            if attempt < max_attempts and outcome.retryable:
                wait = self._backoff_seconds(outcome.errorCode, attempt)
                self._notice(
                    f"{title}：{self._retry_reason(outcome.errorCode)}，{wait:.0f}s 后重试"
                    f"（{attempt + 1}/{max_attempts}）"
                )
                time.sleep(wait)
                continue
            break

        self.record_failure(
            request.model_copy(update={"attempt": min(max_attempts, request.attempt)}),
            parts=parts,
            system_instruction=system_instruction,
            temperature=temperature,
            detail=outcome.error or "未知错误",
            raw_text=raw_text,
            started=started,
            max_attempts=max_attempts,
        )
        raise _as_exception(outcome)


# ===========================================================================
# invoke 段：把请求真正发出去。
#
# studio 与批处理用 HttpInvoker（同步直调，行为与改造前逐字节一致）；worker 路径
# 不用这里的任何东西——那条路上 invoke 是 workflow 里的 invoke_capability。
# ===========================================================================


class HttpInvoker:
    """经网关同步直调一次 capability。

    这是改造前 ``_invoke`` 那几行的全部内容，只是被挪到了独立的一段。**网关对同步
    调用的 300 秒上限只作用在这条路径上**（见 agentk_transport.py），worker 路径走
    Temporal 不受它约束——所以那条措辞不要再当成全局边界。
    """

    def __init__(self, transport: AgentKTransport, *, timeout: float) -> None:
        self._transport = transport
        self._timeout = timeout

    def invoke(self, request: ModelRequest) -> ModelResponse:
        started = time.perf_counter()
        try:
            output = self._transport.invoke(
                request.capabilityId, request.payload, timeout=self._timeout
            )
        except AgentKCallError as error:
            return ModelResponse.from_error(
                str(error),
                code=error.code,
                retryable=error.retryable,
                elapsed_ms=_elapsed_ms(started),
            )
        except BlobUnavailable as error:
            # 上传不可用是配置问题，重试没有意义，交回给调用方换 provider。
            return ModelResponse.from_error(str(error), retryable=False)
        if not isinstance(output, dict):
            return ModelResponse.from_error(
                f"AgentK {request.capabilityId} 输出形状异常：{output!r}",
                retryable=True,
                elapsed_ms=_elapsed_ms(started),
            )
        return ModelResponse.from_output(
            request.capabilityId, output, elapsed_ms=_elapsed_ms(started)
        )


def _elapsed_ms(started: float) -> int:
    return int((time.perf_counter() - started) * 1000)


def _as_exception(outcome: ModelOutcome) -> Exception:
    """把终态失败还原成异常。

    judge 侧有几处对模型失败是**降级**处理的（4c 疑点复核失败保留 4b 主判），它们靠
    ``except Exception`` 接住，所以这里必须抛而不是返回——把失败改成返回值会让那些
    降级分支永远不执行，报告内容随之改变。
    """
    detail = outcome.error or "AgentK 调用失败"
    if outcome.errorCode:
        return AgentKCallError(
            detail, code=outcome.errorCode, retryable=outcome.retryable
        )
    return RuntimeError(detail)
