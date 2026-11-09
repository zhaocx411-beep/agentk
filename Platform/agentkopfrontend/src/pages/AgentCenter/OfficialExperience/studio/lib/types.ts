/**
 * 官方体验创作工作台的数据模型
 * 模版驱动：用户只选模版 + 选一条样本数据，其余字段由模版补齐
 */
import type { ExecutionSpecId } from './executionSpecs';

/** 模版数据样本的一行（字段对齐运营给的测试样本表） */
export interface SampleRow {
  id: string;
  /** 类目，仅用于列表分组与检索 */
  category: string;
  /** 商品标题 */
  title: string;
  /** 商品链接 */
  link: string;
  /** 首图，第一张作为封面展示，也可一键转成参考图 */
  coverUrls: string[];
  /** 商品价格（含币种，直接进 prompt） */
  price: string;
  /** 商品详情图 */
  detailImages: string[];
  /** 商品属性信息，多个属性以 / 连接 */
  attributes: string;
  /**
   * 模版原始入参（对齐 pipeline args / capabilities 字段名）
   * 如 title、video_prompt、images，供动态表单回填
   */
  templateArgs?: Record<string, unknown>;
}

/** 执行链路的一段（模版声明的预期步骤，未开跑时作为占位展示） */
export interface StageSpec {
  key: string;
  title: string;
  /** 执行方标签，如 LLM / KLING / VLM */
  badge: string;
}

/** 创作模版 */
export interface CreationTemplate {
  id: string;
  /** 适用的执行方式；自主规划（agent_loop）能用全部模版，Pipeline 只用各自的 */
  specs: ExecutionSpecId[];
  name: string;
  description: string;
  /** 创作场景，落到「更多设置」的默认值 */
  scene: string;
  tags: string[];
  /** 模版内置 prompt，支持 {{商品标题}} / {{商品价格}} / {{商品属性}} / {{商品链接}} 占位 */
  prompt: string;
  /** 意图还原里展示的历史记忆要点 */
  memory: string[];
  /** 参考图数量上限 */
  refImageLimit: number;
  stages: StageSpec[];
  /** 适配的样本类目，命中的样本在列表里排前面；不限制可选范围 */
  preferredCategories: string[];
}

export type PipelineStatus = 'done' | 'running' | 'failed' | 'pending';

/** 执行链路上的一个节点 */
export interface PipelineNode {
  key: string;
  /** 序号标签，如 A1 */
  index: string;
  title: string;
  badge: string;
  status: PipelineStatus;
  /** 一句话摘要，失败时为错误信息 */
  detail?: string;
  durationMs?: number;
  input?: unknown;
  output?: unknown;
}

import type { ArtifactTier } from './artifactTier';

/** 底部产物区的一个条目 */
export interface ArtifactItem {
  url: string;
  kind: 'video' | 'image' | 'audio';
  /** 产出该文件的步骤名 */
  from: string;
  /** final=漫剧 compose 合成成片；segment=镜头/首帧等中间产物 */
  tier?: ArtifactTier;
  /** 产出工具名 */
  sourceTool?: string;
  /** 接口 artifacts[].stage 原值（film/clips/…） */
  sourceStage?: string;
}

/** 「更多设置」里的可选项 */
export interface CreationSettings {
  scene: string;
  project: string;
  tag: string;
}

/** 参考图（media/upload 返回的 fileUrl，或手填的外链） */
export interface ReferenceImage {
  uid: string;
  name: string;
  url: string;
}

/** 一次生成记录 */
export interface GenerationRecord {
  id: string;
  /** 关联的执行 ID：agent-loop 是 runId，pipeline 是 execId，用于定位这条记录的输出 */
  runId?: string;
  templateName: string;
  /** 发起时的创作场景，列表里作为标签展示 */
  scene: string;
  prompt: string;
  /** 发起时的意图快照，选中这条记录时右栏展示它而不是当前表单 */
  intentPreview: string;
  /** 发起时实际提交的入参 / 对话文本快照 */
  payloadPreview: string;
  /**
   * running：生成中；waiting：等用户回答（ask_user）；
   * done：成功；failed：失败；cancelled：用户停止；
   * null：账本尚未就绪或尚无信号，列表不展示状态（避免默认「成功」闪一下）
   */
  status: 'running' | 'waiting' | 'done' | 'failed' | 'cancelled' | null;
  createdAt: number;
  error?: string;
}
