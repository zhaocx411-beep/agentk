import type { TranscriptDeleteRange, TranscriptEntry } from "@/remote";

export function selectedTranscriptRanges(
  entries: TranscriptEntry[],
  selectedOccurrenceIds: ReadonlySet<string>,
): TranscriptDeleteRange[] {
  const ranges: TranscriptDeleteRange[] = [];
  let first: TranscriptEntry | null = null;
  let last: TranscriptEntry | null = null;
  for (const entry of entries) {
    if (selectedOccurrenceIds.has(entry.occurrenceId)) {
      first ??= entry;
      last = entry;
      continue;
    }
    if (first && last) {
      ranges.push({
        fromOccurrenceId: first.occurrenceId,
        toOccurrenceId: last.occurrenceId,
      });
    }
    first = null;
    last = null;
  }
  if (first && last) {
    ranges.push({
      fromOccurrenceId: first.occurrenceId,
      toOccurrenceId: last.occurrenceId,
    });
  }
  return ranges;
}

export function estimatedSelectedFrames(
  entries: TranscriptEntry[],
  selectedOccurrenceIds: ReadonlySet<string>,
): number {
  const intervals = entries
    .filter((entry) => selectedOccurrenceIds.has(entry.occurrenceId))
    .map((entry) => ({ from: entry.fromFrame, to: entry.toFrame }))
    .filter((range) => range.to > range.from)
    .toSorted((left, right) => left.from - right.from || left.to - right.to);
  let total = 0;
  let current: { from: number; to: number } | null = null;
  for (const interval of intervals) {
    if (!current) {
      current = { ...interval };
      continue;
    }
    if (interval.from <= current.to) {
      current.to = Math.max(current.to, interval.to);
      continue;
    }
    total += current.to - current.from;
    current = { ...interval };
  }
  if (current) total += current.to - current.from;
  return total;
}

export function activeTranscriptOccurrence(
  entries: TranscriptEntry[],
  currentFrame: number,
): string | null {
  let active: TranscriptEntry | null = null;
  for (const entry of entries) {
    if (entry.fromFrame > currentFrame) break;
    if (currentFrame >= entry.fromFrame && currentFrame < entry.toFrame) {
      active = entry;
    }
  }
  return active?.occurrenceId ?? null;
}

export function occurrenceRange(
  entries: TranscriptEntry[],
  fromOccurrenceId: string,
  toOccurrenceId: string,
): Set<string> {
  const from = entries.findIndex((entry) => entry.occurrenceId === fromOccurrenceId);
  const to = entries.findIndex((entry) => entry.occurrenceId === toOccurrenceId);
  if (from < 0 || to < 0) return new Set();
  const start = Math.min(from, to);
  const end = Math.max(from, to);
  return new Set(entries.slice(start, end + 1).map((entry) => entry.occurrenceId));
}
