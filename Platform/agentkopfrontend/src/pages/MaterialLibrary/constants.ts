import type { MaterialCategory, MaterialCategoryFilter, MaterialKind } from './types';

export type KindFilter = 'all' | MaterialKind;

export const KIND_OPTIONS: { label: string; value: KindFilter }[] = [
  { label: '全部', value: 'all' },
  { label: '图片', value: 'image' },
  { label: '音频', value: 'audio' },
  { label: '视频', value: 'video' },
];

/** 素材库 extraFields.category 枚举（pipelineName=asset） */
export const MATERIAL_CATEGORIES = [
  'product_image',
  'model_image',
  'style_ref_image',
  'audio',
  'ecommerce_ad',
  'runway',
  'short_drama',
  'broll',
] as const satisfies readonly MaterialCategory[];

const CATEGORY_LABELS: Record<MaterialCategory, string> = {
  product_image: '商品图',
  model_image: '模特图',
  style_ref_image: '风格/场景参考图',
  audio: '音频',
  ecommerce_ad: '电商广告视频',
  runway: '走秀/试穿视频',
  short_drama: '短剧/漫剧',
  broll: '空镜/氛围',
};

export const MATERIAL_CATEGORY_OPTIONS: { label: string; value: MaterialCategoryFilter }[] = [
  { label: '全部分类', value: 'all' },
  ...MATERIAL_CATEGORIES.map((value) => ({
    value,
    label: CATEGORY_LABELS[value],
  })),
];

export function materialCategoryLabel(category: MaterialCategory): string {
  return CATEGORY_LABELS[category];
}

/** category → mediaType，用于与弹层 kinds 筛选联动 */
export function materialCategoryKind(category: MaterialCategory): MaterialKind {
  if (category === 'audio') return 'audio';
  if (
    category === 'ecommerce_ad' ||
    category === 'runway' ||
    category === 'short_drama' ||
    category === 'broll'
  ) {
    return 'video';
  }
  return 'image';
}

/** 某 mediaType 在素材库中对应的 category 列表（不含「全部分类」） */
export function materialCategoriesForKind(kind: MaterialKind): MaterialCategory[] {
  return MATERIAL_CATEGORIES.filter((category) => materialCategoryKind(category) === kind);
}

/** 弹层仅选一种媒体时，默认素材库分类（避免「全部分类」首屏全是图片/视频导致列表为空） */
export function defaultMaterialCategoryForKind(kind: MaterialKind): MaterialCategoryFilter {
  const matches = materialCategoriesForKind(kind);
  if (kind === 'audio') return 'audio';
  return matches[0] ?? 'all';
}

export function defaultMaterialCategoryForKinds(kinds: MaterialKind[]): MaterialCategoryFilter {
  if (kinds.length !== 1) return 'all';
  return defaultMaterialCategoryForKind(kinds[0]);
}

const KIND_GROUP_LABELS: Record<MaterialKind, string> = {
  image: '图片',
  audio: '音频',
  video: '视频',
};

type CategorySelectOption =
  | { label: string; value: MaterialCategoryFilter }
  | {
      label: string;
      options: { label: string; value: MaterialCategoryFilter }[];
    };

/** 素材库页 / 弹层：按 mediaType 筛选可选分类；全部类型时分组展示 */
export function materialCategoryOptionsForKind(kind: KindFilter): CategorySelectOption[] {
  if (kind !== 'all') {
    return materialCategoriesForKind(kind).map((value) => ({
      value,
      label: materialCategoryLabel(value),
    }));
  }

  return materialCategoryOptionsForKinds(['image', 'audio', 'video']);
}

/** 弹层按当前允许的 kinds 展示分类；多种类型时按图片/音频/视频分组 */
export function materialCategoryOptionsForKinds(kinds: MaterialKind[]): CategorySelectOption[] {
  if (kinds.length === 1) {
    return materialCategoryOptionsForKind(kinds[0]);
  }

  return [
    { label: '全部分类', value: 'all' },
    ...(['image', 'audio', 'video'] as const)
      .filter((mediaKind) => kinds.includes(mediaKind))
      .map((mediaKind) => ({
        label: KIND_GROUP_LABELS[mediaKind],
        options: materialCategoriesForKind(mediaKind).map((value) => ({
          value,
          label: materialCategoryLabel(value),
        })),
      })),
  ];
}

/** 切换 mediaType 时同步 category，避免「全部分类 + 单类型」首屏为空 */
export function resolveMaterialCategoryForKind(
  kind: KindFilter,
  current: MaterialCategoryFilter,
): MaterialCategoryFilter {
  if (kind === 'all') return current;
  const matches = materialCategoriesForKind(kind);
  if (current !== 'all' && matches.includes(current)) return current;
  return defaultMaterialCategoryForKind(kind);
}
