/**
 * 官方体验页的执行方式定义（单一事实来源）
 *
 * 页面有两种执行模式，由 URL 上的 type 决定：
 * - planning  → agent-loop：POST /sessions/{id}/messages，模版与样本被处理成一段 text 对话参数
 * - workflow  → pipeline  ：POST /sessions/{id}/pipeline-runs，入参字段是 args（注意不是通用接口的 inputs），
 *                            具体 Pipeline 由 URL 上的 workflowKey 决定
 *
 * 工作流优先用 capabilities 下发的 fields / uiSchema 动态生成侧栏（见 schemaFields.ts）；
 * 无 schema 时再回退到下方硬编码 PIPELINE_SPECS（兼容旧接口）。
 */
import type { ModelOptionsKind } from '@/utils/modelField';
import { bareWorkflowKey } from '@/utils/workflowKey';
import type { CreationSettings, SampleRow } from './types';

export type ExecutionMode = 'agent-loop' | 'pipeline';

/** 内置增强过的 Pipeline；其余 workflowKey 走 schema 动态表单 */
export type OfficialPipelineName = 'generate_brand_ad' | 'storyboard_film';

/** agent-loop 模式的执行标识，与 Pipeline 名同属一个命名空间 */
export const AGENT_LOOP_ID = 'agent_loop';

/** workflowKey 或 agent_loop；schema 驱动时可为任意后端下发的 key */
export type ExecutionSpecId = string;

/* ========== 各模式的请求载荷 ========== */

/** 品牌广告片：商品图 + 营销诉求直接出片 */
export interface GenerateBrandAdArgs {
  /** 广告标题 / 业务类型，如「电商」 */
  title: string;
  /** 商品图片 URL 列表，至少 1 个 */
  images: string[];
  /** 视频内容、风格、营销诉求 */
  video_prompt: string;
}

/** 分镜短片：由一个主题生成多镜头故事片 */
export interface StoryboardFilmArgs {
  /** 短片主题 */
  topic: string;
  /** 分镜数量 */
  shots: number;
  chatModel: string;
  imageModel: string;
  videoModel: string;
}

export type OfficialPipelineArgs = GenerateBrandAdArgs | StoryboardFilmArgs | Record<string, unknown>;

/** 组装完成、可直接发给后端的请求载荷 */
export type RunPayload =
  | { mode: 'pipeline'; workflow: string; args: OfficialPipelineArgs }
  | { mode: 'agent-loop'; text: string };

/* ========== 动态表单的字段声明 ========== */

export type FieldControl = 'text' | 'textarea' | 'number' | 'select' | 'array' | 'json';

/** 模型下拉选项来源（对齐 GET /agentk/models） */
export type ModelOptionsFrom = ModelOptionsKind;

export interface FieldSpec {
  key: string;
  label: string;
  /** 字段下方的一句话说明 */
  hint?: string;
  control: FieldControl;
  placeholder?: string;
  min?: number;
  max?: number;
  /** 是否必填（schema 驱动时用于校验提示） */
  required?: boolean;
  /** 折叠进「更多设置」，主表单只留关键字段 */
  advanced?: boolean;
  /** select 控件的静态选项 */
  options?: { value: string; label: string }[];
  /** select 动态选项：从模型目录按 kind 取 */
  optionsFrom?: ModelOptionsFrom;
}

/** 表单动态值；复杂类型由 array / json 控件维护，图片列表仍走参考图区 */
export type FieldValue = string | number | boolean | null | string[] | Record<string, unknown>;
export type FieldValues = Record<string, FieldValue>;

export interface FieldDefaultsContext {
  /** 模版 prompt 用样本填充后的文案 */
  filledPrompt: string;
  /** 当前选中的样本；不吃数据集的模式为 null */
  sample: SampleRow | null;
}

export interface RunBuildContext {
  /** 参考图区当前的图片 URL，按顺序使用 */
  imageUrls: string[];
  /** 「更多设置」里的场景 / 项目 / 标签，agent-loop 模式会拼进 text */
  settings: CreationSettings;
}

export type RunBuildResult = { ok: true; payload: RunPayload } | { ok: false; error: string };

export interface ExecutionSpec {
  id: ExecutionSpecId;
  mode: ExecutionMode;
  label: string;
  description: string;
  /** 是否需要从样本数据集里选一条商品 */
  usesDataset: boolean;
  /** 为 true 时参考图区可用；数量上限由模版的 refImageLimit 决定 */
  usesImages: boolean;
  /** usesImages 时图片是否必填（弹窗 / 表单展示 *） */
  imagesRequired?: boolean;
  /** image-url-list 对应的 args 字段名，缺省为 images */
  imageFieldKey?: string;
  /** 图片区标题（来自 capabilities 字段 label） */
  imageFieldLabel?: string;
  fields: FieldSpec[];
  defaults: (ctx: FieldDefaultsContext) => FieldValues;
  build: (values: FieldValues, ctx: RunBuildContext) => RunBuildResult;
  /** 生成记录列表里的一句话摘要 */
  summarize: (payload: RunPayload) => string;
}

/**
 * 分镜短片模型字段的兜底默认值（schema 未给 default 时）
 */
export const FILM_MODEL_DEFAULTS = {
  chatModel: 'deepseek-v4-pro',
  imageModel: 'z-image-turbo',
  editModel: 'gpt-image-2-edit',
  videoModel: 'kling-v3',
};

function text(values: FieldValues, key: string): string {
  const raw = values[key];
  if (typeof raw === 'string') return raw.trim();
  if (raw == null) return '';
  return String(raw).trim();
}

/**
 * agent-loop 模式：把模版填好的创作描述、更多设置与参考图拼成一段完整指令，
 * 作为 /messages 的 text 参数，交给 Agent 自主规划后续步骤。
 */
function composeInstruction(instruction: string, { settings, imageUrls }: RunBuildContext): string {
  const lines = [instruction];
  const meta = [`创作场景：${settings.scene}`, `归属项目：${settings.project}`];
  if (settings.tag.trim()) meta.push(`作品标签：${settings.tag.trim()}`);
  lines.push('', meta.join('；'));
  if (imageUrls.length) {
    lines.push('', `参考图（${imageUrls.length} 张）：`, ...imageUrls.map((url) => `- ${url}`));
  }
  return lines.join('\n');
}

/** 自主规划 Agent：不限模版，描述交给 Agent 自己拆解 */
export const AGENT_LOOP_SPEC: ExecutionSpec = {
  id: AGENT_LOOP_ID,
  mode: 'agent-loop',
  label: 'Agent-Loop',
  description: '选模版即可开始，Agent 会自己拆解步骤并调用所需工具',
  usesDataset: true,
  usesImages: true,
  fields: [
    {
      key: 'instruction',
      label: '创作描述',
      hint: '模版已按样本填好，可直接生成，也可在此微调',
      control: 'textarea',
      placeholder: '描述你想要的画面内容、风格与诉求',
    },
  ],
  defaults: ({ filledPrompt }) => ({ instruction: filledPrompt }),
  build: (values, ctx) => {
    const instruction = text(values, 'instruction');
    if (!instruction) return { ok: false, error: '请先选择模版或填写创作描述' };
    return {
      ok: true,
      payload: { mode: 'agent-loop', text: composeInstruction(instruction, ctx) },
    };
  },
  summarize: (payload) => (payload.mode === 'agent-loop' ? payload.text : ''),
};

export const PIPELINE_SPECS: Record<OfficialPipelineName, ExecutionSpec> = {
  generate_brand_ad: {
    id: 'generate_brand_ad',
    mode: 'pipeline',
    label: '品牌广告片',
    description: '选一条商品样本，用商品图与营销诉求直接生成广告视频',
    usesDataset: true,
    usesImages: true,
    imagesRequired: true,
    fields: [
      {
        key: 'title',
        label: '广告标题',
        hint: '广告标题或业务类型，如「电商」',
        control: 'text',
        placeholder: '电商',
        required: true,
      },
      {
        key: 'video_prompt',
        label: '视频描述',
        hint: '视频内容、风格与营销诉求，模版已按样本填好',
        control: 'textarea',
        placeholder: '描述镜头内容、画面风格与营销诉求',
        required: true,
      },
    ],
    defaults: ({ filledPrompt, sample }) => ({
      // 跨境类目走跨境电商，其余按普通电商，省掉用户手填
      title: sample?.category.startsWith('跨境') ? '跨境电商' : '电商',
      video_prompt: filledPrompt,
    }),
    build: (values, { imageUrls }) => {
      const title = text(values, 'title');
      const videoPrompt = text(values, 'video_prompt');
      if (!title) return { ok: false, error: '请填写广告标题 / 业务类型' };
      if (imageUrls.length === 0) {
        return { ok: false, error: '至少需要 1 张商品图片，可点「用商品图」一键带入' };
      }
      if (!videoPrompt) return { ok: false, error: '请填写视频描述' };
      return {
        ok: true,
        payload: {
          mode: 'pipeline',
          workflow: 'generate_brand_ad',
          args: { title, images: imageUrls, video_prompt: videoPrompt },
        },
      };
    },
    summarize: (payload) => {
      if (payload.mode !== 'pipeline') return '';
      const ad = payload.args as GenerateBrandAdArgs;
      return `${ad.title} · ${ad.images.length} 张商品图 · ${ad.video_prompt}`;
    },
  },

  storyboard_film: {
    id: 'storyboard_film',
    mode: 'pipeline',
    label: '分镜短片',
    description: '给一个主题，自动拆分镜并逐镜生成，拼成一支完整短片',
    usesDataset: false,
    usesImages: false,
    fields: [
      {
        key: 'topic',
        label: '短片主题',
        hint: '一句话说清主角、场景与故事走向',
        control: 'textarea',
        placeholder: '一只橘猫的太空冒险',
        required: true,
      },
      {
        key: 'shots',
        label: '分镜数量',
        hint: '镜头越多耗时越长，建议先用 3 个镜头试效果',
        control: 'number',
        min: 1,
        max: 12,
        required: true,
      },
      {
        key: 'chatModel',
        label: '文本模型',
        control: 'text',
        advanced: true,
        placeholder: '请输入文本模型',
      },
      {
        key: 'imageModel',
        label: '图像模型',
        control: 'text',
        advanced: true,
        placeholder: '请输入图像模型',
      },
      {
        key: 'videoModel',
        label: '视频模型',
        control: 'text',
        advanced: true,
        placeholder: '请输入视频模型',
      },
    ],
    defaults: ({ filledPrompt }) => ({
      topic: filledPrompt,
      shots: 3,
      ...FILM_MODEL_DEFAULTS,
    }),
    build: (values) => {
      const topic = text(values, 'topic');
      if (!topic) return { ok: false, error: '请填写短片主题' };
      const shots = Number(values.shots);
      if (!Number.isInteger(shots) || shots < 1 || shots > 12) {
        return { ok: false, error: '分镜数量需为 1 - 12 之间的整数' };
      }
      return {
        ok: true,
        payload: {
          mode: 'pipeline',
          workflow: 'storyboard_film',
          args: {
            topic,
            shots,
            chatModel: text(values, 'chatModel') || FILM_MODEL_DEFAULTS.chatModel,
            imageModel: text(values, 'imageModel') || FILM_MODEL_DEFAULTS.imageModel,
            videoModel: text(values, 'videoModel') || FILM_MODEL_DEFAULTS.videoModel,
          },
        },
      };
    },
    summarize: (payload) => {
      if (payload.mode !== 'pipeline') return '';
      const film = payload.args as StoryboardFilmArgs;
      return `${film.shots} 个分镜 · ${film.videoModel} · ${film.topic}`;
    },
  },
};

/** 后端 workflowKey 别名 → 内置增强 spec（样本 / 模型下拉等） */
const PIPELINE_ALIASES: Record<string, OfficialPipelineName> = {
  storyboard_film_example: 'storyboard_film',
  storyboard_film: 'storyboard_film',
  generate_brand_ad: 'generate_brand_ad',
};

/**
 * 无 schema、也无内置增强时的通用回退：侧栏一个 JSON 编辑器直接作为 args
 * （如 ffmpeg_tool；有 capabilities.fields 时会被 buildSchemaDrivenSpec 替换）
 */
export function buildRawArgsSpec(workflowKey: string): ExecutionSpec {
  const key = workflowKey.trim() || 'unknown_workflow';
  return {
    id: key,
    mode: 'pipeline',
    label: key,
    description: '以 JSON 填写运行参数；若 capabilities 已下发 fields 将自动切换为字段表单',
    usesDataset: false,
    usesImages: false,
    fields: [
      {
        key: '__raw_args',
        label: '运行参数（JSON）',
        hint: '将作为 pipeline-runs 的 args 原样提交',
        control: 'json',
        required: true,
        placeholder: '{\n  "key": "value"\n}',
      },
    ],
    defaults: () => ({ __raw_args: '{\n  \n}' }),
    build: (values) => {
      const raw = values.__raw_args;
      const text = typeof raw === 'string' ? raw.trim() : '';
      if (!text) return { ok: false, error: '请填写运行参数 JSON' };
      try {
        const parsed = JSON.parse(text) as unknown;
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
          return { ok: false, error: '运行参数须为 JSON 对象' };
        }
        return {
          ok: true,
          payload: {
            mode: 'pipeline',
            workflow: key,
            args: parsed as Record<string, unknown>,
          },
        };
      } catch {
        return { ok: false, error: '运行参数不是合法 JSON' };
      }
    },
    summarize: (payload) => {
      if (payload.mode !== 'pipeline') return '';
      try {
        const text = JSON.stringify(payload.args);
        return text.length > 200 ? `${text.slice(0, 200)}…` : text;
      } catch {
        return key;
      }
    },
  };
}

/**
 * 按 URL 参数定位执行方式（无 schema 时的回退）：
 * 自主规划走 agent-loop；已知 workflowKey 走内置增强；其余走通用 JSON args。
 * workflowKey 兼容 generate_brand_ad / agentk.xxx / pipeline:agentk.xxx
 */
export function resolveExecutionSpec(
  catalogType: 'planning' | 'workflow',
  workflowKey?: string,
): ExecutionSpec {
  if (catalogType === 'planning') return AGENT_LOOP_SPEC;

  const key = workflowKey?.trim();
  if (key) {
    const bare = bareWorkflowKey(key);
    const alias = PIPELINE_ALIASES[key] ?? PIPELINE_ALIASES[bare];
    if (alias) {
      const spec = PIPELINE_SPECS[alias];
      // 请求仍用 URL 上的真实 workflowKey（如 storyboard_film_example / pipeline:…）
      if (key !== alias) {
        return {
          ...spec,
          id: key,
          build: (values, ctx) => {
            const result = spec.build(values, ctx);
            if (!result.ok || result.payload.mode !== 'pipeline') return result;
            return {
              ok: true,
              payload: { ...result.payload, workflow: key },
            };
          },
        };
      }
      return spec;
    }
    return buildRawArgsSpec(key);
  }

  return PIPELINE_SPECS.generate_brand_ad;
}

/** 把载荷渲染成可读文本，用于「查看模版处理详情」与右栏入参页 */
export function previewPayload(payload: RunPayload): string {
  return payload.mode === 'pipeline'
    ? JSON.stringify({ workflow: payload.workflow, args: payload.args }, null, 2)
    : payload.text;
}
