/**
 * 把团队作品轮次折叠成首页「灵感与作品」卡片。
 * 只保留可播放的 http(s) 封面；优先图片，避免灰底视频首帧。
 */
import type { PortfolioAssetSummary, PortfolioRound } from '@/pages/WorksGallery/types';
import type { HomeWorkPreview } from './mock';

export const HOME_WORKS_DISPLAY = 6;
export const HOME_WORKS_FETCH = 24;
export const HOME_WORKS_CANDIDATES = 12;

function isPlayableUrl(url: string): boolean {
  const trimmed = url.trim();
  return /^https?:\/\//i.test(trimmed);
}

function pickCover(round: PortfolioRound): PortfolioAssetSummary | null {
  const pool = [round.cover, ...round.resultAssets, ...round.intermediateAssets].filter(
    (asset): asset is PortfolioAssetSummary =>
      Boolean(asset?.assetUrl) && isPlayableUrl(asset.assetUrl),
  );
  return pool.find((asset) => asset.assetType === 'image') ?? pool[0] ?? null;
}

export function mapPortfolioRoundsToHomeWorks(rounds: PortfolioRound[]): HomeWorkPreview[] {
  const seen = new Set<string>();
  const works: HomeWorkPreview[] = [];
  for (const round of rounds) {
    const cover = pickCover(round);
    if (!cover) continue;
    const key = cover.assetUrl.split('?')[0];
    if (seen.has(key)) continue;
    seen.add(key);
    const prompt = round.promptText.trim() || '未填写创作指令';
    works.push({
      id: round.roundKey,
      title: prompt.length > 28 ? `${prompt.slice(0, 28)}…` : prompt,
      cover: cover.assetUrl,
      category: round.agentName?.trim() || round.sourceName || '团队作品',
      kind: cover.assetType,
    });
    if (works.length >= HOME_WORKS_CANDIDATES) break;
  }
  return works;
}
