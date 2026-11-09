/**
 * 漫剧成片 comic_drama_film：表单中文名 / 说明叠加
 * 字段与模版数据来自 capabilities / TEMPLATE 接口，不再前端兜底写死
 */
import type { OfficialUiField } from '@/types/officialCatalog';
import { bareWorkflowKey } from '@/utils/workflowKey';
import type { FieldSpec } from './executionSpecs';

export const COMIC_DRAMA_PIPELINE = 'comic_drama_film';

export function isComicDramaPipeline(name?: string): boolean {
  return bareWorkflowKey(name || '') === COMIC_DRAMA_PIPELINE;
}

interface ComicDramaFieldMeta {
  label: string;
  description: string;
  placeholder?: string;
}

const COMIC_DRAMA_FIELD_META: Record<string, ComicDramaFieldMeta> = {
  brief: {
    label: '题材 / 剧本',
    description: '题材、梗概或已带「第X集」的完整剧本，作为阶段 1 扩写成可拍摄剧本的原料。',
    placeholder: '输入题材或剧本梗概',
  },
  title: {
    label: '作品名',
    description: '有则剧本阶段必须用这个名；没有则模型起名，再不行就截 brief。',
    placeholder: '作品名',
  },
  stylePrompt: {
    label: '画风锚点',
    description: '全剧画风锚点。空则用默认「日漫赛璐珞」，定妆 / 场景 / 首帧 / 视频都会带上。',
    placeholder: '日漫赛璐珞（留空即用默认）',
  },
  episodes: {
    label: '目标集数',
    description: '1～10 集。brief 已是多集剧本时仍按切集结果走，但会被此上限截断。',
  },
  maxShots: {
    label: '每集最多分镜',
    description: '每一集最多几个视频片段（分镜），1～10。不是全集镜头总数。',
  },
  chatModel: {
    label: '文本模型',
    description: '写剧本、定妆描述、切集、资产描述、精简对白与分镜。',
  },
  imageModel: {
    label: '文生图模型',
    description: '人物定妆三视图、场景 / 道具（无参考图时）。',
  },
  editModel: {
    label: '图生图模型',
    description: '人物状态图（锁定妆）、首帧（锁场景 / 人物参考）。',
  },
  videoModel: {
    label: '视频模型',
    description: '逐镜视频，须为可灵 Omni。首帧 + 参考图，audio=native 出台词声。',
  },
  aspectRatio: {
    label: '画幅',
    description: '成片 / 首帧画幅意向，默认竖屏 9:16。有首帧后实际画幅跟首帧。',
  },
  resolution: {
    label: '清晰度',
    description: '视频清晰度，可灵常见 720p / 1080p / 4k。',
  },
  audio: {
    label: '声音策略',
    description: '默认 native 生成对白。off 无声，original 保留参考视频原声。漫剧建议 native。',
  },
};

/** 有 fields 时保持后端顺序和 widget，只补中文名和说明 */
function overlayComicDramaUiField(field: OfficialUiField): OfficialUiField {
  const meta = COMIC_DRAMA_FIELD_META[field.name];
  if (!meta) return field;
  return {
    ...field,
    label: meta.label,
    description: meta.description || field.description,
  };
}

/** 只叠加中文文案；接口没有 fields 时不再用前端整表兜底 */
export function mergeComicDramaUiFields(
  pipelineKey: string,
  fields: OfficialUiField[],
): OfficialUiField[] {
  if (!isComicDramaPipeline(pipelineKey)) return fields;
  return fields.map(overlayComicDramaUiField);
}

export function applyComicDramaFieldSpecOverlay(fields: FieldSpec[]): FieldSpec[] {
  return fields.map((field) => {
    const meta = COMIC_DRAMA_FIELD_META[field.key];
    if (!meta) return field;
    return {
      ...field,
      label: meta.label,
      hint: meta.description,
      placeholder: meta.placeholder ?? field.placeholder,
    };
  });
}
