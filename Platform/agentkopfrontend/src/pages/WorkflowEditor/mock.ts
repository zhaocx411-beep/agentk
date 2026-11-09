/**
 * WorkflowEditor - 前端 mock 数据
 * 画布初始示例节点与内置示范 Pipeline 模板，均为前端预置内容，不来自接口
 */
import type { AgentEdge, AgentNode, WorkflowTemplate } from './types';

/** 画布默认名称 */
export const DEFAULT_WORKFLOW_NAME = '示例 Pipeline';

/** 初始示例节点：默认只显示开始和结束节点，其余由用户自行添加 */
export const INITIAL_NODES: AgentNode[] = [
  {
    id: 'node-1',
    type: 'agent',
    position: { x: -500, y: 150 },
    data: {
      label: '开始',
      type: 'start',
      description: '用户输入触发',
      status: 'idle',
    },
  },
  {
    id: 'node-5',
    type: 'agent',
    position: { x: 300, y: 150 },
    data: {
      label: '结束',
      type: 'end',
      status: 'idle',
    },
  },
];

/** 初始示例连线 */
export const INITIAL_EDGES: AgentEdge[] = [
  { id: 'e-1-5', source: 'node-1', target: 'node-5', data: { animated: true } },
];

/** 视频创作流程的 System Prompt（对齐 agent-k 最简版分镜→镜头→拼接） */
export const VIDEO_CREATION_SYSTEM_PROMPT =
  '你是 agent-k 助手。用户的诉求需要工具时，调用提供的工具完成。\n\n' +
  '视频创作流程：先用 storyboard 生成分镜；再对每个镜头分别调用 generate_clip 提交生成任务' +
  '（可以在同一轮全部提交）。片段任务在后台执行，就绪后系统会自动唤醒你。被唤醒时核对会话里' +
  '已就绪的片段：若还有镜头未完成，简短说明进度即可，不要重复提交；全部就绪后调用 compose_video ' +
  '按镜头顺序合成，并把成片链接告诉用户。';

/** 视频创作 Pipeline 模板：Start → LLM(编排) → [storyboard / generate_clip / compose_video] → End */
export const VIDEO_CREATION_TEMPLATE: WorkflowTemplate = {
  id: 'video-creation',
  name: '视频创作',
  description: '分镜稿 → 镜头生成 → 拼接合成',
  icon: '🎬',
  color: '#7c3aed',
  nodes: [
    {
      id: 'vc-start',
      type: 'agent',
      position: { x: 400, y: 0 },
      data: { label: '开始', type: 'start', description: '用户输入视频主题', status: 'idle' },
    },
    {
      id: 'vc-llm',
      type: 'agent',
      position: { x: 400, y: 160 },
      data: {
        label: '视频创作 Agent',
        type: 'llm',
        model: 'deepseek-v4-pro',
        prompt: VIDEO_CREATION_SYSTEM_PROMPT,
        temperature: 0.7,
        description: '编排 storyboard → generate_clip → compose_video',
        status: 'idle',
      },
    },
    {
      id: 'vc-tool-storyboard',
      type: 'agent',
      position: { x: 80, y: 360 },
      data: {
        label: 'storyboard',
        type: 'tool',
        toolNames: ['storyboard'],
        description: '① 生成分镜稿',
        status: 'idle',
      },
    },
    {
      id: 'vc-tool-clip',
      type: 'agent',
      position: { x: 400, y: 360 },
      data: {
        label: 'generate_clip',
        type: 'tool',
        toolNames: ['generate_clip'],
        description: '② 逐镜头生成（后台异步）',
        status: 'idle',
      },
    },
    {
      id: 'vc-tool-compose',
      type: 'agent',
      position: { x: 720, y: 360 },
      data: {
        label: 'compose_video',
        type: 'tool',
        toolNames: ['compose_video'],
        description: '③ 按镜头顺序拼接',
        status: 'idle',
      },
    },
    {
      id: 'vc-end',
      type: 'agent',
      position: { x: 400, y: 560 },
      data: { label: '结束', type: 'end', description: '返回成片链接', status: 'idle' },
    },
  ],
  edges: [
    { id: 'vc-e-start-llm', source: 'vc-start', target: 'vc-llm', data: { animated: true } },
    {
      id: 'vc-e-llm-storyboard',
      source: 'vc-llm',
      target: 'vc-tool-storyboard',
      data: { label: '① 分镜', animated: true },
    },
    {
      id: 'vc-e-llm-clip',
      source: 'vc-llm',
      target: 'vc-tool-clip',
      data: { label: '② 镜头', animated: true },
    },
    {
      id: 'vc-e-llm-compose',
      source: 'vc-llm',
      target: 'vc-tool-compose',
      data: { label: '③ 拼接', animated: true },
    },
    { id: 'vc-e-storyboard-end', source: 'vc-tool-storyboard', target: 'vc-end', data: {} },
    { id: 'vc-e-clip-end', source: 'vc-tool-clip', target: 'vc-end', data: {} },
    { id: 'vc-e-compose-end', source: 'vc-tool-compose', target: 'vc-end', data: {} },
  ],
};

/** 所有快捷模板列表（NodeSidebar 渲染用） */
export const WORKFLOW_TEMPLATES: WorkflowTemplate[] = [VIDEO_CREATION_TEMPLATE];
