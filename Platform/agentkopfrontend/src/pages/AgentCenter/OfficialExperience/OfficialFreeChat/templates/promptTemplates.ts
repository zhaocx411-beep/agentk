/**
 * 官方自主 Agent 自由对话的空态引导文案
 * 快捷灵感仅来自 TEMPLATE 接口，此处不再维护 mock 提问
 */
import type { OfficialCatalogCardData } from '@/types/officialCatalog';

export type OfficialLoopProfile = 'manga' | 'full_creation' | 'media' | 'video_edit' | 'tts' | 'document' | 'virtual_tryon' | 'general';

export interface OfficialFreeChatGuide {
  profile: OfficialLoopProfile;
  emptyTitle: string;
  emptyDescription: string;
}

const GUIDES: Record<OfficialLoopProfile, Omit<OfficialFreeChatGuide, 'profile'>> = {
  manga: {
    emptyTitle: '从题材写成一部能拍的动态漫',
    emptyDescription:
      '写下题材、梗概或剧本。Agent 会按定妆、分镜、成片逐步调用工具，过程可在创作画布里查看。',
  },
  full_creation: {
    emptyTitle: '从一个想法做成整套创作交付',
    emptyDescription: '说清主题和要交付的文本、图片或视频。助手会串起从构想到成片的整套流程。',
  },
  media: {
    emptyTitle: '生成、编辑图片和视频',
    emptyDescription: '上传参考图或视频，或直接描述画面。Agent 会生成、改稿，并理解已有媒体素材。',
  },
  video_edit: {
    emptyTitle: '剪辑、合成与生成视频',
    emptyDescription: '上传素材或写下分镜。Agent 会调用裁剪、合成、字幕和音频工具完成成片。',
  },
  tts: {
    emptyTitle: '文字转语音、语音转写与翻译',
    emptyDescription:
      '输入要合成的文本，并选择语音引擎与音色。Agent 会调用 TTS / ASR 工具完成合成与识别。',
  },
  document: {
    emptyTitle: '上传文档，解析表格与研究结论',
    emptyDescription:
      '请通过下方按钮上传 Excel / PDF 等文档。Agent 会读取文件并整理表头、样例与可读结论。',
  },
  virtual_tryon: {
    emptyTitle: '上传商品图，生成虚拟试穿效果',
    emptyDescription:
      '上传服装商品图，可选上传模特参考图，并在下方描述试穿场景（如棚拍、全身、画幅比例）。',
  },
  general: {
    emptyTitle: '图文、视频与创意策划，一站完成',
    emptyDescription: '描述你的目标，或上传图片、视频和文件，助手会选择合适的工具完成创作。',
  },
};

/** 用名称 + 描述识别业务，顺序从更具体到更宽 */
export function resolveOfficialLoopProfile(
  item: OfficialCatalogCardData | null,
): OfficialLoopProfile {
  if (!item || item.variant !== 'planning') return 'general';
  const text = `${item.name} ${item.description}`;
  if (/漫剧|动态漫/.test(text)) return 'manga';
  if (/语音合成|语音转写|TTS|ASR/.test(text)) return 'tts';
  if (/文档解析|文档研究|表格解析|文件解析|文档分析/.test(text)) return 'document';
  if (/虚拟试穿/.test(text)) return 'virtual_tryon';
  if (/完整创作/.test(text)) return 'full_creation';
  if (/剪辑|字幕|音频提取/.test(text)) return 'video_edit';
  if (/图文视频|生成与编辑图片/.test(text)) return 'media';
  return 'general';
}

/** 当前官方 Agent 的空态标题与说明 */
export function getOfficialFreeChatGuide(
  item: OfficialCatalogCardData | null,
): OfficialFreeChatGuide {
  const profile = resolveOfficialLoopProfile(item);
  return { profile, ...GUIDES[profile] };
}
