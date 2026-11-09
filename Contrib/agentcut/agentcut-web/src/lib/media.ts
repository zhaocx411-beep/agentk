// 浏览器端素材探测工具（上传前置步骤，设计文档第 9 节）。
import type { MediaKind } from "@/domain/timeline/types";
import { formatFrameDuration } from "@/domain/timeline/time";

const EXT_KIND: Record<string, MediaKind> = {
  mp4: "video", mov: "video", mkv: "video", webm: "video", avi: "video", m4v: "video",
  mp3: "audio", wav: "audio", m4a: "audio", aac: "audio", flac: "audio", ogg: "audio",
  png: "image", jpg: "image", jpeg: "image", gif: "image", webp: "image", bmp: "image",
};

/** 按 MIME 前缀判断素材类别；MIME 缺失/不识别时按扩展名兜底（部分系统对
 *  mkv/flac 等格式不给 file.type）。都判不出返回 null。 */
export function mediaKindOfFile(file: File): MediaKind | null {
  if (file.type.startsWith("video/")) return "video";
  if (file.type.startsWith("audio/")) return "audio";
  if (file.type.startsWith("image/")) return "image";
  const ext = file.name.split(".").pop()?.toLowerCase();
  return (ext && EXT_KIND[ext]) || null;
}

/** 元数据探测超时：超过后按失败处理，避免上传按钮无限转圈。 */
const PROBE_TIMEOUT_MS = 8000;

/**
 * 用离屏 <video>/<audio> 元素探测时长（毫秒）。这是上传体验优化；
 * 不支持的编码或超时时调用方应省略 durationMs，交给服务端 ffprobe。
 */
export function probeDurationMs(file: File, kind: "video" | "audio"): Promise<number> {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file);
    const el = document.createElement(kind);
    el.preload = "metadata";

    let settled = false;
    const finish = (fn: () => void) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      el.removeAttribute("src");
      el.load();
      URL.revokeObjectURL(url);
      fn();
    };
    const timer = setTimeout(() => {
      finish(() => reject(new Error("读取素材时长超时，请检查文件是否损坏")));
    }, PROBE_TIMEOUT_MS);

    el.onloadedmetadata = () => {
      const seconds = el.duration;
      finish(() => {
        if (!Number.isFinite(seconds) || seconds <= 0) {
          reject(new Error("无法读取素材时长（文件可能损坏或格式不支持）"));
        } else {
          resolve(seconds * 1000);
        }
      });
    };
    el.onerror = () => {
      finish(() => reject(new Error("浏览器无法解码该素材，请检查文件格式")));
    };
    el.src = url;
  });
}

export async function probeDurationBestEffort(
  file: File,
  kind: "video" | "audio",
  probe: typeof probeDurationMs = probeDurationMs,
): Promise<number | undefined> {
  try {
    return await probe(file, kind);
  } catch {
    return undefined;
  }
}

/** 帧数 → "mm:ss" 显示。 */
export function formatFrames(durationInFrames: number, fps: number): string {
  return formatFrameDuration(durationInFrames, fps);
}
