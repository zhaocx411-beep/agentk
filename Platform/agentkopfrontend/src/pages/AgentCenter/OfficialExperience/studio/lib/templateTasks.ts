/**
 * 创作模版：datasets/tasks/query?usage=TEMPLATE&pipelineName=…
 * Agent-loop 无 pipelineName 时用中文展示名，并对齐后端 TEMPLATE 数据集 key
 */
import type { ChatSuggestionItem } from '@/pages/AgentPlayground/components/chat/ChatEmptyState';
import { inferMediaTypeFromUrl } from '@/services/media';
import { queryDatasetTasks } from '@/pages/OfflineRun/api';
import type { DatasetTask } from '@/pages/OfflineRun/types';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import {
  isOfficialVirtualTryOnLoop,
  OFFICIAL_VIRTUAL_TRYON_AGENT_LOOP_ID,
} from '../../../utils';
import { isComicDramaPipeline } from './comicDrama';
import type { ExecutionSpecId } from './executionSpecs';
import type { CreationTemplate, SampleRow } from './types';

export interface CreationTemplateBundle {
  template: CreationTemplate;
  sample: SampleRow;
  /** TEMPLATE 用例的 extraFields，供工作台回填表单 */
  extraFields: Record<string, unknown>;
  /** referImages + extraFields 中的图片/视频/文档/音频 URL */
  referUrls: string[];
}

/** 单页模版查询结果（供下拉滚动翻页） */
export interface CreationTemplatePage {
  bundles: CreationTemplateBundle[];
  current: number;
  pageSize: number;
  total: number;
  hasMore: boolean;
}

export const TEMPLATE_PAGE_SIZE = 20;

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function asString(value: unknown): string {
  if (typeof value === 'string') return value.trim();
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return '';
}

function asUrlList(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.map((item) => asString(item)).filter(Boolean);
}

const PIPELINE_STAGES = [
  { key: 'prepare', title: '参数确认', badge: 'FORM' },
  { key: 'run', title: '执行工作流', badge: 'PIPELINE' },
  { key: 'output', title: '产物回收', badge: 'ARTIFACT' },
];

/** extraFields 里常见的单 URL 字段（文档、音频等） */
const TEMPLATE_SINGLE_URL_KEYS = [
  'fileUrl',
  'docUrl',
  'documentUrl',
  'audioUrl',
  'audio_url',
  'modelImageUrl',
] as const;

function collectTemplateSingleUrls(extra: Record<string, unknown>): string[] {
  return TEMPLATE_SINGLE_URL_KEYS.map((key) => asString(extra[key])).filter(Boolean);
}

/** 参考素材：referImages + extraFields.images / videos / audios / fileUrl / audioUrl 等 */
export function collectTemplateReferUrls(
  task: DatasetTask,
  extra?: Record<string, unknown>,
): string[] {
  const extraRecord = extra ?? asRecord(task.extraFields) ?? {};
  const urls = [
    ...asUrlList(task.referImages),
    ...asUrlList(extraRecord.images),
    ...asUrlList(extraRecord.videos),
    ...asUrlList(extraRecord.referVideos),
    ...asUrlList(extraRecord.video),
    ...asUrlList(extraRecord.audios),
    ...asUrlList(extraRecord.audioUrls),
    ...asUrlList(extraRecord.audio_urls),
    ...asUrlList(extraRecord.productImageUrls),
    ...collectTemplateSingleUrls(extraRecord),
  ];
  return [...new Set(urls)];
}

/** 参考图：优先 referImages，其次 extraFields.images（仅图片类，供工作台封面区） */
function collectTemplateImages(task: DatasetTask, extra: Record<string, unknown>): string[] {
  const urls = collectTemplateReferUrls(task, extra);
  return urls.filter((url) => inferMediaTypeFromUrl(url) === 'image');
}

/** 将 TEMPLATE 用例映射为样本行（商品字段 + 动态表单入参） */
export function mapTemplateTaskToSample(task: DatasetTask): SampleRow {
  const extra = asRecord(task.extraFields) ?? {};
  const title =
    asString(extra['商品标题']) ||
    asString(extra.title) ||
    asString(extra.name) ||
    task.prompt.split('\n')[0]?.trim() ||
    task.taskKey ||
    `模版 ${task.id}`;
  const price = asString(extra['商品价格']) || asString(extra.price);
  const attributes =
    asString(extra['商品属性信息']) ||
    asString(extra['商品属性']) ||
    asString(extra.attributes);
  const category =
    asString(extra['类目']) || asString(extra.category) || asString(extra['商品类目']) || '商品';
  const images = collectTemplateImages(task, extra);
  const coverUrls = images.length > 0 ? images.slice(0, 1) : [];
  const detailImages = images.length > 1 ? images.slice(1) : [];
  const videoPrompt = asString(extra.video_prompt) || task.prompt || '';
  const link = asString(task.linkUrl) || asString(extra['商品链接']) || asString(extra.link);

  // 与 capabilities 字段名对齐，供侧栏动态表单回填（images 仍走参考图区）
  const templateArgs: Record<string, unknown> = { ...extra };
  if (title) templateArgs.title = asString(extra.title) || title;
  if (videoPrompt) templateArgs.video_prompt = videoPrompt;
  if (images.length > 0) templateArgs.images = images;
  if (link) templateArgs.link = link;

  return {
    id: String(task.id),
    category,
    title,
    link,
    coverUrls,
    price,
    detailImages,
    attributes,
    templateArgs,
  };
}

/**
 * TEMPLATE 数据集 key（文档写作 piplineName；请求参数名仍是 pipelineName）。
 * 与 official/defaults 展示名、workflowKey、pipeline-runs 的 pipeline 字段可能不同。
 */
export const VIRTUAL_TRYON_TEMPLATE_PIPELINE_NAME = '虚拟试穿agentloop';
export const COMIC_DRAMA_TEMPLATE_PIPELINE_NAME = '漫剧成片pipline';

export interface TemplatePipelineNameHints {
  workflowKey?: string;
  name?: string;
  agentLoopId?: number;
}

/**
 * 对齐后端 TEMPLATE 数据集的 pipelineName。
 * 例：defaults 展示名「漫剧动态漫 Agent」→ 数据集 key「漫剧动态漫Agent」
 */
function normalizeTemplatePipelineName(name: string): string {
  return name.replace(/\s+Agent$/i, 'Agent').trim();
}

/**
 * TEMPLATE 查询用的 pipelineName。
 * 虚拟试穿 / 漫剧成片走文档约定的数据集 key；其余优先 defaults.pipelineName。
 */
export function resolveTemplateQueryPipelineName(
  pipelineName: string,
  hints?: TemplatePipelineNameHints,
): string {
  const name = normalizeTemplatePipelineName(pipelineName);
  const haystack = [name, hints?.workflowKey, hints?.name].filter(Boolean).join(' ');
  if (
    hints?.agentLoopId === OFFICIAL_VIRTUAL_TRYON_AGENT_LOOP_ID ||
    /虚拟试穿/.test(haystack)
  ) {
    return VIRTUAL_TRYON_TEMPLATE_PIPELINE_NAME;
  }
  if (
    isComicDramaPipeline(name) ||
    isComicDramaPipeline(hints?.workflowKey) ||
    /漫剧成片/.test(haystack) ||
    /comic_drama/.test(haystack)
  ) {
    return COMIC_DRAMA_TEMPLATE_PIPELINE_NAME;
  }
  return name;
}

/**
 * TEMPLATE 查询用的 pipelineName。
 * 优先 official/defaults 下发的 pipelineName；Agent-loop 无字段时用中文展示名 item.name。
 */
export function resolveOfficialTemplatePipelineName(
  item: OfficialCatalogCardData | null | undefined,
): string {
  if (!item) return '';
  if (isOfficialVirtualTryOnLoop(item)) return VIRTUAL_TRYON_TEMPLATE_PIPELINE_NAME;
  const raw =
    item.variant === 'planning'
      ? (item.pipelineName || item.name || '')
      : (item.pipelineName || '');
  return resolveTemplateQueryPipelineName(raw.trim(), {
    workflowKey: item.workflowKey,
    name: item.name,
    agentLoopId: item.agentLoopId,
  });
}

/** 将 TEMPLATE 用例映射为创作模版下拉项 */
export function mapTemplateTaskToBundle(
  task: DatasetTask,
  specId: ExecutionSpecId,
): CreationTemplateBundle {
  const extra = asRecord(task.extraFields) ?? {};
  const referUrls = collectTemplateReferUrls(task, extra);
  const sample = mapTemplateTaskToSample(task);
  const tags = [sample.category, sample.price ? `¥${sample.price}` : ''].filter(Boolean);
  const description =
    task.prompt.length > 120 ? `${task.prompt.slice(0, 120)}…` : task.prompt || sample.title;

  const videoPrompt = asString(sample.templateArgs?.video_prompt) || task.prompt || '';
  const template: CreationTemplate = {
    id: String(task.id),
    specs: [specId],
    name: sample.title,
    description,
    scene: '效果广告',
    tags,
    preferredCategories: sample.category ? [sample.category] : [],
    prompt: videoPrompt,
    memory: [],
    refImageLimit: Math.max(12, sample.coverUrls.length + sample.detailImages.length),
    stages: PIPELINE_STAGES,
  };

  return { template, sample, extraFields: extra, referUrls };
}

/** Agent-loop 空态快捷提问：优先 extraFields.title + video_prompt */
export function mapTemplateBundleToChatSuggestion(
  bundle: CreationTemplateBundle,
): ChatSuggestionItem {
  const extra = bundle.extraFields;
  const label =
    asString(extra.title) ||
    asString(extra.name) ||
    bundle.template.name ||
    bundle.sample.title;
  const prompt =
    asString(extra.video_prompt) ||
    bundle.template.prompt ||
    asString(bundle.sample.templateArgs?.video_prompt) ||
    label;
  return { label, prompt, referUrls: bundle.referUrls };
}

/**
 * 按页拉取创作模版。
 * pipelineName 须是 TEMPLATE 数据集 key（虚拟试穿 / 漫剧成片见文档约定）。
 */
export async function fetchCreationTemplatePage(
  pipelineName: string,
  current = 1,
  pageSize = TEMPLATE_PAGE_SIZE,
  specId?: ExecutionSpecId,
): Promise<CreationTemplatePage> {
  const name = resolveTemplateQueryPipelineName(pipelineName);
  const resolvedSpecId = (specId || name || 'workflow') as ExecutionSpecId;
  if (!name) {
    return { bundles: [], current: 1, pageSize, total: 0, hasMore: false };
  }

  const res = await queryDatasetTasks({
    usage: 'TEMPLATE',
    pipelineName: name,
    current,
    pageSize,
  });
  const records = Array.isArray(res?.records) ? res.records : [];
  const total = res?.total ?? records.length;
  const page = res?.current || current;
  const size = res?.pageSize || pageSize;
  const bundles = records
    .slice()
    .sort((a, b) => a.id - b.id)
    .map((task) => mapTemplateTaskToBundle(task, resolvedSpecId));

  return {
    bundles,
    current: page,
    pageSize: size,
    total,
    hasMore: page * size < total,
  };
}
