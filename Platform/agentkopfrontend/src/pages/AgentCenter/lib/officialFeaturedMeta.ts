/**
 * 精选轮播展示文案生成：按卡片 key 稳定分配场景 / 卖点 / 指标
 * 文案池统一维护在 AgentCenter/mock.ts，这里只负责挑选与拼装逻辑
 */
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import { pickByHash } from '@/utils/text';
import type { FeaturedSlideMeta, MarketDetailMeta } from '../types';
import {
  FEATURED_HOOK_BY_KEY,
  FEATURED_ORDER,
  FEATURED_PITCH_BY_KEY,
  FEATURED_PITCH_MIN_LENGTH,
  FEATURED_PITCH_SUFFIX,
  FEATURED_PLANNING_HOOKS,
  FEATURED_PLANNING_STATS,
  FEATURED_SCENE_BY_KEY,
  FEATURED_SCENE_BY_NAME,
  FEATURED_WORKFLOW_HOOKS,
  FEATURED_WORKFLOW_STATS,
  MARKET_DETAIL_BY_KEY,
  MARKET_STEPS_PLANNING,
  MARKET_STEPS_WORKFLOW,
} from '../mock';

/** 按 key / name / workflowKey 解析场景标签 */
function resolveScene(item: OfficialCatalogCardData): string {
  if (FEATURED_SCENE_BY_KEY[item.key]) return FEATURED_SCENE_BY_KEY[item.key];
  if (item.workflowKey && FEATURED_SCENE_BY_NAME[item.workflowKey]) {
    return FEATURED_SCENE_BY_NAME[item.workflowKey];
  }
  if (FEATURED_SCENE_BY_NAME[item.name]) return FEATURED_SCENE_BY_NAME[item.name];
  return '官方示范';
}

/** 轮播项排序：优先 FEATURED_ORDER，其余保持原序 */
export function sortFeaturedCarouselItems(
  items: OfficialCatalogCardData[],
): OfficialCatalogCardData[] {
  return [...items].sort((a, b) => {
    const ai = FEATURED_ORDER.indexOf(a.key);
    const bi = FEATURED_ORDER.indexOf(b.key);
    if (ai === -1 && bi === -1) return 0;
    if (ai === -1) return 1;
    if (bi === -1) return -1;
    return ai - bi;
  });
}

/** 为轮播项生成展示文案 */
export function buildFeaturedSlideMeta(item: OfficialCatalogCardData): FeaturedSlideMeta {
  const isWorkflow = item.variant === 'workflow';
  const scene = resolveScene(item);
  const typeLabel = isWorkflow ? '工作流 Agent' : '智能体 Agent';
  const hook =
    FEATURED_HOOK_BY_KEY[item.key] ??
    pickByHash(item.key, isWorkflow ? FEATURED_WORKFLOW_HOOKS : FEATURED_PLANNING_HOOKS, 2);
  const statPool = isWorkflow ? FEATURED_WORKFLOW_STATS : FEATURED_PLANNING_STATS;
  const stats = [...pickByHash(item.key, statPool, 4)];

  const versionSkill = item.skills.find((skill) => skill.startsWith('v'));
  if (versionSkill) {
    stats[2] = { value: versionSkill, label: '当前版本' };
  }

  const pitchSource = (item.detail || item.description || '').trim();
  const pitch =
    FEATURED_PITCH_BY_KEY[item.key] ??
    (pitchSource.length >= FEATURED_PITCH_MIN_LENGTH
      ? pitchSource
      : `${pitchSource || item.name}${FEATURED_PITCH_SUFFIX}`);

  return {
    eyebrow: `${typeLabel} · ${scene}`,
    hook,
    pitch,
    stats,
  };
}

/** Market 详情落地页文案：精选 meta + 三步 / 案例 */
export function buildMarketDetailMeta(item: OfficialCatalogCardData): MarketDetailMeta {
  const slide = buildFeaturedSlideMeta(item);
  const custom = MARKET_DETAIL_BY_KEY[item.key];
  const isWorkflow = item.variant === 'workflow';
  return {
    ...slide,
    stepsTitle: custom?.stepsTitle ?? (isWorkflow ? '3 步跑通官方工作流' : '3 步开始智能协作'),
    steps: custom?.steps ?? (isWorkflow ? MARKET_STEPS_WORKFLOW : MARKET_STEPS_PLANNING),
    casesTitle: custom?.casesTitle ?? '精选案例',
    cases:
      custom?.cases ??
      item.skills.slice(0, 3).map((skill) => ({
        title: skill,
        description: slide.hook,
      })),
  };
}
