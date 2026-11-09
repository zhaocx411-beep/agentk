/**
 * 工具展示元数据：图标色相、中文说明、分类标签
 * 未知工具走兜底文案，保证目录扩展后仍可渲染
 */
import type { ComponentType } from 'react';
import {
  ApiOutlined,
  AudioOutlined,
  ClockCircleOutlined,
  CloudDownloadOutlined,
  CommentOutlined,
  EditOutlined,
  FileImageOutlined,
  FolderOpenOutlined,
  GlobalOutlined,
  NodeIndexOutlined,
  PictureOutlined,
  QuestionCircleOutlined,
  ScissorOutlined,
  SearchOutlined,
  ShoppingOutlined,
  SoundOutlined,
  ToolOutlined,
  VideoCameraOutlined,
} from '@ant-design/icons';

export type ToolAccent = 'indigo' | 'sky' | 'emerald' | 'amber' | 'rose' | 'violet';

export interface ToolMeta {
  /** 中文展示名 */
  title: string;
  /** 一句话能力说明 */
  description: string;
  /** 分类标签文案 */
  category: string;
  /** 图标色相（仅影响图标底，不改变选中态品牌色） */
  accent: ToolAccent;
  Icon: ComponentType;
}

const FALLBACK: ToolMeta = {
  title: '未命名工具',
  description: '扩展 Agent 可调用的能力',
  category: '工具',
  accent: 'indigo',
  Icon: ToolOutlined,
};

/** 已知工具的展示信息；key 与 catalog 工具名一致 */
const TOOL_META: Record<string, ToolMeta> = {
  echo: {
    title: '回显调试',
    description: '回显输入内容，用于调试与连通性验证',
    category: '系统',
    accent: 'sky',
    Icon: SoundOutlined,
  },
  current_time: {
    title: '当前时间',
    description: '获取当前系统时间，辅助时效性判断',
    category: '系统',
    accent: 'sky',
    Icon: ClockCircleOutlined,
  },
  /** 遗留统一视频工具名；新实例优先使用 kling_* / seedance_* */
  generate_video: {
    title: '视频生成（遗留）',
    description: '根据文本或图片提示生成视频内容；新能力请改用厂商细粒度视频工具',
    category: '媒体',
    accent: 'violet',
    Icon: VideoCameraOutlined,
  },
  generate_image: {
    title: '图像生成',
    description: '根据提示词生成高质量图像',
    category: '媒体',
    accent: 'violet',
    Icon: PictureOutlined,
  },
  edit_image: {
    title: '图像编辑',
    description: '对已有图像进行编辑、改图与增强',
    category: '媒体',
    accent: 'amber',
    Icon: EditOutlined,
  },
  understand_media: {
    title: '媒体理解',
    description: '理解图片、视频等多模态内容语义',
    category: '媒体',
    accent: 'emerald',
    Icon: SearchOutlined,
  },
  understand_video_segment: {
    title: '视频分段理解',
    description: '按时间区间精细理解视频分镜、对白与运镜',
    category: '媒体',
    accent: 'emerald',
    Icon: SearchOutlined,
  },
  generate_brand_ad: {
    title: '品牌广告',
    description: '生成品牌广告创意与成片能力',
    category: '工作流',
    accent: 'rose',
    Icon: NodeIndexOutlined,
  },
  concat_videos: {
    title: '视频拼接',
    description: '拼接多段视频素材为完整成片',
    category: '媒体',
    accent: 'violet',
    Icon: VideoCameraOutlined,
  },
  kling_text_to_video: {
    title: 'Kling 文生视频',
    description: '纯文本描述从零生成视频',
    category: '媒体',
    accent: 'violet',
    Icon: VideoCameraOutlined,
  },
  kling_image_to_video: {
    title: 'Kling 图生视频',
    description: '以首帧图驱动生成视频',
    category: '媒体',
    accent: 'violet',
    Icon: VideoCameraOutlined,
  },
  kling_omni_video: {
    title: 'Kling Omni 视频',
    description: '参考图/视频保持主体一致或改写已有视频',
    category: '媒体',
    accent: 'violet',
    Icon: VideoCameraOutlined,
  },
  seedance_generate_video: {
    title: 'Seedance 视频生成',
    description: 'Seedance 方言视频生成，支持参考音频与特殊画幅',
    category: '媒体',
    accent: 'violet',
    Icon: VideoCameraOutlined,
  },
  probe_video: {
    title: '视频探测',
    description: '读取时长、分辨率、帧率、音轨等技术元数据',
    category: '剪辑',
    accent: 'amber',
    Icon: VideoCameraOutlined,
  },
  process_video: {
    title: '视频处理',
    description: '裁剪、缩放、变速、静音等单视频处理',
    category: '剪辑',
    accent: 'amber',
    Icon: ScissorOutlined,
  },
  compose_video: {
    title: '视频合成',
    description: '多段合成，可叠加音轨与字幕烧录',
    category: '剪辑',
    accent: 'amber',
    Icon: ScissorOutlined,
  },
  extract_audio: {
    title: '提取音频',
    description: '从视频提取受管音频产物',
    category: '剪辑',
    accent: 'amber',
    Icon: AudioOutlined,
  },
  extract_frame: {
    title: '提取帧',
    description: '从视频指定时间点截取图片',
    category: '剪辑',
    accent: 'amber',
    Icon: FileImageOutlined,
  },
  generate_speech: {
    title: '语音合成',
    description: '文本转语音（TTS），支持多厂商音色',
    category: '语音',
    accent: 'sky',
    Icon: AudioOutlined,
  },
  transcribe_audio: {
    title: '语音识别',
    description: '语音转文字（ASR），支持转写与翻译',
    category: '语音',
    accent: 'sky',
    Icon: SoundOutlined,
  },
  job_status: {
    title: '任务状态',
    description: '查询异步任务执行进度与结果',
    category: '系统',
    accent: 'sky',
    Icon: CloudDownloadOutlined,
  },
  list_artifacts: {
    title: '产物列表',
    description: '列出会话过程中产生的产物文件',
    category: '系统',
    accent: 'sky',
    Icon: FolderOpenOutlined,
  },
  web_search: {
    title: '网页搜索',
    description: '检索公开网页摘要，用于事实与时效信息',
    category: '联网',
    accent: 'emerald',
    Icon: GlobalOutlined,
  },
  web_fetch: {
    title: '网页抓取',
    description: '抓取并解析网页正文，供 Agent 引用',
    category: '联网',
    accent: 'emerald',
    Icon: ApiOutlined,
  },
  fetch_product_detail: {
    title: '商品详情抓取',
    description: '解析淘宝/天猫/Amazon 商品页结构化信息与图片',
    category: '联网',
    accent: 'emerald',
    Icon: ShoppingOutlined,
  },
  ask_user: {
    title: '询问用户',
    description: '向用户发起澄清或确认，补齐关键信息',
    category: '交互',
    accent: 'amber',
    Icon: CommentOutlined,
  },
  storyboard_film: {
    title: '分镜成片',
    description: '分镜脚本驱动的影片生成与编排',
    category: '工作流',
    accent: 'rose',
    Icon: FileImageOutlined,
  },
};

/** 按工具名解析展示元数据，未知工具回落兜底 */
export function getToolMeta(name: string): ToolMeta {
  return (
    TOOL_META[name] ?? {
      ...FALLBACK,
      title: name,
      description: `启用 ${name} 能力`,
      Icon: QuestionCircleOutlined,
    }
  );
}

/** 从工具目录提取分类列表（含「全部」之外的去重分类） */
export function listToolCategories(toolNames: string[]): string[] {
  return Array.from(new Set(toolNames.map((n) => getToolMeta(n).category))).sort();
}
