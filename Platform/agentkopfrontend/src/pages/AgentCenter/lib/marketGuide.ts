/**
 * 详情页「如何开始」：通用三步 + 本地 Unsplash 配图
 * Agent 描述本身走接口，不在这里写营销文案
 */
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import guideDeliver from '@/assets/market/guide-deliver.jpg';
import guideGoal from '@/assets/market/guide-goal.jpg';
import guideParams from '@/assets/market/guide-params.jpg';
import { MARKET_DETAIL_BY_KEY, MARKET_STEPS_PLANNING, MARKET_STEPS_WORKFLOW } from '../mock';

const GUIDE_IMAGES = [guideGoal, guideParams, guideDeliver] as const;

function withGuideImages(
  steps: Array<{ title: string; description: string }>,
): Array<{ title: string; description: string; image: string }> {
  return steps.map((step, index) => ({
    ...step,
    image: GUIDE_IMAGES[index] ?? guideGoal,
  }));
}

/** @deprecated 仅 planning 默认步骤；请用 resolveMarketGuideSteps */
export const MARKET_GUIDE_STEPS = withGuideImages(MARKET_STEPS_PLANNING);

export function resolveMarketGuideTitle(item: OfficialCatalogCardData): string {
  return MARKET_DETAIL_BY_KEY[item.key]?.stepsTitle ?? '如何开始';
}

export function resolveMarketGuideSteps(item: OfficialCatalogCardData) {
  const custom = MARKET_DETAIL_BY_KEY[item.key]?.steps;
  const steps =
    custom ??
    (item.variant === 'workflow' ? MARKET_STEPS_WORKFLOW : MARKET_STEPS_PLANNING);
  return withGuideImages(steps);
}
