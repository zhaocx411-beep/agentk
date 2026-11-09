/**
 * PipelineEditor - 类型定义
 * 基于 @xyflow/react 的 AI Agent Pipeline 编辑器
 */
import type { Node, Edge } from '@xyflow/react';

/** Agent 节点类型枚举 */
export type AgentNodeType = 'llm' | 'tool' | 'condition' | 'start' | 'end' | 'subflow';

/** Agent 节点数据 */
export interface AgentNodeData {
  label: string;
  type: AgentNodeType;
  description?: string;
  /** LLM 节点配置 */
  model?: string;
  prompt?: string;
  temperature?: number;
  /** Tool 节点配置：该节点可调用的工具列表（多选，对齐 playground 新会话设置） */
  toolNames?: string[];
  toolConfig?: Record<string, unknown>;
  /** Condition 节点配置 */
  conditionExpr?: string;
  /** 子流程配置 */
  subflowId?: string;
  /** 输出变量名（Pipeline 内部数据流引用，前端编排能力，不传后端） */
  outputVar?: string;
  /** 节点状态 */
  status?: 'idle' | 'running' | 'success' | 'error';
  [key: string]: unknown;
}

/** 自定义 Agent 节点（扩展 xyflow Node） */
export type AgentNode = Node<AgentNodeData, 'agent'>;

/** 边的条件标签 */
export interface AgentEdgeData {
  label?: string;
  condition?: string;
  animated?: boolean;
  [key: string]: unknown;
}

/** 自定义 Agent 边（扩展 xyflow Edge） */
export type AgentEdge = Edge<AgentEdgeData>;

/** Pipeline 完整数据 */
export interface WorkflowData {
  id: string;
  name: string;
  description?: string;
  nodes: AgentNode[];
  edges: AgentEdge[];
  /** 创建时间 */
  createdAt?: string;
  /** 更新时间 */
  updatedAt?: string;
}

/** 可用 Agent 模板（用于从侧栏拖入画布） */
export interface AgentTemplate {
  type: AgentNodeType;
  label: string;
  description: string;
  icon: string;
  defaultData: Partial<AgentNodeData>;
}

/** 快捷 Pipeline 模板（一键加载到画布） */
export interface WorkflowTemplate {
  /** 模板唯一标识 */
  id: string;
  /** 展示名称 */
  name: string;
  /** 描述 */
  description: string;
  /** 图标（emoji 或文字） */
  icon: string;
  /** 主题色（用于左侧色条与高亮） */
  color: string;
  /** 节点列表 */
  nodes: AgentNode[];
  /** 边列表 */
  edges: AgentEdge[];
}
