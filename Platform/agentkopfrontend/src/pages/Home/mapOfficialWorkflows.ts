/**
 * 官方工作流 → 首页智能体卡片（保留首页卡片 UI，文案与跳转走官方目录）
 */
import { resolveOfficialCardVisual } from '@/pages/AgentCenter/lib/officialCardCovers';
import { buildOfficialLaunchChatPath, toOfficialWorkflowCard } from '@/pages/AgentCenter/utils';
import type { OfficialWorkflowItem } from '@/types/officialCatalog';
import type { HomeAgentCard } from './mock';

const HOME_WORKFLOW_LIMIT = 3;

/** 取官方工作流前 3 条，映射为首页卡片；点击 CTA 直达体验页 */
export function mapOfficialWorkflowsToHomeCards(
  workflows: OfficialWorkflowItem[] | undefined,
): HomeAgentCard[] {
  const list = Array.isArray(workflows) ? workflows : [];
  return list.slice(0, HOME_WORKFLOW_LIMIT).map((raw) => {
    const item = toOfficialWorkflowCard(raw);
    const visual = resolveOfficialCardVisual(item);

    return {
      id: item.key,
      title: item.name,
      description: item.description || '官方工作流示范，点击立即进入创作页使用。',
      status: 'live' as const,
      priceLabel: '官方示范',
      cta: '立即体验',
      href: buildOfficialLaunchChatPath(item),
      cover: visual.cover,
      thumb: visual.thumb,
    };
  });
}
