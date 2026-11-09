export type FrameRounding = "round" | "floor" | "ceil";

export function safeFps(fps: number): number {
  return Number.isFinite(fps) && fps > 0 ? fps : 1;
}

export function frameToSeconds(frame: number, fps: number): number {
  return Math.max(0, frame) / safeFps(fps);
}

export function secondsToFrame(
  seconds: number,
  fps: number,
  rounding: FrameRounding = "round",
): number {
  const value = Math.max(0, seconds) * safeFps(fps);
  return Math[rounding](value);
}

export function millisecondsToFrame(
  milliseconds: number,
  fps: number,
  rounding: FrameRounding = "round",
): number {
  return secondsToFrame(milliseconds / 1000, fps, rounding);
}

export function normalizeFrame(frame: number): number {
  if (!Number.isFinite(frame)) return 0;
  return Math.max(0, Math.round(frame));
}

export function clampFrame(frame: number, durationInFrames: number): number {
  return Math.min(normalizeFrame(durationInFrames), normalizeFrame(frame));
}

export function frameFromRatio(ratio: number, durationInFrames: number): number {
  const boundedRatio = Math.min(1, Math.max(0, ratio));
  return clampFrame(boundedRatio * durationInFrames, durationInFrames);
}

export function formatFrameTimecode(frame: number, fps: number): string {
  const wholeFrame = normalizeFrame(frame);
  const wholeFps = Math.max(1, Math.round(safeFps(fps)));
  const minutes = Math.floor(wholeFrame / (wholeFps * 60));
  const seconds = Math.floor(wholeFrame / wholeFps) % 60;
  const frames = wholeFrame % wholeFps;
  return [minutes, seconds, frames]
    .map((value) => String(value).padStart(2, "0"))
    .join(":");
}

export function formatFrameDuration(frame: number, fps: number): string {
  if (frame <= 0 || fps <= 0) return "—";
  const totalSeconds = Math.round(frameToSeconds(frame, fps));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
}
