/**
 * WorkflowEditor - 编辑器静态配置
 * 节点类型的展示配置与侧栏可拖拽节点定义
 */
import type { AgentNodeType, AgentTemplate } from './types';
import type { NodeRunStatus } from './store';

/** 节点类型 → 展示名称 / 主题色 / 图标 */
export const AGENT_NODE_TYPE_CONFIG: Record<
  AgentNodeType,
  { label: string; color: string; icon: string }
> = {
  start: { label: '开始', color: '#52c41a', icon: '▶' },
  end: { label: '结束', color: '#ff4d4f', icon: '■' },
  llm: { label: 'LLM', color: '#1677ff', icon: '🤖' },
  tool: { label: '工具', color: '#fa8c16', icon: '🔧' },
  condition: { label: '条件', color: '#722ed1', icon: '◇' },
  subflow: { label: '子流程', color: '#13c2c2', icon: '⊞' },
};

/** 画布节点运行状态 → Tag 颜色 */
export const RUN_STATUS_COLOR: Record<NodeRunStatus, string> = {
  idle: 'default',
  running: 'processing',
  success: 'success',
  error: 'error',
};

/** 画布节点运行状态 → 展示文案 */
export const RUN_STATUS_LABEL: Record<NodeRunStatus, string> = {
  idle: '空闲',
  running: '运行中',
  success: '成功',
  error: '失败',
};

/** 侧栏节点分组：流程控制 vs 执行节点 */
export const NODE_GROUPS: Array<{ title: string; hint: string; types: AgentNodeType[] }> = [
  {
    title: '流程控制',
    hint: 'Pipeline 起止与分支',
    types: ['start', 'condition', 'subflow', 'end'],
  },
  { title: '执行节点', hint: '模型调用与工具调用', types: ['llm', 'tool'] },
];

/** 侧栏可拖入画布的节点模板 */
export const AGENT_TEMPLATES: AgentTemplate[] = [
  {
    type: 'start',
    label: '开始节点',
    description: 'Pipeline 入口',
    icon: '▶',
    defaultData: { type: 'start', label: '开始' },
  },
  {
    type: 'end',
    label: '结束节点',
    description: 'Pipeline 出口',
    icon: '■',
    defaultData: { type: 'end', label: '结束' },
  },
  {
    type: 'llm',
    label: 'LLM 调用',
    description: '调用大语言模型',
    icon: '🤖',
    defaultData: {
      type: 'llm',
      label: 'LLM',
      model: 'deepseek-v4-pro',
      prompt: '',
      temperature: 0.7,
      outputVar: 'llm_output',
    },
  },
  {
    type: 'tool',
    label: '工具节点',
    description: '调用外部工具/API',
    icon: '🔧',
    defaultData: { type: 'tool', label: '工具', toolNames: [], outputVar: 'tool_output' },
  },
  {
    type: 'condition',
    label: '条件判断',
    description: '根据条件分支',
    icon: '◇',
    defaultData: { type: 'condition', label: '条件', conditionExpr: '', outputVar: 'branch' },
  },
  {
    type: 'subflow',
    label: '子流程',
    description: '嵌套子 Pipeline',
    icon: '⊞',
    defaultData: { type: 'subflow', label: '子流程', subflowId: '', outputVar: 'subflow_output' },
  },
];
