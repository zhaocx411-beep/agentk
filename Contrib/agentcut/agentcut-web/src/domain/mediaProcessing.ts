import type { Media, MediaProcessing, ProcessingState } from "./timeline/types";

const STAGES: {
  key: keyof Pick<
    MediaProcessing,
    "probe" | "thumbnail" | "waveform" | "transcript" | "understanding"
  >;
  active: string;
  failed: string;
}[] = [
  { key: "probe", active: "正在探测素材", failed: "基础探测失败" },
  { key: "thumbnail", active: "正在生成缩略图", failed: "缩略图生成失败" },
  { key: "waveform", active: "正在提取波形", failed: "波形提取失败" },
  { key: "transcript", active: "正在生成文字稿", failed: "文字稿生成失败" },
  { key: "understanding", active: "正在理解内容", failed: "内容理解失败" },
];

export type ProcessingTone = "neutral" | "success" | "warning" | "danger";

export interface MediaProcessingView {
  label: string;
  stage?: string;
  tone: ProcessingTone;
  failedStages: string[];
  error?: string;
}

/** 旧素材没有 processing，必须继续保持可用。 */
export function isMediaReadyForTimeline(media: Media): boolean {
  return media.processing?.readyForTimeline ?? true;
}

export function processingView(media: Media): MediaProcessingView | null {
  const processing = media.processing;
  if (!processing) return null;
  const active = STAGES.find(({ key }) => isActive(processing[key]));
  const failedStages = STAGES.filter(({ key }) => processing[key] === "failed").map(
    ({ failed }) => failed,
  );
  switch (processing.status) {
    case "ready":
      return {
        label: "准备完成",
        tone: "success",
        failedStages,
      };
    case "partial":
      return {
        label: "部分解析失败",
        stage: active?.active,
        tone: "warning",
        failedStages,
        error: processing.error,
      };
    case "failed":
      return {
        label: "解析失败",
        stage: active?.active,
        tone: "danger",
        failedStages,
        error: processing.error,
      };
    case "processing":
      return {
        label:
          processing.readyForTimeline && isActive(processing.transcript)
            ? "可编辑，文字稿生成中"
            : "解析中",
        stage: active?.active,
        tone: "neutral",
        failedStages,
        error: processing.error,
      };
  }
}

export function mediaNeedsProcessingPoll(media: Media): boolean {
  const processing = media.processing;
  if (!processing) return false;
  return STAGES.some(({ key }) => isActive(processing[key]));
}

export function mediaListNeedsProcessingPoll(media: Media[]): boolean {
  return media.some(mediaNeedsProcessingPoll);
}

export interface MediaMenuAvailability {
  addToTimeline: boolean;
  viewTranscript: boolean;
  generateCaptions: boolean;
  viewUnderstanding: boolean;
  retry: boolean;
}

export function mediaMenuAvailability(media: Media): MediaMenuAvailability {
  const hasTranscript = (media.transcript?.segments?.length ?? 0) > 0;
  return {
    addToTimeline: isMediaReadyForTimeline(media),
    viewTranscript: hasTranscript,
    generateCaptions: hasTranscript,
    viewUnderstanding: !!media.understandingText?.trim(),
    retry:
      !media.processing ||
      media.processing.status === "processing" ||
      media.processing.status === "partial" ||
      media.processing.status === "failed",
  };
}

function isActive(state: ProcessingState): boolean {
  return state === "pending" || state === "running";
}

type Schedule = (callback: () => void, delayMs: number) => ReturnType<typeof setTimeout>;
type Cancel = (timer: ReturnType<typeof setTimeout>) => void;

/**
 * 单链式轮询：每次刷新完成后才安排下一次，避免慢请求产生重复 in-flight。
 * getMedia 返回最新 store 数据；所有阶段终态后不再安排 timer。
 */
export function startMediaProcessingPoller(options: {
  getMedia: () => Media[];
  refresh: () => Promise<void>;
  shouldPoll?: () => boolean;
  intervalMs?: number;
  schedule?: Schedule;
  cancel?: Cancel;
}): () => void {
  const schedule = options.schedule ?? ((callback, delay) => window.setTimeout(callback, delay));
  const cancel = options.cancel ?? ((timer) => window.clearTimeout(timer));
  const intervalMs = options.intervalMs ?? 2000;
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | null = null;

  const scheduleNext = () => {
    const shouldPoll =
      options.shouldPoll?.() ?? mediaListNeedsProcessingPoll(options.getMedia());
    if (stopped || !shouldPoll) return;
    timer = schedule(() => {
      timer = null;
      void options.refresh().finally(scheduleNext);
    }, intervalMs);
  };

  scheduleNext();
  return () => {
    stopped = true;
    if (timer !== null) cancel(timer);
  };
}
