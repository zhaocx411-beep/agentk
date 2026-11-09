/**
 * 一键成片 v2 入参（additionalProperties: false，多传会 412）
 *
 * documentary_video: videoUri, instructions, models, analysisOptions, tts, aspect, volumes, subtitles
 * documentary_script: videoUri, instructions, models, analysisOptions（无 theme / tts）
 * documentary_render: script, tts, aspect, volumes, subtitles（无 models / voiceId）
 */
import { defaultTtsValue, ttsProviderOf } from './ttsConfig';
import type { Agent100Config, Agent100Pipeline, JsonSchemaNode } from './types';
import { isPlainObject } from './utils';

const VIDEO_KEYS = [
  'videoUri',
  'instructions',
  'models',
  'analysisOptions',
  'tts',
  'aspect',
  'narrationVolume',
  'originalAudioVolume',
  'subtitles',
] as const;

const SCRIPT_KEYS = ['videoUri', 'instructions', 'models', 'analysisOptions'] as const;
const RENDER_KEYS = [
  'script',
  'tts',
  'aspect',
  'narrationVolume',
  'originalAudioVolume',
  'subtitles',
] as const;
const MODEL_KEYS = ['vision', 'asr', 'search', 'story', 'script'] as const;
const STRIP_SCRIPT_KEYS = ['theme', 'segments', 'ost'] as const;

const ARGS_ALLOWLIST: Record<string, readonly string[]> = {
  documentary_video: VIDEO_KEYS,
  documentary_script: SCRIPT_KEYS,
  documentary_render: RENDER_KEYS,
};

export function isDocumentaryPipeline(name: string): boolean {
  return name in ARGS_ALLOWLIST;
}

function cloneSchema(schema: JsonSchemaNode): JsonSchemaNode {
  return JSON.parse(JSON.stringify(schema)) as JsonSchemaNode;
}

function asTrimmedString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

function pickKeys(source: Record<string, unknown>, keys: readonly string[]): Record<string, unknown> {
  const next: Record<string, unknown> = {};
  for (const key of keys) {
    if (source[key] !== undefined) next[key] = source[key];
  }
  return next;
}

function sanitizeModels(models: unknown): Record<string, unknown> | undefined {
  if (!isPlainObject(models)) return undefined;
  const next = pickKeys(models, MODEL_KEYS);
  return Object.keys(next).length > 0 ? next : undefined;
}

function sanitizeAnalysisOptions(raw: unknown): Record<string, unknown> | undefined {
  if (!isPlainObject(raw)) return undefined;
  return {
    ...raw,
    asrLanguage: typeof raw.asrLanguage === 'string' ? raw.asrLanguage : '',
  };
}

function buildTtsObject(
  args: Record<string, unknown>,
  config?: Agent100Config,
): Record<string, string> | undefined {
  const existing = isPlainObject(args.tts) ? args.tts : {};
  const fallback = defaultTtsValue(config?.tts);
  const provider = asTrimmedString(existing.provider) || fallback.provider;
  const row = ttsProviderOf(config?.tts, provider);
  const model =
    asTrimmedString(existing.model) || asTrimmedString(row?.defaultModel) || fallback.model;
  const voiceId =
    asTrimmedString(existing.voiceId) ||
    asTrimmedString(args.voiceId) ||
    asTrimmedString(row?.defaultVoiceId) ||
    fallback.voiceId;
  const tts: Record<string, string> = {};
  if (provider) tts.provider = provider;
  if (model) tts.model = model;
  if (voiceId) tts.voiceId = voiceId;
  return Object.keys(tts).length > 0 ? tts : undefined;
}

export function sanitizeDocumentaryScript(script: unknown): unknown {
  if (typeof script === 'string' && script.trim()) {
    try {
      return sanitizeDocumentaryScript(JSON.parse(script) as unknown);
    } catch {
      return script;
    }
  }
  if (!isPlainObject(script)) return script;
  const next: Record<string, unknown> = { ...script };
  for (const key of STRIP_SCRIPT_KEYS) {
    delete next[key];
  }
  if (next.version === '1' || next.version === 1 || next.version == null) {
    next.version = '2';
  }
  return next;
}

/** 提交 / 回填：只保留该 pipeline 的 v2 字段 */
export function toDocumentaryV2Args(
  pipelineName: string,
  args: Record<string, unknown>,
  config?: Agent100Config,
): Record<string, unknown> {
  const allow = ARGS_ALLOWLIST[pipelineName];
  if (!allow) return args;

  const next = pickKeys(args, allow);
  const models = sanitizeModels(args.models);
  if (pipelineName === 'documentary_render') {
    delete next.models;
  } else if (models) {
    next.models = models;
  } else {
    delete next.models;
  }

  if (pipelineName === 'documentary_script') {
    delete next.tts;
  } else {
    const tts = buildTtsObject(args, config);
    if (tts) next.tts = tts;
    else delete next.tts;
  }

  if (next.script !== undefined) {
    next.script = sanitizeDocumentaryScript(next.script);
  }

  if (allow.includes('analysisOptions')) {
    const analysis = sanitizeAnalysisOptions(args.analysisOptions ?? next.analysisOptions);
    if (analysis) next.analysisOptions = analysis;
  }

  return next;
}

function withoutRequired(schema: JsonSchemaNode, names: string[]): void {
  if (!schema.required?.length) return;
  schema.required = schema.required.filter((item) => !names.includes(item));
}

function keepSchemaProps(schema: JsonSchemaNode, keys: readonly string[]): void {
  const props = schema.properties ?? {};
  const next: Record<string, JsonSchemaNode> = {};
  for (const key of keys) {
    if (props[key]) next[key] = props[key];
  }
  schema.properties = next;
  if (schema.required?.length) {
    schema.required = schema.required.filter((item) => item in next);
  }
}

function ensureTtsSchema(props: Record<string, JsonSchemaNode>, config: Agent100Config): void {
  const existing = props.tts;
  if (existing?.type === 'object' && existing.properties?.provider && existing.properties?.voiceId) {
    return;
  }
  const defaults = defaultTtsValue(config.tts);
  const providers = Object.keys(config.tts?.providers ?? config.voices ?? {});
  props.tts = {
    type: 'object',
    required: ['provider', 'model', 'voiceId'],
    properties: {
      provider: {
        type: 'string',
        ...(providers.length > 0 ? { enum: providers } : {}),
        default: defaults.provider || providers[0] || 'qwen',
      },
      model: { type: 'string', default: defaults.model || undefined },
      voiceId: { type: 'string', default: defaults.voiceId || undefined },
    },
  };
}

function ensureSearchModel(props: Record<string, JsonSchemaNode>, config: Agent100Config): void {
  if (!props.models?.properties || props.models.properties.search) return;
  const searchDefault = config.models.defaults.search;
  props.models.properties.search = {
    type: 'string',
    ...(searchDefault ? { default: searchDefault } : {}),
  };
}

/** 表单只展示该 pipeline 的 v2 字段 */
export function adaptDocumentaryPipeline(
  pipeline: Agent100Pipeline,
  config: Agent100Config,
): Agent100Pipeline {
  const allow = ARGS_ALLOWLIST[pipeline.name];
  if (!allow) return pipeline;

  const inputSchema = cloneSchema(pipeline.inputSchema);
  const props = inputSchema.properties ?? {};

  delete props.theme;
  delete props.voiceId;
  withoutRequired(inputSchema, ['theme', 'voiceId']);

  if (props.models?.properties) {
    delete props.models.properties.tts;
    withoutRequired(props.models, ['tts']);
    if (pipeline.name !== 'documentary_render') ensureSearchModel(props, config);
  }

  if (pipeline.name === 'documentary_script') {
    delete props.tts;
    withoutRequired(inputSchema, ['tts']);
  } else {
    ensureTtsSchema(props, config);
  }

  inputSchema.properties = props;
  keepSchemaProps(inputSchema, allow);
  return { ...pipeline, inputSchema };
}
