"""kess 桥接层：把 agent-k 的输入（URL/blob key/base64）翻译成
ffmpeg kess 服务的 Infer 请求，并把响应映射回契约输出。

阻塞式实现，activity 层用线程包装。协议为 infer.LocalInferService/Infer
（biz_def=mmu，不走 maas 网关），pb2 桩代码 vendor 在本目录 protos/，
源头是 kling-aigc-gpu-service 仓库的 kess_client/protos。
"""

import base64
import json
import os
import threading
import time
import uuid
from dataclasses import dataclass
from urllib.parse import urlsplit

from temporalio.exceptions import ApplicationError

from abi import FfmpegToolInput, InferActivityOutput

SINGLE_SLOTS = ("video", "video2", "audio", "image", "subtitle", "font")
LIST_SLOTS = ("videos", "audios")

# base64 内联上限：kess gRPC 消息上限约 128MB，base64 膨胀 4/3，留余量
DEFAULT_INLINE_LIMIT = 80 * 1024 * 1024


@dataclass(frozen=True)
class KessFfmpegConfig:
    kess_name: str = "maas-agentalgo-klingai-ffmpeg-tool"
    db: str = "upload"
    table: str = "ylab-stunt"
    rpc_timeout: int = 600
    inline_limit: int = DEFAULT_INLINE_LIMIT
    blob_http_base: str = "http://bs3-hb1.internal"
    sso_http_base: str = "https://bs3.corp.kuaishou.com"

    @classmethod
    def from_env(cls) -> "KessFfmpegConfig":
        default = cls()
        return cls(
            kess_name=os.getenv("FFMPEG_KESS_NAME", default.kess_name),
            db=os.getenv("FFMPEG_BLOB_DB", default.db),
            table=os.getenv("FFMPEG_BLOB_TABLE", default.table),
            rpc_timeout=int(
                os.getenv("FFMPEG_RPC_TIMEOUT", str(default.rpc_timeout))
            ),
            inline_limit=int(
                os.getenv("FFMPEG_INLINE_LIMIT", str(default.inline_limit))
            ),
            blob_http_base=os.getenv(
                "BLOBSTORE_HTTP_BASE", default.blob_http_base
            ).rstrip("/"),
            sso_http_base=os.getenv(
                "BLOBSTORE_SSO_BASE", default.sso_http_base
            ).rstrip("/"),
        )


class KessFfmpegClient:
    """阻塞式 ffmpeg kess 客户端，进程内复用一个 GrpcClient。"""

    def __init__(self, config: KessFfmpegConfig) -> None:
        self._config = config
        self._grpc = None
        self._lock = threading.Lock()

    def warm_up(self) -> None:
        """在主线程预建 GrpcClient。

        infra-kess 初始化时注册信号处理器，只能在主线程执行；activity
        跑在线程池里，惰性初始化会报 signal only works in main thread。
        """
        self._client()

    def infer(self, args: FfmpegToolInput) -> InferActivityOutput:
        from protos import infer_pb2

        cfg = self._config
        request_id = f"agentk_{uuid.uuid4().hex[:12]}"
        req = infer_pb2.InferRequest(request_id=request_id)
        req.feature["request_id"].str_val = request_id
        req.feature["start_time"].int_val = int(time.time() * 1000)
        req.feature["db"].str_val = cfg.db
        req.feature["table"].str_val = cfg.table
        if args.op is not None:
            req.feature["op"].str_val = args.op
            req.feature["params"].str_val = json.dumps(
                args.params, ensure_ascii=False
            )
        else:
            req.feature["pipeline"].str_val = json.dumps(
                [
                    {"op": step.op, "params": step.params}
                    for step in args.pipeline
                ],
                ensure_ascii=False,
            )
        for slot in SINGLE_SLOTS:
            value = getattr(args, slot)
            if value:
                req.feature[slot].str_val = self._resolve_media(value, slot)
        for slot in LIST_SLOTS:
            values = getattr(args, slot)
            if values:
                req.feature[slot].str_val = json.dumps(
                    [
                        self._resolve_media(v, f"{slot}[{i}]")
                        for i, v in enumerate(values)
                    ]
                )

        resp = self._client().Infer(req, timeout=cfg.rpc_timeout)

        if resp.feature["error_code"].int_val != 0:
            raise ApplicationError(
                "ffmpeg 服务处理失败: "
                f"error_code={resp.feature['error_code'].int_val} "
                f"error_message={resp.feature['error_message'].str_val[:800]}",
                type="FfmpegInferFailed",
                non_retryable=True,
            )
        return self._parse_response(resp)

    def _client(self):
        with self._lock:
            if self._grpc is None:
                from kess.framework import ClientOption, GrpcClient
                from protos.infer_pb2_grpc import LocalInferServiceStub

                self._grpc = GrpcClient(
                    ClientOption(
                        biz_def="mmu",
                        grpc_service_name=self._config.kess_name,
                        grpc_stub_class=LocalInferServiceStub,
                    )
                )
            return self._grpc

    def _resolve_media(self, value: str, name: str) -> str:
        """URL/blob key/base64 -> kess 服务可接受的输入形态。"""
        v = value.strip()
        if v.startswith("base64:"):
            return v
        if v.startswith(("http://", "https://")):
            key = _url_to_blob_key(v)
            if key is not None:
                return key
            return self._download_as_base64(v, name)
        if v.count(":") == 2 and all(part for part in v.split(":", 2)):
            return v
        raise ApplicationError(
            f"{name} 不是有效的 https URL / blob key / base64: {v[:120]}",
            type="InvalidArgument",
            non_retryable=True,
        )

    def _download_as_base64(self, url: str, name: str) -> str:
        import requests

        try:
            resp = requests.get(url, timeout=120)
            resp.raise_for_status()
        except requests.RequestException as exc:
            raise ApplicationError(
                f"{name} 下载失败: {url[:200]}: {exc}",
                type="MediaDownloadFailed",
            ) from exc
        data = resp.content
        if len(data) > self._config.inline_limit:
            raise ApplicationError(
                f"{name} {len(data)} 字节超过内联上限 "
                f"{self._config.inline_limit}，请改传 blobstore key "
                "（db:table:key）或 bs3 URL",
                type="InvalidArgument",
                non_retryable=True,
            )
        return "base64:" + base64.b64encode(data).decode()

    def _parse_response(self, resp) -> InferActivityOutput:
        cfg = self._config
        kind = None
        blob_key = None
        output_size = None
        for candidate in ("video", "audio", "image"):
            if candidate in resp.feature and resp.feature[candidate].str_val:
                kind = candidate
                blob_key = resp.feature[candidate].str_val
                output_size = resp.feature["output_size"].int_val
                break

        uri = sso_uri = None
        if blob_key is not None:
            db, table, key = blob_key.split(":", 2)
            uri = f"{cfg.blob_http_base}/{db}-{table}/{key}"
            sso_uri = f"{cfg.sso_http_base}/{db}-{table}/{key}"

        result = None
        if "result" in resp.feature and resp.feature["result"].str_val:
            parsed = json.loads(resp.feature["result"].str_val)
            result = parsed if isinstance(parsed, dict) else {"value": parsed}

        return InferActivityOutput(
            kind=kind,
            blobKey=blob_key,
            uri=uri,
            ssoUri=sso_uri,
            outputSize=output_size,
            result=result,
        )


def _url_to_blob_key(url: str) -> str | None:
    """已知 blobstore HTTP 地址直接还原成 db:table:key，避免下载再内联。

    bucket 形如 `{db}-{table}`，按第一个 `-` 拆分；常用 db（upload 等）
    不含 `-`。无法还原时返回 None，调用方走下载内联。
    """
    parts = urlsplit(url)
    host = parts.hostname or ""
    if not (host == "bs3-hb1.internal" or host.startswith(("bs3.", "bs3-"))):
        return None
    path = parts.path.lstrip("/")
    bucket, _, key = path.partition("/")
    if not key:
        return None
    db, sep, table = bucket.partition("-")
    if not sep or not db or not table:
        return None
    return f"{db}:{table}:{key}"
