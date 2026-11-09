import {
  ALLOWED_FPS,
  docDurationInFrames,
  type TimelineDoc,
} from "@/domain/timeline/types";
import { validateM3BDoc } from "@/domain/m3bSemantics";
import { frameToSeconds } from "@/domain/timeline/time";

export interface ExportValidationResult {
  errors: string[];
  warnings: string[];
  durationInFrames: number;
}

export function validateExport(doc: TimelineDoc | null): ExportValidationResult {
  if (!doc) {
    return { errors: ["时间线尚未加载"], warnings: [], durationInFrames: 0 };
  }

  const errors: string[] = [];
  const warnings: string[] = [];
  const { width, height, fps } = doc.metadata;
  const validMetadata =
    Number.isInteger(width) &&
    width > 0 &&
    Number.isInteger(height) &&
    height > 0 &&
    Number.isFinite(fps) &&
    ALLOWED_FPS.has(fps);

  if (!validMetadata) {
    errors.push("项目 metadata 无效，请检查画幅和 fps");
  }

  const durationInFrames = docDurationInFrames(doc);
  if (doc.items.length === 0 || durationInFrames <= 0) {
    errors.push("时间线为空，请先添加片段");
  }
  if (!doc.items.some((item) => item.kind === "video")) {
    errors.push("时间线缺少视频片段");
  }
  let usesLocalOnlyFeature = false;
  for (const item of doc.items) {
    const fadeIn = item.fadeInFrames ?? 0;
    const fadeOut = item.fadeOutFrames ?? 0;
    if (
      fadeIn < 0 ||
      fadeOut < 0 ||
      !Number.isInteger(fadeIn) ||
      !Number.isInteger(fadeOut) ||
      fadeIn + fadeOut > item.durationInFrames ||
      ((fadeIn > 0 || fadeOut > 0) && item.kind !== "video" && item.kind !== "audio")
    ) {
      errors.push(`片段 ${item.itemId} 的淡入淡出参数无效`);
    }
    if (item.transform) {
      const t = item.transform;
      if (
        item.kind !== "video" && item.kind !== "image" ||
        ![t.x, t.y, t.width, t.height, t.opacity ?? 0].every(Number.isFinite) ||
        t.x < 0 ||
        t.y < 0 ||
        t.width <= 0 ||
        t.height <= 0 ||
        t.x + t.width > 1 ||
        t.y + t.height > 1 ||
        (t.opacity ?? 0) < 0 ||
        (t.opacity ?? 0) > 1 ||
        (t.fit !== "contain" && t.fit !== "cover")
      ) {
        errors.push(`片段 ${item.itemId} 的画中画参数无效`);
      }
    }
    if (
      fadeIn > 0 ||
      fadeOut > 0 ||
      item.transform ||
      ((item.kind === "audio" || item.kind === "video") && (item.speed ?? 1) !== 1) ||
      ((item.kind === "video") && (item.volume ?? 1) !== 1)
    ) {
      usesLocalOnlyFeature = true;
    }
    if ((item.volume ?? 0) > 1) {
      warnings.push(`片段 ${item.itemId} 使用大于 1 的增益；不支持 Web Audio 时预览会近似。`);
    }
  }
  if (usesLocalOnlyFeature) {
    warnings.push(
      "当前项目使用淡入淡出、变速或画中画；Local Executor 可精确导出，Agent-K Executor 会明确拒绝不支持的功能。",
    );
  }
  for (const issue of validateM3BDoc(doc)) {
    (issue.level === "error" ? errors : warnings).push(issue.message);
  }

  return { errors: [...new Set(errors)], warnings: [...new Set(warnings)], durationInFrames };
}

export function formatExportDuration(frames: number, fps: number): string {
  if (!Number.isFinite(fps) || fps <= 0) return "—";
  const totalSeconds = frameToSeconds(frames, fps);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds - minutes * 60;
  return minutes > 0
    ? `${minutes}:${seconds.toFixed(1).padStart(4, "0")}`
    : `${seconds.toFixed(1)} 秒`;
}
