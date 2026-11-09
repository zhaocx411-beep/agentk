/**
 * 由 SSE 运行状态推导执行链路节点
 * 未开跑时用模版声明的预期步骤占位，跑起来后以真实工具调用为准
 */
import { HIDDEN_TOOL_CARDS } from '@/pages/AgentPlayground/constants';
import { collectMediaUrls } from '@/pages/AgentPlayground/hooks/sse/helpers';
import type { SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import { getToolMeta } from '../../../AgentDetail/lib/toolMeta';
import type { WorkflowRunDetail, WorkflowRunInfo } from '@/pages/AgentPlayground/types';
import { classifyArtifactTier, sortArtifactsByTier } from './artifactTier';
import type { ArtifactItem, CreationTemplate, PipelineNode, PipelineStatus } from './types';

/** 节点序号：A1、A2… */
function indexLabel(i: number): string {
  return `A${i + 1}`;
}

const PIPELINE_STATUS_DETAIL: Record<string, string> = {
  running: '执行中',
  pending: '等待运行',
};

/** 把工具卡片状态映射成链路状态 */
function toolStatus(status: string | undefined, isRunning: boolean): PipelineStatus {
  if (status === 'ok') return 'done';
  if (status === 'err') return 'failed';
  return isRunning ? 'running' : 'pending';
}

/** 结果里能直接读到的一句话摘要，用于节点副标题 */
function summarize(value: unknown): string | undefined {
  if (value == null) return undefined;
  const text = typeof value === 'string' ? value : JSON.stringify(value);
  const flat = text.replace(/\s+/g, ' ').trim();
  if (!flat) return undefined;
  return flat.length > 90 ? `${flat.slice(0, 90)}…` : flat;
}

export function buildPipelineNodes(
  state: SSEState,
  template: CreationTemplate | null,
  composedPrompt: string,
): PipelineNode[] {
  const tools = state.toolCards.filter((card) => !HIDDEN_TOOL_CARDS.has(card.toolName));
  const hasRun = state.messages.length > 0 || state.isRunning;

  if (!hasRun) {
    return (template?.stages ?? []).map((stage, i) => ({
      key: stage.key,
      index: indexLabel(i),
      title: stage.title,
      badge: stage.badge,
      status: 'pending' as const,
      detail: '等待运行',
    }));
  }

  // 首节点固定为意图还原：它对应「模版 + 样本 + 用户输入」合成最终 prompt 的过程
  const nodes: PipelineNode[] = [
    {
      key: 'intent',
      index: indexLabel(0),
      title: '意图还原',
      badge: 'LLM',
      status: state.error && tools.length === 0 ? 'failed' : 'done',
      detail: summarize(composedPrompt),
      output: composedPrompt,
    },
  ];

  tools.forEach((card, i) => {
    const meta = getToolMeta(card.toolName);
    nodes.push({
      key: card.toolCallId,
      index: indexLabel(i + 1),
      title: meta.title,
      badge: meta.category,
      status: toolStatus(card.status, state.isRunning),
      detail: card.status === 'err' ? summarize(card.output) : summarize(card.input),
      durationMs: card.duration,
      input: card.input,
      output: card.output,
    });
  });

  // 真实链路还没铺满时，用模版剩余步骤补占位，让用户看得到「还剩几步」
  const planned = template?.stages ?? [];
  for (let i = nodes.length; i < planned.length; i += 1) {
    nodes.push({
      key: `planned-${planned[i].key}`,
      index: indexLabel(i),
      title: planned[i].title,
      badge: planned[i].badge,
      status: 'pending',
      detail: '等待中',
    });
  }

  return nodes;
}

/** 后端 pipeline-runs 状态 → 链路节点状态 */
export function mapWorkflowRunStatus(status: string | undefined): PipelineStatus {
  const value = (status ?? '').toLowerCase();
  if (value === 'completed' || value === 'done' || value === 'ok' || value === 'success') {
    return 'done';
  }
  if (value === 'failed' || value === 'error' || value === 'err') return 'failed';
  if (value === 'running') return 'running';
  return 'pending';
}

type PipelineRunArgsSource = {
  args?: Record<string, unknown>;
  inputs?: Record<string, unknown>;
};

/** pipeline-runs 入参：官方接口用 args，部分环境用 inputs */
export function pipelineRunArgs(run: PipelineRunArgsSource | null | undefined): Record<string, unknown> {
  if (!run) return {};
  const fromArgs = run.args;
  if (fromArgs && typeof fromArgs === 'object' && Object.keys(fromArgs).length > 0) {
    return fromArgs;
  }
  return run.inputs ?? {};
}

/** 详情 / 列表项统一写入 inputs，便于过程面板与表单回填 */
export function normalizePipelineRunDetail(detail: WorkflowRunDetail): WorkflowRunDetail {
  const args = pipelineRunArgs(detail);
  return {
    ...detail,
    inputs: args,
    args: detail.args ?? (Object.keys(args).length > 0 ? args : undefined),
  };
}

/** 列表 / 启动响应 → 详情面板可用的最小结构 */
export function toRunDetail(
  run: Pick<
    WorkflowRunInfo,
    'id' | 'name' | 'status' | 'args' | 'inputs' | 'output' | 'createdAt' | 'finishedAt'
  > & {
    error?: string;
    profileVersion?: string;
  },
): WorkflowRunDetail {
  const args = pipelineRunArgs(run);
  return {
    id: run.id,
    name: run.name,
    status: run.status,
    inputs: args,
    args: run.args,
    output: run.output,
    error: run.error,
    profileVersion: run.profileVersion ?? '',
    createdAt: run.createdAt ?? '',
    finishedAt: run.finishedAt,
  };
}

/** 优先 running，否则按创建时间取最近一次 */
export function pickLatestPipelineRun<T extends { status: string; createdAt?: string }>(
  runs: T[],
): T | undefined {
  const running = runs.find((run) => run.status === 'running');
  if (running) return running;
  return [...runs].sort((left, right) => {
    const leftAt = left.createdAt ? new Date(left.createdAt).getTime() : 0;
    const rightAt = right.createdAt ? new Date(right.createdAt).getTime() : 0;
    return rightAt - leftAt;
  })[0];
}

/** 用单次执行详情铺链路；没有 nodes 时用整次 run 作为一步 */
export function buildNodesFromRunDetail(
  detail: WorkflowRunDetail | null | undefined,
  template: CreationTemplate | null,
): PipelineNode[] {
  const nodes = detail?.nodes;
  if (nodes && nodes.length > 0) {
    return nodes.map((node, i) => ({
      key: node.nodeId || `node-${i}`,
      index: indexLabel(i),
      title: node.nodeId || `节点 ${i + 1}`,
      badge: node.attempts > 1 ? `重试 ${node.attempts}` : 'STEP',
      status: mapWorkflowRunStatus(node.status),
      detail: summarize(node.output),
      output: node.output,
    }));
  }
  if (detail) {
    const status = mapWorkflowRunStatus(detail.status);
    return [
      {
        key: detail.id,
        index: indexLabel(0),
        title: detail.name || 'Pipeline',
        badge: 'RUN',
        status,
        detail:
          detail.error || summarize(detail.output) || PIPELINE_STATUS_DETAIL[status],
        input: detail.inputs,
        output: detail.output,
      },
    ];
  }
  return (template?.stages ?? []).map((stage, i) => ({
    key: stage.key,
    index: indexLabel(i),
    title: stage.title,
    badge: stage.badge,
    status: 'pending' as const,
    detail: '等待运行',
  }));
}

/** 已完成节点数 / 总节点数 */
export function pipelineProgress(nodes: PipelineNode[]): { done: number; total: number } {
  return { done: nodes.filter((n) => n.status === 'done').length, total: nodes.length };
}

/** 一次 Pipeline 执行的产出快照 */
export interface RunOutput {
  runId: string;
  /** Pipeline 名称，作为产物来源标签 */
  name: string;
  output: unknown;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

function readArtifactUri(raw: Record<string, unknown>): string {
  if (typeof raw.uri === 'string' && raw.uri.trim()) return raw.uri.trim();
  if (typeof raw.url === 'string' && raw.url.trim()) return raw.url.trim();
  return '';
}

interface ParsedArtifactRecord {
  url: string;
  kind: ArtifactItem['kind'];
  from: string;
  sourceTool?: string;
  sourceStage?: string;
}

/** 从 tool.exec_succeeded 的 artifacts / result.artifacts 读取结构化产物（含 stage） */
function extractArtifactRecords(
  source: unknown,
  from: string,
  fallbackTool?: string,
): ParsedArtifactRecord[] {
  const rows: Record<string, unknown>[] = [];
  if (isRecord(source)) {
    if (Array.isArray(source.artifacts)) {
      rows.push(...source.artifacts.filter(isRecord));
    }
    if (isRecord(source.result) && Array.isArray(source.result.artifacts)) {
      rows.push(...source.result.artifacts.filter(isRecord));
    }
  }

  const records: ParsedArtifactRecord[] = [];
  const seen = new Set<string>();
  for (const raw of rows) {
    const url = readArtifactUri(raw);
    const type = typeof raw.type === 'string' ? raw.type : '';
    if (!url || (type !== 'image' && type !== 'video' && type !== 'audio')) continue;
    if (seen.has(url)) continue;
    seen.add(url);
    records.push({
      url,
      kind: type,
      from,
      sourceTool: typeof raw.tool === 'string' ? raw.tool : fallbackTool,
      sourceStage: typeof raw.stage === 'string' ? raw.stage : undefined,
    });
  }
  return records;
}

/** 汇总产出的视频、图片与音频，按出现顺序去重；成片视频优先排在前面 */
export function collectArtifacts(
  state: SSEState,
  runOutputs: RunOutput[] = [],
  options?: { enableFinalTier?: boolean },
): ArtifactItem[] {
  const enableFinalTier = options?.enableFinalTier ?? false;
  const seen = new Set<string>();
  const items: ArtifactItem[] = [];

  const push = (source: unknown, from: string, toolName?: string) => {
    const normalizedTool = toolName?.trim() || undefined;
    for (const record of extractArtifactRecords(source, from, normalizedTool)) {
      if (seen.has(record.url)) continue;
      seen.add(record.url);
      items.push({
        url: record.url,
        kind: record.kind,
        from: record.from,
        tier: enableFinalTier
          ? classifyArtifactTier(record.url, {
              toolName: record.sourceTool ?? normalizedTool,
              sourceStage: record.sourceStage,
            })
          : undefined,
        sourceTool: record.sourceTool ?? normalizedTool,
        sourceStage: record.sourceStage,
      });
    }

    const { videos, images, audios } = collectMediaUrls(source);
    for (const url of videos) {
      if (seen.has(url)) continue;
      seen.add(url);
      items.push({
        url,
        kind: 'video',
        from,
        tier: enableFinalTier
          ? classifyArtifactTier(url, { toolName: normalizedTool, sourceStage: undefined })
          : undefined,
        sourceTool: normalizedTool,
      });
    }
    for (const url of images) {
      if (seen.has(url)) continue;
      seen.add(url);
      items.push({
        url,
        kind: 'image',
        from,
        tier: enableFinalTier
          ? classifyArtifactTier(url, { toolName: normalizedTool, sourceStage: undefined })
          : undefined,
        sourceTool: normalizedTool,
      });
    }
    for (const url of audios) {
      if (seen.has(url)) continue;
      seen.add(url);
      items.push({
        url,
        kind: 'audio',
        from,
        tier: undefined,
        sourceTool: normalizedTool,
      });
    }
  };

  for (const run of runOutputs) push(run.output, run.name);
  for (const card of state.toolCards) {
    push(card.output, getToolMeta(card.toolName).title, card.toolName);
  }
  for (const job of state.jobs) push(job.result, job.kind);
  // Agent 常把成片链接写在回复正文里，工具/任务结果里反而没有可播 URL
  for (const msg of state.messages) {
    if (msg.role !== 'assistant') continue;
    for (const part of msg.parts) {
      if (part.type === 'text') push(part.text, '成片回复');
      if (part.type === 'tool-result') push(part.output, '工具结果');
    }
  }
  if (state.streamingText) push(state.streamingText, '成片回复');

  return sortArtifactsByTier(items);
}

/** 仅用于输入分析、不应出现在「当前产物」的工具 */
const INPUT_ANALYSIS_TOOLS = new Set(['understand_media']);

/** 从产物列表排除用户上传与输入分析类结果 */
export function filterSessionOutputArtifacts(
  items: ArtifactItem[],
  inputUrls: Set<string>,
): ArtifactItem[] {
  if (inputUrls.size === 0) {
    return items.filter(
      (item) => !item.sourceTool || !INPUT_ANALYSIS_TOOLS.has(item.sourceTool),
    );
  }
  return items.filter((item) => {
    if (inputUrls.has(item.url)) return false;
    if (item.sourceTool && INPUT_ANALYSIS_TOOLS.has(item.sourceTool)) return false;
    return true;
  });
}
