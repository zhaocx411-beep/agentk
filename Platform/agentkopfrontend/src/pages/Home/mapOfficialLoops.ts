/**
 * 官方自主规划 AgentLoop → 首页卡片
 */
import { resolveOfficialCardVisual } from '@/pages/AgentCenter/lib/officialCardCovers';
import { buildOfficialLaunchChatPath, toOfficialLoopCard } from '@/pages/AgentCenter/utils';
import type { OfficialAgentLoopItem } from '@/types/officialCatalog';
import type { HomeAgentCard } from './mock';

const HOME_LOOP_LIMIT = 3;

/** 取官方 agent-loop 前 3 条；点击 CTA 进官方体验对话页 */
export function mapOfficialLoopsToHomeCards(
  loops: OfficialAgentLoopItem[] | undefined,
): HomeAgentCard[] {
  const list = Array.isArray(loops) ? loops : [];
  return list.slice(0, HOME_LOOP_LIMIT).map((raw) => {
    const item = toOfficialLoopCard(raw);
    const visual = resolveOfficialCardVisual(item);

    return {
      id: item.key,
      title: item.name,
      description: item.description || '官方 Agent-Loop 示范，点击立即进入对话体验。',
      status: 'live' as const,
      priceLabel: '官方示范',
      cta: '立即使用',
      href: buildOfficialLaunchChatPath(item),
      cover: visual.cover,
      thumb: visual.thumb,
      visual: 'media' as const,
    };
  });
}
