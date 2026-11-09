/**
 * 首页官方工作流 / 自主规划卡片配图（Unsplash 可商用写实素材，本地缓存）
 * Agent-Loop 封面按业务名匹配，见 officialCardCovers.resolveOfficialCardVisual
 */
import wfBrandCover from '@/assets/home/wf-brand-cover.jpg';
import wfBrandThumb from '@/assets/home/wf-brand-thumb.jpg';
import wfFilmCover from '@/assets/home/wf-film-cover.jpg';
import wfFilmThumb from '@/assets/home/wf-film-thumb.jpg';
import wfCinemaCover from '@/assets/home/wf-cinema-cover.jpg';
import wfCinemaThumb from '@/assets/home/wf-cinema-thumb.jpg';

export interface HomeCardVisual {
  cover: string;
  thumb: string;
}

/** 官方工作流：商品广告 / 分镜片场 / 后期剪辑 */
export const HOME_WORKFLOW_VISUALS: readonly HomeCardVisual[] = [
  { cover: wfBrandCover, thumb: wfBrandThumb },
  { cover: wfFilmCover, thumb: wfFilmThumb },
  { cover: wfCinemaCover, thumb: wfCinemaThumb },
] as const;
