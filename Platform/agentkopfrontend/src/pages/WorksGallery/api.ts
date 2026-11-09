import { get, post } from '@/utils/request';
import type { PortfolioAssetDetail, PortfolioPage, PortfolioQueryParams } from './types';

const PORTFOLIO_BASE = '/agent/operation/backend/platform/chat/portfolio';
const MY_PORTFOLIO_BASE = '/agent/operation/backend/platform/chat/my-portfolio';

type AgentNamesData =
  | string[]
  | {
      agentNames?: string[];
      list?: string[];
      records?: string[];
    };

export const portfolioQueryKey = (params: PortfolioQueryParams) =>
  ['portfolio', 'rounds', params] as const;

export const portfolioAssetKey = (assetId?: number) => ['portfolio', 'asset', assetId] as const;

export const myPortfolioQueryKey = (params: PortfolioQueryParams) =>
  ['my-portfolio', 'rounds', params] as const;

export const portfolioAgentNamesKey = (mine: boolean) =>
  [mine ? 'my-portfolio' : 'portfolio', 'agent-names'] as const;

function normalizeAgentNames(data: AgentNamesData): string[] {
  const names = Array.isArray(data) ? data : data.agentNames || data.list || data.records || [];
  return Array.from(new Set(names.filter(Boolean)));
}

/** 按创作轮次分页查询作品 */
export function queryPortfolio(params: PortfolioQueryParams) {
  return post<PortfolioPage>(`${PORTFOLIO_BASE}/query`, params);
}

/** 查询当前登录用户的作品集 */
export function queryMyPortfolio(params: PortfolioQueryParams) {
  return post<PortfolioPage>(`${MY_PORTFOLIO_BASE}/query`, params);
}

/** 获取作品集中的 Agent 名称筛选项 */
export async function getPortfolioAgentNames(mine: boolean) {
  const base = mine ? MY_PORTFOLIO_BASE : PORTFOLIO_BASE;
  const data = await get<AgentNamesData>(`${base}/agent-names`);
  return normalizeAgentNames(data);
}

/** 查询单个作品详情 */
export async function getPortfolioAsset(assetId: number) {
  const raw = await get<PortfolioAssetDetail>(`${PORTFOLIO_BASE}/assets/${assetId}`);
  return normalizePortfolioAssetDetail(raw);
}

function normalizePortfolioAssetDetail(raw: PortfolioAssetDetail): PortfolioAssetDetail {
  return {
    ...raw,
    promptText: raw.promptText ?? '',
    sameRoundAssets: Array.isArray(raw.sameRoundAssets) ? raw.sameRoundAssets : [],
  };
}
