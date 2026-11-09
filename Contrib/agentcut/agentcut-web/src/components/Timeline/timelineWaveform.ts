import { effectiveSpeed } from "@/domain/timeline/types";
import type { Item, Media } from "@/domain/timeline/types";

const WAVEFORM_TARGET_PEAK = 0.92;
const WAVEFORM_MAX_DISPLAY_GAIN = 32;

export function waveformDisplayGain(peaks: number[]): number {
  const maxPeak = peaks.reduce(
    (maximum, peak) =>
      Number.isFinite(peak) ? Math.max(maximum, Math.max(0, peak)) : maximum,
    0,
  );
  if (maxPeak <= 0) return 1;
  return Math.max(
    1,
    Math.min(WAVEFORM_MAX_DISPLAY_GAIN, WAVEFORM_TARGET_PEAK / maxPeak),
  );
}

export function visibleSourceRange(
  item: Item,
  media: Media | undefined,
): [number, number] {
  if (!media || media.durationInFrames <= 0) return [0, 1];
  const trimStart = item.trimStart ?? 0;
  const consumed = item.durationInFrames * effectiveSpeed(item);
  const start = trimStart / media.durationInFrames;
  const end = (trimStart + consumed) / media.durationInFrames;
  return [
    Math.max(0, Math.min(1, start)),
    Math.max(start, Math.min(1, end)),
  ];
}

export function resampleWaveformPeaks(
  peaks: number[],
  rangeStart: number,
  rangeEnd: number,
  widthPx: number,
): number[] {
  if (peaks.length === 0 || widthPx <= 0) return [];
  const start = Math.max(0, Math.min(1, rangeStart)) * peaks.length;
  const end = Math.max(start, Math.min(1, rangeEnd)) * peaks.length;
  const span = Math.max(Number.EPSILON, end - start);
  const sampleCount = Math.max(1, Math.min(2000, Math.floor(widthPx / 3)));
  const samples = new Array<number>(sampleCount);

  if (sampleCount <= span) {
    for (let index = 0; index < sampleCount; index += 1) {
      const bucketStart = start + (index / sampleCount) * span;
      const bucketEnd = start + ((index + 1) / sampleCount) * span;
      let peak = 0;
      for (
        let sourceIndex = Math.max(0, Math.floor(bucketStart));
        sourceIndex < Math.min(peaks.length, Math.ceil(bucketEnd));
        sourceIndex += 1
      ) {
        peak = Math.max(peak, peaks[sourceIndex] ?? 0);
      }
      samples[index] = peak;
    }
    return samples;
  }

  for (let index = 0; index < sampleCount; index += 1) {
    const position = Math.min(
      peaks.length - 1,
      start + ((index + 0.5) / sampleCount) * span,
    );
    const left = Math.max(0, Math.floor(position));
    const right = Math.min(peaks.length - 1, left + 1);
    const ratio = position - left;
    samples[index] =
      (peaks[left] ?? 0) * (1 - ratio) + (peaks[right] ?? 0) * ratio;
  }
  return samples;
}
