/**
 * 把官方对话 SSE 折叠成漫剧画布：工序 / 产物 / 血缘 / 在途占位
 * 规则对齐 comic-drama stages.go + fold_agent.go，不依赖 /v1/dramas
 *
 * 列归属优先用产物上的 stage；没有则按工具名启发式。
 * 连线优先用产物上的 inputs；没有则扫工具入参里的 https URL。
 */
import { collectMediaUrls } from '@/pages/AgentPlayground/hooks/sse/helpers';
import type { SSEState } from '@/pages/AgentPlayground/hooks/useSSE';

export const MANGA_STAGE_DEFS = [
  { id: 'script', name: '剧本与设定' },
  { id: 'characters', name: '定妆 · 场景 · 道具' },
  { id: 'frames', name: '分镜首帧' },
  { id: 'clips', name: '镜头视频' },
  { id: 'film', name: '成片合成' },
] as const;

export type MangaStageId = (typeof MANGA_STAGE_DEFS)[number]['id'];
export type MangaBoardStage = Exclude<MangaStageId, 'script'>;
export type MangaStageStatus = 'pending' | 'active' | 'done';

export interface MangaCanvasAsset {
  uri: string;
  kind: 'image' | 'video';
  tool: string;
  stage: MangaBoardStage;
  description: string;
}

export interface MangaCanvasEdge {
  from: string;
  to: string;
}

export interface MangaCanvasPending {
  id: string;
  stage: MangaBoardStage;
  tool: string;
  inputs: string[];
}

export interface MangaCanvasStageView {
  id: MangaStageId;
  name: string;
  status: MangaStageStatus;
}

export interface MangaCanvasModel {
  stages: MangaCanvasStageView[];
  assets: MangaCanvasAsset[];
  edges: MangaCanvasEdge[];
  pending: MangaCanvasPending[];
}

const SKIP_TOOLS = new Set(['list_artifacts', 'job_status']);
const COMPOSE_TOOLS = new Set(['compose_video', 'concat_video', 'concat_videos']);
const BOARD_STAGES: MangaBoardStage[] = ['characters', 'frames', 'clips', 'film'];

function artifactUri(raw: Record<string, unknown>): string {
  if (typeof raw.uri === 'string' && raw.uri.trim()) return raw.uri.trim();
  if (typeof raw.url === 'string' && raw.url.trim()) return raw.url.trim();
  return '';
}

/** 合成工具成功结果里的成片 URL（对齐 comic-drama filmURL） */
function filmURL(output: unknown): string {
  const artifactRows: Record<string, unknown>[] = [];
  if (isRecord(output)) {
    const videoUrl = output.videoUrl;
    if (typeof videoUrl === 'string' && videoUrl.trim()) return videoUrl.trim();
    if (Array.isArray(output.artifacts)) {
      for (const raw of output.artifacts) {
        if (isRecord(raw)) artifactRows.push(raw);
      }
    }
  }
  for (const raw of artifactRows) {
    const type = typeof raw.type === 'string' ? raw.type : '';
    if (type !== 'video') continue;
    const uri = artifactUri(raw);
    if (uri) return uri;
  }
  return '';
}

function pushFilm(
  films: string[],
  assets: MangaCanvasAsset[],
  seen: Set<string>,
  uri: string,
  description = '合成成片',
) {
  const key = assetKey(uri);
  if (!key) return;
  if (!films.some((item) => assetKey(item) === key)) films.push(uri);
  const existing = assets.find((asset) => assetKey(asset.uri) === key);
  if (existing) {
    existing.stage = 'film';
    existing.tool = 'compose_video';
    if (!existing.description) existing.description = description;
    return;
  }
  pushAsset(assets, seen, {
    uri,
    kind: 'video',
    tool: 'compose_video',
    stage: 'film',
    description,
  });
}

/** 镜头生成引用的 referenceImages 归到分镜首帧（对齐 comic-drama 流水线语义） */
function promoteClipReferenceFrames(state: SSEState, assets: MangaCanvasAsset[]) {
  const refKeys = new Set<string>();
  for (const card of state.toolCards) {
    if (card.status !== 'ok') continue;
    const name = normalizeTool(card.toolName);
    if (!name.includes('video')) continue;
    if (!isRecord(card.input)) continue;
    const refs = card.input.referenceImages;
    if (!Array.isArray(refs)) continue;
    for (const ref of refs) {
      if (typeof ref === 'string' && ref.trim()) refKeys.add(assetKey(ref));
    }
  }
  for (const asset of assets) {
    if (asset.kind === 'image' && refKeys.has(assetKey(asset.uri))) {
      asset.stage = 'frames';
    }
  }
}

function addEdge(edges: MangaCanvasEdge[], edgeKeys: Set<string>, from: string, to: string) {
  const key = `${from}\0${to}`;
  if (!from || !to || from === to || edgeKeys.has(key)) return;
  edgeKeys.add(key);
  edges.push({ from, to });
}

/** 入参对不上 URL 时，按相邻列补流水线连线，画布才看得到从左到右的流向 */
function fillPipelineLinks(
  buckets: Record<MangaBoardStage, MangaCanvasAsset[]>,
  pending: MangaCanvasPending[],
  edges: MangaCanvasEdge[],
  edgeKeys: Set<string>,
) {
  const incoming = new Set(edges.map((edge) => edge.to));
  for (let index = 1; index < BOARD_STAGES.length; index += 1) {
    const sources = buckets[BOARD_STAGES[index - 1]];
    if (sources.length === 0) continue;
    const targets = buckets[BOARD_STAGES[index]].filter(
      (asset) => !incoming.has(assetKey(asset.uri)),
    );
    for (let offset = 0; offset < targets.length; offset += 1) {
      const source = sources[sources.length === 1 ? 0 : offset % sources.length];
      addEdge(edges, edgeKeys, assetKey(source.uri), assetKey(targets[offset].uri));
    }
    for (const item of pending) {
      if (item.stage !== BOARD_STAGES[index] || item.inputs.length > 0) continue;
      item.inputs.push(...sources.map((asset) => assetKey(asset.uri)));
    }
  }
}

export function assetKey(uri: string): string {
  return uri.split('?')[0];
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

/** agentk.generate_image / official/edit_image → generate_image */
export function normalizeTool(tool: string): string {
  const trimmed = tool.trim();
  if (!trimmed) return '';
  const parts = trimmed.split(/[./:]/);
  return (parts[parts.length - 1] || trimmed).toLowerCase();
}

function asBoardStage(value: unknown): MangaBoardStage | '' {
  return typeof value === 'string' && BOARD_STAGES.includes(value as MangaBoardStage)
    ? (value as MangaBoardStage)
    : '';
}

function extractHttpUrls(value: unknown): string[] {
  const urls: string[] = [];
  const walk = (node: unknown) => {
    if (urls.length >= 64) return;
    if (typeof node === 'string') {
      const trimmed = node.trim();
      if (/^https?:\/\//i.test(trimmed)) urls.push(trimmed);
      return;
    }
    if (Array.isArray(node)) {
      node.forEach(walk);
      return;
    }
    if (isRecord(node)) Object.values(node).forEach(walk);
  };
  walk(value);
  return urls;
}

function promptFromInput(input: unknown): string {
  if (!isRecord(input)) return '';
  for (const key of ['prompt', 'instruction', 'text', 'query', 'caption']) {
    const value = input[key];
    if (typeof value === 'string' && value.trim()) return value.trim();
  }
  return '';
}

function describeCall(tool: string, input: unknown): string {
  const prompt = promptFromInput(input);
  const refs = extractHttpUrls(input).length;
  const name = normalizeTool(tool) || tool;
  if (name.includes('edit_image') && refs > 0) {
    const hint = prompt ? `和指令「${prompt.slice(0, 36)}」` : '和指令';
    return `使用 ${name} 根据 ${refs} 张参考图${hint}`;
  }
  if (prompt) return `使用 ${name} 按提示词「${prompt.slice(0, 48)}」`;
  return name ? `使用 ${name}` : '生成产物';
}

export function stageForTool(tool: string): MangaBoardStage | '' {
  const name = normalizeTool(tool);
  if (!name || SKIP_TOOLS.has(name)) return '';
  if (COMPOSE_TOOLS.has(name)) return 'film';
  if (name.includes('video')) return 'clips';
  if (name.includes('edit_image')) return 'frames';
  if (name.includes('generate_image') || name.includes('text_to_image') || name === 't2i') {
    return 'characters';
  }
  return '';
}

function stageForAsset(
  tool: string,
  description: string,
  kind: 'image' | 'video',
  explicit: MangaBoardStage | '',
  hasCharacters: boolean,
): MangaBoardStage {
  if (explicit) return explicit;
  const name = normalizeTool(tool);
  if (name.includes('edit_image')) {
    if (description.includes('首帧')) return 'frames';
    for (const marker of ['场景', '道具', '定妆', '状态图', '三视图']) {
      if (description.includes(marker)) return 'characters';
    }
    return 'frames';
  }
  if (name.includes('generate_image') || name.includes('text_to_image') || name === 't2i') {
    return /首帧|分镜/.test(description) ? 'frames' : 'characters';
  }
  const byTool = stageForTool(tool);
  if (byTool) return byTool;
  if (kind === 'video') return 'clips';
  return hasCharacters ? 'frames' : 'characters';
}

function pushAsset(
  assets: MangaCanvasAsset[],
  seen: Set<string>,
  item: MangaCanvasAsset,
): boolean {
  const key = assetKey(item.uri);
  if (!key || seen.has(key)) return false;
  seen.add(key);
  assets.push(item);
  return true;
}

interface MediaItem {
  uri: string;
  kind: 'image' | 'video';
  description: string;
  stage: MangaBoardStage | '';
  inputs: string[];
}

function collectOutputMedia(output: unknown): MediaItem[] {
  const items: MediaItem[] = [];
  const seen = new Set<string>();
  const push = (item: MediaItem) => {
    const key = assetKey(item.uri);
    if (!key || seen.has(key)) return;
    seen.add(key);
    items.push(item);
  };

  const artifacts = isRecord(output) && Array.isArray(output.artifacts) ? output.artifacts : [];
  for (const raw of artifacts) {
    if (!isRecord(raw)) continue;
    const uri = artifactUri(raw);
    const type = typeof raw.type === 'string' ? raw.type : '';
    if (type !== 'image' && type !== 'video') continue;
    const description = typeof raw.description === 'string' ? raw.description.trim() : '';
    const inputs = Array.isArray(raw.inputs)
      ? raw.inputs.filter((value): value is string => typeof value === 'string' && value.trim() !== '')
      : [];
    if (uri) {
      push({ uri, kind: type, description, stage: asBoardStage(raw.stage), inputs });
    }
  }

  const media = collectMediaUrls(output);
  for (const uri of media.images) {
    push({ uri, kind: 'image', description: '', stage: '', inputs: [] });
  }
  for (const uri of media.videos) {
    push({ uri, kind: 'video', description: '', stage: '', inputs: [] });
  }
  return items;
}

/** list_artifacts 重放已有产物：用每条 artifact.tool 归类，补齐分页账本里缺失的镜头 */
function ingestArtifactList(
  output: unknown,
  assets: MangaCanvasAsset[],
  seen: Set<string>,
  edges: MangaCanvasEdge[],
  edgeKeys: Set<string>,
) {
  if (!isRecord(output) || !Array.isArray(output.artifacts)) return;
  const linkInputs = (inputs: string[], toUri: string) => {
    const to = assetKey(toUri);
    for (const input of inputs) {
      const from = assetKey(input);
      if (!from || !seen.has(from)) continue;
      addEdge(edges, edgeKeys, from, to);
    }
  };

  for (const raw of output.artifacts) {
    if (!isRecord(raw)) continue;
    const uri = artifactUri(raw);
    const type = typeof raw.type === 'string' ? raw.type : '';
    if (!uri || (type !== 'image' && type !== 'video')) continue;
    const originTool = typeof raw.tool === 'string' ? raw.tool : '';
    const description = typeof raw.description === 'string' ? raw.description.trim() : '';
    const hasCharacters = assets.some((asset) => asset.stage === 'characters');
    const added = pushAsset(assets, seen, {
      uri,
      kind: type,
      tool: originTool || 'list_artifacts',
      stage: stageForAsset(originTool, description, type, asBoardStage(raw.stage), hasCharacters),
      description,
    });
    if (added && isRecord(raw.input)) {
      linkInputs(extractHttpUrls(raw.input.referenceImages), uri);
    }
  }
}

/** 把镜头工具 / list_artifacts 里的 referenceImages 补进分镜首帧列 */
function ensureReferenceFrameAssets(state: SSEState, assets: MangaCanvasAsset[], seen: Set<string>) {
  const addFrame = (uri: string) => {
    if (!uri.trim()) return;
    const key = assetKey(uri);
    const existing = assets.find((asset) => assetKey(asset.uri) === key);
    if (existing) {
      existing.stage = 'frames';
      return;
    }
    pushAsset(assets, seen, {
      uri,
      kind: 'image',
      tool: 'reference',
      stage: 'frames',
      description: '分镜参考',
    });
  };

  for (const card of state.toolCards) {
    if (card.status !== 'ok') continue;
    const norm = normalizeTool(card.toolName);
    if (norm === 'list_artifacts' && isRecord(card.output) && Array.isArray(card.output.artifacts)) {
      for (const raw of card.output.artifacts) {
        if (!isRecord(raw) || !isRecord(raw.input)) continue;
        const refs = raw.input.referenceImages;
        if (!Array.isArray(refs)) continue;
        for (const ref of refs) {
          if (typeof ref === 'string') addFrame(ref);
        }
      }
    }
    if (!norm.includes('video') || !isRecord(card.input)) continue;
    const refs = card.input.referenceImages;
    if (!Array.isArray(refs)) continue;
    for (const ref of refs) {
      if (typeof ref === 'string') addFrame(ref);
    }
  }
}

/** 成片列只保留 Films 列表里的 URL，去掉 compose 描述里误扫进来的 clip */
function reconcileFilmAssets(assets: MangaCanvasAsset[], films: string[]) {
  const filmKeys = new Set(films.map((uri) => assetKey(uri)));
  for (const asset of assets) {
    if (asset.stage !== 'film') continue;
    if (filmKeys.has(assetKey(asset.uri))) continue;
    asset.stage = 'clips';
  }
}

export function foldMangaCanvas(state: SSEState): MangaCanvasModel {
  const assets: MangaCanvasAsset[] = [];
  const edges: MangaCanvasEdge[] = [];
  const pending: MangaCanvasPending[] = [];
  const films: string[] = [];
  const seen = new Set<string>();
  const edgeKeys = new Set<string>();
  const openStages = new Set<MangaBoardStage>();

  const linkInputs = (inputs: string[], toUri: string) => {
    const to = assetKey(toUri);
    for (const input of inputs) {
      const from = assetKey(input);
      if (!from || !seen.has(from)) continue;
      addEdge(edges, edgeKeys, from, to);
    }
  };

  const ingest = (tool: string, input: unknown, output: unknown) => {
    const norm = normalizeTool(tool);
    if (SKIP_TOOLS.has(norm)) return;

    if (COMPOSE_TOOLS.has(norm)) {
      const url = filmURL(output);
      if (url) {
        const artifacts =
          isRecord(output) && Array.isArray(output.artifacts) ? output.artifacts : [];
        const described = artifacts.find(
          (raw) => isRecord(raw) && artifactUri(raw) === url && typeof raw.description === 'string',
        );
        const description =
          isRecord(described) && typeof described.description === 'string'
            ? described.description.trim()
            : '合成成片';
        pushFilm(films, assets, seen, url, description);
      }
      return;
    }

    const fallback = describeCall(tool, input);
    const fromArgs = extractHttpUrls(input);
    const hasCharacters = assets.some((asset) => asset.stage === 'characters');
    for (const media of collectOutputMedia(output)) {
      const description = media.description || fallback;
      const added = pushAsset(assets, seen, {
        uri: media.uri,
        kind: media.kind,
        tool,
        stage: stageForAsset(tool, description, media.kind, media.stage, hasCharacters),
        description,
      });
      if (added) linkInputs([...fromArgs, ...media.inputs], media.uri);
    }
  };

  for (const card of state.toolCards) {
    if (card.status !== 'ok') continue;
    const norm = normalizeTool(card.toolName);
    if (norm === 'list_artifacts') {
      ingestArtifactList(card.output, assets, seen, edges, edgeKeys);
      continue;
    }
    ingest(card.toolName, card.input, card.output);
  }
  for (const job of state.jobs) {
    if (job.status !== 'completed') continue;
    const card = state.toolCards.find((item) => item.toolCallId === job.toolCallId);
    ingest(card?.toolName ?? '', card?.input, job.result);
  }

  // Agent 常把成片链接写在回复正文里；与 comic-drama Films 一样单独入账，避免被 clips 去重挡住
  const ingestReplyVideos = (source: unknown) => {
    for (const uri of collectMediaUrls(source).videos) {
      pushFilm(films, assets, seen, uri, '成片（来自回复正文）');
    }
  };
  for (const msg of state.messages) {
    if (msg.role !== 'assistant') continue;
    for (const part of msg.parts) {
      if (part.type === 'text') ingestReplyVideos(part.text);
      if (part.type === 'tool-result') ingestReplyVideos(part.output);
    }
  }
  if (state.streamingText) ingestReplyVideos(state.streamingText);

  reconcileFilmAssets(assets, films);
  ensureReferenceFrameAssets(state, assets, seen);
  promoteClipReferenceFrames(state, assets);

  const pendingIds = new Set<string>();
  const markPending = (id: string, tool: string, input: unknown) => {
    const stage = stageForTool(tool);
    if (!stage || pendingIds.has(id)) return;
    pendingIds.add(id);
    openStages.add(stage);
    const inputs = extractHttpUrls(input)
      .map(assetKey)
      .filter((uri) => seen.has(uri));
    pending.push({ id, stage, tool, inputs });
  };

  for (const card of state.toolCards) {
    if (SKIP_TOOLS.has(normalizeTool(card.toolName)) || card.status !== 'running') continue;
    markPending(card.toolCallId, card.toolName, card.input);
  }
  for (const job of state.jobs) {
    if (job.status !== 'submitted') continue;
    const card = state.toolCards.find((item) => item.toolCallId === job.toolCallId);
    if (!card?.toolName) continue;
    markPending(job.jobId, card.toolName, card.input);
  }

  const assistantSpoke = state.messages.some(
    (message) =>
      message.role === 'assistant' &&
      message.parts.some((part) => part.type === 'text' && part.text.trim()),
  );
  const generating =
    pending.length > 0 || state.jobs.some((job) => job.status === 'submitted');

  const buckets: Record<MangaBoardStage, MangaCanvasAsset[]> = {
    characters: [],
    frames: [],
    clips: [],
    film: [],
  };
  for (const asset of assets) buckets[asset.stage].push(asset);
  fillPipelineLinks(buckets, pending, edges, edgeKeys);

  const stages: MangaCanvasStageView[] = MANGA_STAGE_DEFS.map((def) => {
    const done =
      def.id === 'script'
        ? assistantSpoke || assets.length > 0 || films.length > 0
        : def.id === 'film'
          ? buckets.film.length > 0 || films.length > 0
          : buckets[def.id].length > 0;
    let status: MangaStageStatus = done ? 'done' : 'pending';
    if (generating && def.id !== 'script' && openStages.has(def.id)) status = 'active';
    return { id: def.id, name: def.name, status };
  });

  if (generating && !stages.some((stage) => stage.status === 'active')) {
    const next = stages.find((stage) => stage.status === 'pending');
    if (next) next.status = 'active';
  }

  return { stages, assets, edges, pending };
}
