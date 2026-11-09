/** 漫剧 compose 合成产出视为成片 */
const COMPOSE_TOOL_NAMES = new Set(['compose_video', 'concat_video', 'concat_videos']);

/** 漫剧流程中的镜头/探测类输出，仍是中间产物 */
const SEGMENT_VIDEO_TOOL_NAMES = new Set([
  'generate_clip',
  'probe_video',
  'process_video',
  'extract_frame',
  'understand_video_segment',
]);

export type ArtifactTier = 'final' | 'segment';

export function classifyArtifactTier(
  url: string,
  options?: { toolName?: string; sourceStage?: string },
): ArtifactTier {
  const sourceStage = options?.sourceStage?.trim().toLowerCase();
  if (sourceStage === 'film') return 'final';
  if (
    sourceStage === 'clips' ||
    sourceStage === 'frames' ||
    sourceStage === 'characters' ||
    sourceStage === 'script'
  ) {
    return 'segment';
  }

  const tool = (options?.toolName ?? '').trim().toLowerCase();
  if (tool && SEGMENT_VIDEO_TOOL_NAMES.has(tool)) return 'segment';
  if (tool && COMPOSE_TOOL_NAMES.has(tool)) return 'final';

  const path = url.split('?')[0]?.split('#')[0]?.toLowerCase() ?? '';
  if (path.includes('/capabilities/videos/compose/')) return 'final';

  return 'segment';
}

export function sortArtifactsByTier<T extends { kind: string; tier?: ArtifactTier }>(
  items: T[],
): T[] {
  const rank = (item: T): number => {
    if (item.tier === 'final' && item.kind === 'video') return 0;
    if (item.tier === 'final' && item.kind === 'image') return 1;
    if (item.kind === 'video') return 2;
    if (item.kind === 'image') return 3;
    return 4;
  };
  return [...items].sort((left, right) => rank(left) - rank(right));
}
