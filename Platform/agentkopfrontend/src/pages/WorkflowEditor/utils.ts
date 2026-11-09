/**
 * WorkflowEditor - 画布与后端运行态之间的转换工具
 * 均为纯函数，不读写 store（同步 store 的逻辑留在组件内）
 */
import type { WorkflowDefinition, WorkflowNodeStatus } from '@/pages/AgentPlayground/types';
import type { NodeRunStatus } from './store';
import { WORKFLOW_TEMPLATES } from './mock';
import type { AgentEdge, AgentNode, AgentNodeType } from './types';

/** 自动生成画布时的布局参数 */
const AUTO_LAYOUT = { startY: 60, gapY: 140, centerX: 350 };

/** 后端 WorkflowNodeStatus → 画布节点 NodeRunStatus */
export function toNodeRunStatus(status: WorkflowNodeStatus): NodeRunStatus {
  switch (status) {
    case 'running':
      return 'running';
    case 'completed':
      return 'success';
    case 'failed':
      return 'error';
    default:
      return 'idle';
  }
}

/**
 * 根据后端 nodeId 找到画布节点 id
 * 匹配策略：1) 精确匹配 id  2) 匹配 wf-xxx-{nodeId} 格式  3) 按 data.label 匹配
 */
export function findCanvasNodeId(nodeId: string, canvasNodes: AgentNode[]): string | null {
  const exact = canvasNodes.find((n) => n.id === nodeId);
  if (exact) return exact.id;
  // 从 Pipeline 同步到画布的节点，id 形如 wf-{workflowId}-{nodeId}
  const byWfId = canvasNodes.find((n) => n.id.endsWith(`-${nodeId}`));
  if (byWfId) return byWfId.id;
  const byLabel = canvasNodes.find((n) => n.data.label === nodeId);
  if (byLabel) return byLabel.id;
  return null;
}

/** 根据节点名推断画布节点类型 */
export function inferNodeType(name: string): AgentNodeType {
  const lower = name.toLowerCase();
  if (lower.includes('llm') || lower.includes('agent') || lower.includes('model')) return 'llm';
  if (lower.includes('cond') || lower.includes('branch') || lower.includes('if'))
    return 'condition';
  if (lower.includes('sub') || lower.includes('child')) return 'subflow';
  // 默认当作 Tool 节点（Pipeline 中的步骤通常对应工具调用）
  return 'tool';
}

/**
 * 选择 Pipeline 后同步画布
 * 优先从 WORKFLOW_TEMPLATES 匹配模板（复用已有的完整节点布局），
 * 无匹配模板时降级为按节点列表自动生成简单链路
 */
export function workflowToCanvas(wf: WorkflowDefinition): {
  nodes: AgentNode[];
  edges: AgentEdge[];
} {
  const matchedTemplate = WORKFLOW_TEMPLATES.find((t) => t.id === wf.name || t.id === wf.id);
  if (matchedTemplate) {
    // 深拷贝模板，避免污染模板数据，并把节点状态重置为 idle
    const nodes = JSON.parse(JSON.stringify(matchedTemplate.nodes)) as AgentNode[];
    const edges = JSON.parse(JSON.stringify(matchedTemplate.edges)) as AgentEdge[];
    for (const n of nodes) {
      if (n.data) n.data.status = 'idle';
    }
    return { nodes, edges };
  }

  const { startY, gapY, centerX } = AUTO_LAYOUT;

  if (wf.nodes.length === 0) {
    // 节点列表为空时，只放开始 + 结束
    return {
      nodes: [
        {
          id: `wf-${wf.id}-start`,
          type: 'agent' as const,
          position: { x: centerX, y: startY },
          data: {
            label: '开始',
            type: 'start' as AgentNodeType,
            description: wf.description,
            status: 'idle' as const,
          },
        },
        {
          id: `wf-${wf.id}-end`,
          type: 'agent' as const,
          position: { x: centerX, y: 200 },
          data: { label: '结束', type: 'end' as AgentNodeType, status: 'idle' as const },
        },
      ],
      edges: [
        {
          id: `wf-e-${wf.id}-start-end`,
          source: `wf-${wf.id}-start`,
          target: `wf-${wf.id}-end`,
          data: { animated: true },
        },
      ],
    };
  }

  const nodes: AgentNode[] = [];
  const edges: AgentEdge[] = [];

  const startId = `wf-${wf.id}-start`;
  nodes.push({
    id: startId,
    type: 'agent',
    position: { x: centerX, y: startY },
    data: { label: '开始', type: 'start', description: wf.description, status: 'idle' },
  });

  wf.nodes.forEach((n, i) => {
    const nodeType = inferNodeType(n.name);
    nodes.push({
      id: `wf-${wf.id}-${n.name}`,
      type: 'agent',
      position: { x: centerX, y: startY + (i + 1) * gapY },
      data: {
        label: n.label || n.name,
        type: nodeType,
        description: n.description,
        status: 'idle',
        ...(nodeType === 'tool' ? { toolNames: [n.name] } : {}),
      },
    });
  });

  const endId = `wf-${wf.id}-end`;
  nodes.push({
    id: endId,
    type: 'agent',
    position: { x: centerX, y: startY + (wf.nodes.length + 1) * gapY },
    data: { label: '结束', type: 'end', status: 'idle' },
  });

  edges.push({
    id: `wf-e-${wf.id}-start-0`,
    source: startId,
    target: `wf-${wf.id}-${wf.nodes[0].name}`,
    data: { animated: true },
  });

  wf.nodes.forEach((n, i) => {
    const targetId = i < wf.nodes.length - 1 ? `wf-${wf.id}-${wf.nodes[i + 1].name}` : endId;
    edges.push({
      id: `wf-e-${wf.id}-${i}-${i + 1}`,
      source: `wf-${wf.id}-${n.name}`,
      target: targetId,
      data: { animated: true },
    });
  });

  return { nodes, edges };
}
