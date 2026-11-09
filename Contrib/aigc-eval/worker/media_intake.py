"""把入参里的素材 URL 落成 worker 本地文件。

质检链路全程按本地路径工作（ffprobe 抽帧、SSIM 对齐、拼图都要真实文件），
所以入口必须先把 URL 取到本地。这里只用标准库，不引第三方 HTTP 依赖。

安全上有两条硬约束，都是为了别让入参决定 worker 往哪读写：

* 只允许 http/https；``file://`` 之类一律拒绝，否则调用方可以让 worker 读本机任意文件。
* 落盘文件名由我们自己生成（内容序号 + 从 URL 推断的后缀），不采用 URL 里的
  文件名，避免 ``../`` 之类的路径逃逸。
"""

from __future__ import annotations

import shutil
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

# 单个素材的下载上限。质检的输入是短视频，正常在几十 MB 量级；给到 512MB
# 是为了不误伤长素材，同时挡住把 worker 磁盘写满的输入。
MAX_BYTES = 512 * 1024 * 1024
_TIMEOUT_SECONDS = 120

_VIDEO_SUFFIXES = {".mp4", ".mov", ".webm", ".mkv", ".avi", ".m4v"}
_IMAGE_SUFFIXES = {".jpg", ".jpeg", ".png", ".webp", ".bmp"}


class MediaIntakeError(RuntimeError):
    """素材取不下来。属于入参问题，不该重试。"""


def _suffix_from_url(url: str, *, fallback: str) -> str:
    """从 URL 路径推断后缀。

    只认白名单后缀：下游 ffprobe 与 mime 推断都按后缀走，接受任意字符串会让
    一个伪装成 ``.sh`` 的链接落到磁盘上。
    """
    path = urllib.parse.urlparse(url).path
    suffix = Path(path).suffix.lower()
    if suffix in _VIDEO_SUFFIXES or suffix in _IMAGE_SUFFIXES:
        return suffix
    return fallback


def _download(url: str, dest: Path) -> Path:
    request = urllib.request.Request(url, headers={"User-Agent": "aigc-eval-worker"})
    try:
        with urllib.request.urlopen(request, timeout=_TIMEOUT_SECONDS) as response:
            declared = response.headers.get("Content-Length")
            if declared and int(declared) > MAX_BYTES:
                raise MediaIntakeError(
                    f"素材超过 {MAX_BYTES // 1024 // 1024}MB 上限：{url}"
                )
            with dest.open("wb") as handle:
                # 边写边计数：Content-Length 可以缺失或撒谎，只信实际写入量。
                written = 0
                while chunk := response.read(1024 * 1024):
                    written += len(chunk)
                    if written > MAX_BYTES:
                        raise MediaIntakeError(
                            f"素材超过 {MAX_BYTES // 1024 // 1024}MB 上限：{url}"
                        )
                    handle.write(chunk)
    except MediaIntakeError:
        dest.unlink(missing_ok=True)
        raise
    except (urllib.error.URLError, OSError, ValueError) as exc:
        dest.unlink(missing_ok=True)
        raise MediaIntakeError(f"素材下载失败：{url}（{exc}）") from exc
    if dest.stat().st_size == 0:
        dest.unlink(missing_ok=True)
        raise MediaIntakeError(f"素材内容为空：{url}")
    return dest


_FALLBACK_SUFFIX = {"video": ".mp4", "image": ".jpg", "audio": ".mp3"}


def resolve_media(
    source: str,
    *,
    dest_dir: Path,
    name: str,
    kind: str = "video",
) -> Path:
    """把一个素材引用落成 ``dest_dir`` 下的本地文件并返回路径。

    ``source`` 为 http(s) URL 时下载；为绝对路径时原地拷贝一份（仅本机调试可用，
    分布式部署下调用方与 worker 不共享文件系统）。
    """
    # 后缀决定下游怎么认这份素材（``cases.reference_kind`` 按它分类），所以兜底值
    # 必须跟着 kind 走：给音频兜一个 .jpg，它会被当成参考图，而且不报错。
    fallback = _FALLBACK_SUFFIX.get(kind, ".jpg")
    dest_dir.mkdir(parents=True, exist_ok=True)
    reference = (source or "").strip()
    if not reference:
        raise MediaIntakeError("素材引用为空")

    scheme = urllib.parse.urlparse(reference).scheme.lower()
    if scheme in ("http", "https"):
        dest = dest_dir / f"{name}{_suffix_from_url(reference, fallback=fallback)}"
        return _download(reference, dest)

    if scheme:
        # 显式拒绝 file:// 等其它 scheme，而不是让它掉到本地路径分支里。
        raise MediaIntakeError(f"不支持的素材协议 {scheme}://，只接受 http(s)")

    local = Path(reference)
    if not local.is_absolute() or not local.is_file():
        raise MediaIntakeError(f"素材不存在或不是绝对路径：{reference}")
    dest = dest_dir / f"{name}{local.suffix.lower() or fallback}"
    shutil.copy2(local, dest)
    return dest
