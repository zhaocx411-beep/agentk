"""本地媒体 → AgentK 可读 URL 的上传层。

AgentK 的 ``agentk.understand_media`` 只接受 HTTP(S) URL（见
``agentk/internal/capability/mediaunderstand/understand_media.schema.json``），
而质检 pipeline 手里全是本地文件（整段 mp4、抽出来的高清帧、参考图/音频）。
这一层把本地字节搬到对象存储上换成 URL。

协议与 comic-drama 的共享上传保持一致（``platform/chat/upload.go``）：
匿名 ``PUT {endpoint}/{bucket}/{key}``，配了 CDN base 就返回 CDN 直链。

URL 只需要**从 AgentK 服务端**可达，不必是公网地址：``understand_media`` 对认不出
的 URL 按外部直链处理，由 AgentK 自己下载再内联（``fetcher.one`` → ``inlineFromURL``，
该路径不校验内网地址）。所以内网对象存储域名可以直接用，也不需要和 AgentK 侧的
``AGENTK_BLOB_*`` 对齐 —— 那个只决定 URL 能否被认成受管 artifact 而走「上游自取
直链」的优化，该优化对本通道没有收益。

去重是这里的关键设计：一次质检要对同一段视频发起 8~9 次模型调用，逐次上传会
把整个流程拖垮。key 用内容哈希生成，同一进程内还有一层 URL 备忘缓存，
所以同一份字节在一次 run 里只会真正上传一次。
"""

from __future__ import annotations

import hashlib
import os
import threading
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path

from aigc_eval.providers.media_io import guess_mime as _guess_mime


# MIME 表在 providers/media_io.py，此处只是转导：对象存储的兜底值必须是
# application/octet-stream（写错 Content-Type 会让上游模型拒识媒体类型，宁可暴露），
# 与内联编码那边的 image/jpeg 兜底不同，所以兜底值在调用点给。
def guess_mime(suffix: str) -> str:
    return _guess_mime(suffix, default="application/octet-stream")


class BlobUnavailable(RuntimeError):
    """对象存储没配置或不可用。调用方据此决定回退到直连 provider。"""


@dataclass(frozen=True)
class BlobConfig:
    endpoint: str
    bucket: str
    cdn_base: str = ""
    timeout_seconds: float = 300.0
    key_prefix: str = "aigc-eval/uploads"

    @property
    def configured(self) -> bool:
        return bool(self.endpoint and self.bucket)


def load_blob_config() -> BlobConfig:
    return BlobConfig(
        endpoint=os.environ.get("AIGC_EVAL_BLOB_ENDPOINT", "").strip(),
        bucket=os.environ.get("AIGC_EVAL_BLOB_BUCKET", "").strip(),
        cdn_base=os.environ.get("AIGC_EVAL_BLOB_CDN_BASE", "").strip(),
        timeout_seconds=float(os.environ.get("AIGC_EVAL_BLOB_TIMEOUT", "300")),
        key_prefix=os.environ.get("AIGC_EVAL_BLOB_PREFIX", "aigc-eval/uploads").strip("/"),
    )


class BlobStore:
    """内容寻址的对象上传。线程安全：批量模式下多个 case 并发共用一个实例。"""

    def __init__(self, config: BlobConfig | None = None):
        self.config = config or load_blob_config()
        if not self.config.configured:
            raise BlobUnavailable(
                "对象存储未配置：请设置 AIGC_EVAL_BLOB_ENDPOINT 与 AIGC_EVAL_BLOB_BUCKET"
            )
        self._urls: dict[str, str] = {}
        self._lock = threading.Lock()
        self.uploaded_bytes = 0
        self.upload_count = 0

    def object_url(self, key: str) -> str:
        return (
            self.config.endpoint.rstrip("/")
            + "/"
            + self.config.bucket.strip("/")
            + "/"
            + key.lstrip("/")
        )

    def public_url(self, key: str) -> str:
        if self.config.cdn_base:
            return self.config.cdn_base.rstrip("/") + "/" + key.lstrip("/")
        return self.object_url(key)

    def put_bytes(self, data: bytes, *, suffix: str) -> str:
        """上传字节并返回可读 URL。同一份内容重复调用只上传一次。"""
        digest = hashlib.sha256(data).hexdigest()
        cache_key = f"{digest}{suffix.lower()}"
        with self._lock:
            cached = self._urls.get(cache_key)
        if cached:
            return cached

        key = f"{self.config.key_prefix}/{digest[:2]}/{digest}{suffix.lower()}"
        request = urllib.request.Request(
            self.object_url(key),
            data=data,
            method="PUT",
            headers={
                "Content-Type": guess_mime(suffix),
                "Content-Length": str(len(data)),
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=self.config.timeout_seconds) as response:
                status = response.status
        except urllib.error.HTTPError as error:
            detail = error.read(512).decode("utf-8", "replace").strip()
            raise BlobUnavailable(f"上传失败 HTTP {error.code}：{detail}") from error
        except urllib.error.URLError as error:
            raise BlobUnavailable(f"上传失败（网络）：{error.reason}") from error
        if status not in (200, 201, 204):
            raise BlobUnavailable(f"上传失败 HTTP {status}")

        url = self.public_url(key)
        with self._lock:
            self._urls[cache_key] = url
            self.uploaded_bytes += len(data)
            self.upload_count += 1
        return url

    def put_file(self, path: Path) -> str:
        """上传文件并返回**完整可读 URL**。

        返回值已经是 URL，不是 key——不要再拿它去调 ``public_url()`` / ``object_url()``。
        那样会拼出 ``{endpoint}/{bucket}/https://{endpoint}/{bucket}/...`` 这种双前缀地址，
        而且不会报错：上传是成功的，只是报告里那条链接打不开。真出过一次。
        """
        path = Path(path)
        return self.put_bytes(path.read_bytes(), suffix=path.suffix)

    def put_at(self, key: str, data: bytes, *, content_type: str) -> str:
        """按调用方指定的 key 上传（不做内容寻址）。

        与 ``put_bytes`` 的区别在于 key 的来源：那边用内容哈希，适合"同一份字节只传
        一次"的素材去重；这边由调用方决定 key，适合检查点这种"按请求哈希寻址、
        内容是响应"的场景——写入的字节和 key 之间没有哈希关系。
        """
        request = urllib.request.Request(
            self.object_url(key),
            data=data,
            method="PUT",
            headers={"Content-Type": content_type, "Content-Length": str(len(data))},
        )
        try:
            with urllib.request.urlopen(request, timeout=self.config.timeout_seconds) as response:
                status = response.status
        except urllib.error.HTTPError as error:
            detail = error.read(512).decode("utf-8", "replace").strip()
            raise BlobUnavailable(f"上传失败 HTTP {error.code}：{detail}") from error
        except urllib.error.URLError as error:
            raise BlobUnavailable(f"上传失败（网络）：{error.reason}") from error
        if status not in (200, 201, 204):
            raise BlobUnavailable(f"上传失败 HTTP {status}")
        with self._lock:
            self.uploaded_bytes += len(data)
            self.upload_count += 1
        return self.public_url(key)

    def exists(self, key: str) -> bool:
        """对象是否已在。用 HEAD 而不是 GET：判断存在不该把整个对象拉回来。

        网络异常一律当"不在"处理——多传一次的代价远小于误判成已存在、导致后续
        activity 在另一台实例上取不到产物。
        """
        request = urllib.request.Request(self.object_url(key), method="HEAD")
        try:
            with urllib.request.urlopen(request, timeout=self.config.timeout_seconds) as response:
                return response.status == 200
        except urllib.error.HTTPError as error:
            return error.code == 200
        except urllib.error.URLError:
            return False

    def get_bytes(self, key: str) -> bytes | None:
        """读回对象；不存在返回 None。

        404 是正常结果而不是错误——检查点未命中就走这条路，所以它和"网络挂了"
        必须区分开：前者返回 None 让调用方照常跑，后者抛 BlobUnavailable。
        """
        request = urllib.request.Request(self.object_url(key), method="GET")
        try:
            with urllib.request.urlopen(request, timeout=self.config.timeout_seconds) as response:
                if response.status != 200:
                    return None
                return response.read()
        except urllib.error.HTTPError as error:
            if error.code in (403, 404):
                # 私有桶把不存在读成 403，两者都当"没有"处理。
                return None
            detail = error.read(512).decode("utf-8", "replace").strip()
            raise BlobUnavailable(f"读取失败 HTTP {error.code}：{detail}") from error
        except urllib.error.URLError as error:
            raise BlobUnavailable(f"读取失败（网络）：{error.reason}") from error

    def stats(self) -> dict[str, int]:
        with self._lock:
            return {
                "uploads": self.upload_count,
                "uploaded_bytes": self.uploaded_bytes,
                "deduped_objects": len(self._urls),
            }


# 进程级共享实例。住在这里而不是 agentk.py，是因为 worker 的 report activity 也要
# 用它上传报告，而那条路径不该为了拿个对象存储去 import 一个模型通道。
_shared_blob_lock = threading.Lock()
_shared_blob: BlobStore | None = None


def shared_blob_store() -> BlobStore:
    """进程级共用的上传器。

    批量模式下多个 case 并发跑，共用一个实例才能让内容寻址的去重生效
    （同一段参考视频在不同 case 里只上传一次）。
    """
    global _shared_blob
    with _shared_blob_lock:
        if _shared_blob is None:
            _shared_blob = BlobStore()
        return _shared_blob
