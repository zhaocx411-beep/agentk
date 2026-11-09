/**
 * 官方目录卡片封面：按业务名称/描述匹配本地可商用写实图（Unsplash / Pexels）
 * 首页 Agent-Loop 与目录页共用同一套解析，避免外链热加载、避免规划类卡片共用一张图
 */
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import wfBrandCover from '@/assets/home/wf-brand-cover.jpg';
import wfBrandThumb from '@/assets/home/wf-brand-thumb.jpg';
import wfFilmCover from '@/assets/home/wf-film-cover.jpg';
import wfFilmThumb from '@/assets/home/wf-film-thumb.jpg';
import wfCinemaCover from '@/assets/home/wf-cinema-cover.jpg';
import wfCinemaThumb from '@/assets/home/wf-cinema-thumb.jpg';
import wfScrapeCover from '@/assets/home/wf-scrape-cover.jpg';
import wfScrapeThumb from '@/assets/home/wf-scrape-thumb.jpg';
import wfProductAdCover from '@/assets/home/wf-product-ad-cover.jpg';
import wfProductAdThumb from '@/assets/home/wf-product-ad-thumb.jpg';
import wfOutfitCover from '@/assets/home/wf-outfit-cover.jpg';
import wfOutfitThumb from '@/assets/home/wf-outfit-thumb.jpg';
import wfMangaCover from '@/assets/home/wf-manga-cover.jpg';
import wfMangaThumb from '@/assets/home/wf-manga-thumb.jpg';
import loopAssistantCover from '@/assets/home/loop-assistant-cover.jpg';
import loopAssistantThumb from '@/assets/home/loop-assistant-thumb.jpg';
import loopMangaCover from '@/assets/home/loop-manga-cover.jpg';
import loopMangaThumb from '@/assets/home/loop-manga-thumb.jpg';
import loopCreationCover from '@/assets/home/loop-creation-cover.jpg';
import loopCreationThumb from '@/assets/home/loop-creation-thumb.jpg';
import loopMediaCover from '@/assets/home/loop-media-cover.jpg';
import loopMediaThumb from '@/assets/home/loop-media-thumb.jpg';
import loopEditCover from '@/assets/home/loop-workspace-cover.jpg';
import loopEditThumb from '@/assets/home/loop-workspace-thumb.jpg';
import loopDocsCover from '@/assets/home/loop-docs-cover.jpg';
import loopDocsThumb from '@/assets/home/loop-docs-thumb.jpg';
import loopSpeechCover from '@/assets/home/loop-speech-cover.jpg';
import loopSpeechThumb from '@/assets/home/loop-speech-thumb.jpg';
import loopTryonCover from '@/assets/home/loop-tryon-cover.jpg';
import loopTryonThumb from '@/assets/home/loop-tryon-thumb.jpg';
import loopCreativeCover from '@/assets/home/loop-creative-cover.jpg';
import loopCreativeThumb from '@/assets/home/loop-creative-thumb.jpg';
import loopActivityCover from '@/assets/home/loop-activity-cover.jpg';
import loopActivityThumb from '@/assets/home/loop-activity-thumb.jpg';

export interface OfficialCardVisual {
  cover: string;
  thumb: string;
}

const WF_BRAND: OfficialCardVisual = { cover: wfBrandCover, thumb: wfBrandThumb };
const WF_FILM: OfficialCardVisual = { cover: wfFilmCover, thumb: wfFilmThumb };
const WF_CINEMA: OfficialCardVisual = { cover: wfCinemaCover, thumb: wfCinemaThumb };
/** 电商商品详情抓取：商品列表页 / 在线选品 */
const WF_SCRAPE: OfficialCardVisual = { cover: wfScrapeCover, thumb: wfScrapeThumb };
/** 电商商品广告成片：摄影棚拍广告片 */
const WF_PRODUCT_AD: OfficialCardVisual = { cover: wfProductAdCover, thumb: wfProductAdThumb };
/** 一键换装：模特成套穿搭 */
const WF_OUTFIT: OfficialCardVisual = { cover: wfOutfitCover, thumb: wfOutfitThumb };
/** 漫剧成片：数码分镜 / 成片流水线（与 Agent-Loop 印刷漫画书区分） */
const WF_MANGA: OfficialCardVisual = { cover: wfMangaCover, thumb: wfMangaThumb };

/** 官方全工具助手：保留原协作策划图 */
const LOOP_ASSISTANT: OfficialCardVisual = { cover: loopAssistantCover, thumb: loopAssistantThumb };
/** Activity Agent：多屏多模态制作工作台，体现图片、视频、音频和工具编排 */
const LOOP_ACTIVITY: OfficialCardVisual = { cover: loopActivityCover, thumb: loopActivityThumb };
/** 漫剧动态漫：漫画分镜书页 */
const LOOP_MANGA: OfficialCardVisual = { cover: loopMangaCover, thumb: loopMangaThumb };
/** 完整创作助手：多媒体制作工作台 */
const LOOP_CREATION: OfficialCardVisual = { cover: loopCreationCover, thumb: loopCreationThumb };
/** 图文视频创作：实拍取景 */
const LOOP_MEDIA: OfficialCardVisual = { cover: loopMediaCover, thumb: loopMediaThumb };
/** 视频剪辑与生成：剪辑工作台（与分镜工作流的场记板场景区分） */
const LOOP_EDIT: OfficialCardVisual = { cover: loopEditCover, thumb: loopEditThumb };
/** 文档解析与研究：案头资料与协作 */
const LOOP_DOCS: OfficialCardVisual = { cover: loopDocsCover, thumb: loopDocsThumb };
/** 语音合成与识别：录音棚话筒 */
const LOOP_SPEECH: OfficialCardVisual = { cover: loopSpeechCover, thumb: loopSpeechThumb };
/** 虚拟试穿：成衣与穿搭 */
const LOOP_TRYON: OfficialCardVisual = { cover: loopTryonCover, thumb: loopTryonThumb };
/** 未点名卡片兜底，避免和上面几张撞图 */
const LOOP_CREATIVE: OfficialCardVisual = { cover: loopCreativeCover, thumb: loopCreativeThumb };

const LOOP_FALLBACKS: readonly OfficialCardVisual[] = [LOOP_CREATIVE, LOOP_ASSISTANT];

function coverHaystack(item: OfficialCatalogCardData): string {
  return [item.key, item.workflowKey, item.pipelineName, item.name, item.description, item.subtitle]
    .filter(Boolean)
    .join(' ')
    .toLowerCase();
}

function loopFallbackIndex(key: string): number {
  let hash = 0;
  for (let i = 0; i < key.length; i += 1) {
    hash = (hash + key.charCodeAt(i)) % LOOP_FALLBACKS.length;
  }
  return hash;
}

/** 按 workflowKey / 业务名称匹配封面+缩略图；未知卡片走稳定哈希兜底 */
export function resolveOfficialCardVisual(item: OfficialCatalogCardData): OfficialCardVisual {
  const haystack = coverHaystack(item);

  if (item.variant === 'workflow') {
    if (/generate_brand_ad|brand_ad/.test(haystack)) return WF_BRAND;
    if (/storyboard|story_board/.test(haystack)) return WF_FILM;
    if (/ffmpeg/.test(haystack)) return WF_CINEMA;
    if (/换装|clothing.?dupe|一键复刻/.test(haystack)) return WF_OUTFIT;
    if (/抓取|商品详情|product.?detail|fetch_product|scrape|goods.?scraper/.test(haystack)) {
      return WF_SCRAPE;
    }
    if (/广告成片|brand.?studio|goods.?studio/.test(haystack)) return WF_PRODUCT_AD;
    if (/漫剧|动态漫|comic_drama|comic.?drama/.test(haystack)) return WF_MANGA;
  }

  if (/漫剧|动态漫|manga|comic/.test(haystack)) return LOOP_MANGA;
  if (/视频剪辑|剪辑与生成/.test(haystack)) return LOOP_EDIT;
  if (/图文视频/.test(haystack)) return LOOP_MEDIA;
  if (/完整创作|整套创作/.test(haystack)) return LOOP_CREATION;
  if (/activity.?agent/.test(haystack)) return LOOP_ACTIVITY;
  if (/全工具/.test(haystack)) return LOOP_ASSISTANT;
  if (/文档解析|文档研究|表格解析|文件解析|文档分析/.test(haystack)) return LOOP_DOCS;
  if (/语音合成|语音转写|语音识别|\btts\b|\basr\b/.test(haystack)) return LOOP_SPEECH;
  if (/虚拟试穿|试衣/.test(haystack)) return LOOP_TRYON;

  return LOOP_FALLBACKS[loopFallbackIndex(item.key)];
}

/** 按 workflowKey / 名称匹配封面；未知卡片走稳定哈希兜底 */
export function resolveOfficialCardCover(item: OfficialCatalogCardData): string {
  return resolveOfficialCardVisual(item).cover;
}
