/** 把 TEMPLATE extraFields 解析成 keffects 工作台表单值 */
import type { MediaAsset } from '@/components';
import type { ClothingDupeMedia } from '../clothingDupe/mediaFields';
import { MEDIA_LIMIT_BY_FIELD } from '../clothingDupe/mediaFields';
import {
  CLOTHING_DUPE_ASPECT_RATIOS,
  GOODS_STUDIO_ASPECT_RATIOS,
  GOODS_STUDIO_DURATIONS,
  GOODS_STUDIO_MAX_REF_IMAGES,
  KEFFECTS_RESOLUTIONS,
  type ClothingDupeAspectRatio,
  type GoodsStudioAspectRatio,
  type GoodsStudioDuration,
  type KeffectsResolution,
} from './constants';

function asRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value === 'string') {
    const trimmed = value.trim();
    if (!trimmed) return null;
    try {
      return asRecord(JSON.parse(trimmed) as unknown);
    } catch {
      return null;
    }
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function asString(value: unknown): string {
  if (typeof value === 'string') return value.trim();
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return '';
}

function asNumber(value: unknown): number | null {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
}

interface TemplateContent {
  type: string;
  url: string;
  text: string;
  voiceId: string;
}

function contentsOf(extra: Record<string, unknown>): TemplateContent[] {
  const raw = asRecord(extra.input)?.contents ?? extra.contents;
  if (!Array.isArray(raw)) return [];
  return raw
    .map((item) => {
      const rec = asRecord(item);
      return {
        type: asString(rec?.type),
        url: asString(rec?.url),
        text: asString(rec?.text),
        voiceId: asString(rec?.voiceId),
      };
    })
    .filter((item) => item.type);
}

function settingsOf(extra: Record<string, unknown>): Record<string, unknown> | null {
  return asRecord(asRecord(extra.input)?.settings) ?? asRecord(extra.settings);
}

function settingString(settings: Record<string, unknown> | null, keys: string[]): string {
  if (!settings) return '';
  for (const key of keys) {
    const value = asString(settings[key]);
    if (value) return value;
  }
  return '';
}

function toMedia(url: string, index: number, prefix: string): ClothingDupeMedia {
  const name = url.split('/').pop()?.split('?')[0] || url;
  return { id: `template-${prefix}-${index}`, name, url };
}

function asResolution(value: string): KeffectsResolution | undefined {
  return (KEFFECTS_RESOLUTIONS as readonly string[]).includes(value)
    ? (value as KeffectsResolution)
    : undefined;
}

function asClothingAspect(value: string): ClothingDupeAspectRatio | undefined {
  return (CLOTHING_DUPE_ASPECT_RATIOS as readonly string[]).includes(value)
    ? (value as ClothingDupeAspectRatio)
    : undefined;
}

function asStudioAspect(value: string): GoodsStudioAspectRatio | undefined {
  return (GOODS_STUDIO_ASPECT_RATIOS as readonly string[]).includes(value)
    ? (value as GoodsStudioAspectRatio)
    : undefined;
}

function asDuration(value: number | null): GoodsStudioDuration | undefined {
  if (value == null) return undefined;
  return (GOODS_STUDIO_DURATIONS as readonly number[]).includes(value)
    ? (value as GoodsStudioDuration)
    : undefined;
}

export interface ClothingDupeTemplateValues {
  prompt: string;
  voiceId: string;
  sourceVideo: ClothingDupeMedia[];
  productImages: ClothingDupeMedia[];
  modelImages: ClothingDupeMedia[];
  bgm: ClothingDupeMedia[];
  resolution?: KeffectsResolution;
  aspectRatio?: ClothingDupeAspectRatio;
}

export function parseClothingDupeTemplate(
  extra: Record<string, unknown>,
): ClothingDupeTemplateValues {
  const contents = contentsOf(extra);
  const settings = settingsOf(extra);
  const promptFromContent = contents.find((item) => item.type === 'prompt')?.text ?? '';
  return {
    prompt: asString(extra.video_prompt) || promptFromContent,
    voiceId: contents.find((item) => item.type === 'voice')?.voiceId ?? '',
    sourceVideo: contents
      .filter((item) => item.type === 'source_video' && item.url)
      .map((item, index) => toMedia(item.url, index, 'source'))
      .slice(0, MEDIA_LIMIT_BY_FIELD.sourceVideo),
    productImages: contents
      .filter((item) => item.type === 'product_image' && item.url)
      .map((item, index) => toMedia(item.url, index, 'product'))
      .slice(0, MEDIA_LIMIT_BY_FIELD.productImages),
    modelImages: contents
      .filter((item) => item.type === 'model_image' && item.url)
      .map((item, index) => toMedia(item.url, index, 'model'))
      .slice(0, MEDIA_LIMIT_BY_FIELD.modelImages),
    bgm: contents
      .filter((item) => item.type === 'bgm' && item.url)
      .map((item, index) => toMedia(item.url, index, 'bgm'))
      .slice(0, MEDIA_LIMIT_BY_FIELD.bgm),
    resolution: asResolution(settingString(settings, ['resolution'])),
    aspectRatio: asClothingAspect(settingString(settings, ['aspectRatio', 'aspect_ratio'])),
  };
}

export function parseGoodsScraperTemplate(extra: Record<string, unknown>): { url: string } {
  const fromInput = asString(asRecord(extra.input)?.url);
  if (fromInput) return { url: fromInput };
  const fromRoot = asString(extra.url);
  if (fromRoot) return { url: fromRoot };
  const fromContent = contentsOf(extra).find((item) => item.type === 'url' || item.url)?.url ?? '';
  return { url: fromContent };
}

export interface GoodsStudioTemplateValues {
  refImages: MediaAsset[];
  title: string;
  description: string;
  resolution?: KeffectsResolution;
  aspectRatio?: GoodsStudioAspectRatio;
  duration?: GoodsStudioDuration;
}

function toImageAsset(url: string, index: number): MediaAsset {
  const name = url.split('/').pop()?.split('?')[0] || url;
  return { id: `template-ref-${index}`, url, name, mediaType: 'image', source: 'material' };
}

export function parseGoodsStudioTemplate(extra: Record<string, unknown>): GoodsStudioTemplateValues {
  const contents = contentsOf(extra);
  const settings = settingsOf(extra);
  const title = contents.find((item) => item.type === 'goods_title')?.text || asString(extra.title);
  const description = contents.find((item) => item.type === 'goods_description')?.text || '';
  const fromContents = contents
    .filter((item) => item.type === 'ref_image' && item.url)
    .map((item) => item.url);
  const fromImages = Array.isArray(extra.images)
    ? extra.images.map((item) => asString(item)).filter(Boolean)
    : [];
  const urls = (fromContents.length > 0 ? fromContents : fromImages).slice(
    0,
    GOODS_STUDIO_MAX_REF_IMAGES,
  );
  return {
    refImages: urls.map(toImageAsset),
    title,
    description,
    resolution: asResolution(settingString(settings, ['resolution'])),
    aspectRatio: asStudioAspect(settingString(settings, ['aspectRatio', 'aspect_ratio'])),
    duration: asDuration(asNumber(settings?.duration)),
  };
}
