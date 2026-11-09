import { create } from 'zustand';
import {
  applyNodeChanges,
  applyEdgeChanges,
  addEdge,
  type OnNodesChange,
  type OnEdgesChange,
  type OnConnect,
  type Connection,
  type NodeChange,
  type EdgeChange,
} from '@xyflow/react';
import type { AgentNode, AgentEdge, AgentNodeData, WorkflowData } from './types';
import { DEFAULT_WORKFLOW_NAME, INITIAL_EDGES, INITIAL_NODES } from './mock';

/** 节点运行状态（与画布节点 id 对应） */
export type NodeRunStatus = 'idle' | 'running' | 'success' | 'error';

/** 历史快照（用于撤销/重做） */
interface HistorySnapshot {
  nodes: AgentNode[];
  edges: AgentEdge[];
}

/** 最大历史记录数 */
const MAX_HISTORY = 50;

/** Agent-k 全局配置：与 playground 新会话设置对齐，导出到调试台覆盖 profile */
export interface AgentKConfig {
  /** 模型（单选，对应 playground 的 selectedModel） */
  model: string;
  /** 启用工具列表（多选，对应 playground 的 selectedTools） */
  tools: string[];
  /** System Prompt（对应 profile YAML 的 system 字段） */
  systemPrompt: string;
}

interface WorkflowState {
  nodes: AgentNode[];
  edges: AgentEdge[];
  selectedNodeId: string | null;
  workflowName: string;
  /** Agent-k 全局配置（与 playground 对齐，用于跨页面导入覆盖） */
  agentKConfig: AgentKConfig;
  /** 节点运行状态映射（由 WorkflowPanel 调试运行时写入） */
  nodeRunStatuses: Record<string, NodeRunStatus>;
  /** 批量更新节点运行状态（调试运行/轮询时调用） */
  setNodeRunStatuses: (statuses: Record<string, NodeRunStatus>) => void;
  /** 重置所有节点运行状态为 idle */
  resetNodeRunStatuses: () => void;
  /** Agent-k 配置抽屉开关 */
  agentKDrawerOpen: boolean;

  onNodesChange: OnNodesChange<AgentNode>;
  onEdgesChange: OnEdgesChange<AgentEdge>;
  onConnect: OnConnect;

  setSelectedNode: (id: string | null) => void;
  updateNodeData: (id: string, data: Partial<AgentNodeData>) => void;
  addAgentNode: (node: AgentNode) => void;
  deleteNode: (id: string) => void;
  setWorkflowName: (name: string) => void;
  /** 更新 Agent-k 全局配置（增量合并） */
  setAgentKConfig: (config: Partial<AgentKConfig>) => void;
  /** 从画布节点同步 Agent-k 全局配置（取首个 LLM 节点 + 全部 Tool 节点） */
  syncAgentKConfigFromCanvas: () => void;
  /** 导出 Pipeline */
  exportWorkflow: () => WorkflowData;
  /** 清空画布 */
  clearCanvas: () => void;
  /** 加载快捷模板：原子替换整张画布（只记一次历史） */
  loadTemplate: (nodes: AgentNode[], edges: AgentEdge[]) => void;
  /** 撤销 */
  undo: () => void;
  /** 重做 */
  redo: () => void;
  /** 是否可撤销 */
  canUndo: () => boolean;
  /** 是否可重做 */
  canRedo: () => boolean;
  /** 复制选中的节点 */
  copyNode: (id: string) => void;
  /** 粘贴节点，可指定目标位置（如鼠标位置） */
  pasteNode: (targetPosition?: { x: number; y: number }) => void;
  /** 剪贴板中的节点 */
  clipboard: AgentNode | null;
}

export const useWorkflowStore = create<WorkflowState>((set, get) => {
  /** 历史栈 */
  let history: HistorySnapshot[] = [{ nodes: INITIAL_NODES, edges: INITIAL_EDGES }];
  let historyIndex = 0;

  /** 推入历史（跳过中间态的 PositionChange） */
  const pushHistory = () => {
    const { nodes, edges } = get();
    // 截断 redo 部分
    history = history.slice(0, historyIndex + 1);
    history.push({
      nodes: JSON.parse(JSON.stringify(nodes)),
      edges: JSON.parse(JSON.stringify(edges)),
    });
    if (history.length > MAX_HISTORY) {
      history = history.slice(-MAX_HISTORY);
    }
    historyIndex = history.length - 1;
  };

  return {
    nodes: INITIAL_NODES,
    edges: INITIAL_EDGES,
    selectedNodeId: null,
    workflowName: DEFAULT_WORKFLOW_NAME,
    clipboard: null,
    // Agent-k 全局配置默认空，由用户在「Agent-k 配置」面板中编辑或从画布同步
    agentKConfig: { model: '', tools: [], systemPrompt: '' },
    nodeRunStatuses: {},
    agentKDrawerOpen: false,

    onNodesChange: (changes: NodeChange<AgentNode>[]) => {
      set({ nodes: applyNodeChanges(changes, get().nodes) });
      // 只在非 Position 变更时推历史（拖拽过程中不频繁推）
      const hasSignificantChange = changes.some(
        (c) => c.type !== 'position' && c.type !== 'select',
      );
      if (hasSignificantChange) {
        pushHistory();
      }
    },

    onEdgesChange: (changes: EdgeChange<AgentEdge>[]) => {
      set({ edges: applyEdgeChanges(changes, get().edges) });
      const hasSignificantChange = changes.some((c) => c.type !== 'select');
      if (hasSignificantChange) {
        pushHistory();
      }
    },

    onConnect: (connection: Connection) => {
      set({
        edges: addEdge(
          {
            ...connection,
            type: 'agentEdge',
            data: { animated: true },
          },
          get().edges,
        ),
      });
      pushHistory();
    },

    setNodeRunStatuses: (statuses) => set({ nodeRunStatuses: statuses }),
    resetNodeRunStatuses: () => set({ nodeRunStatuses: {} }),
    setSelectedNode: (id) => set({ selectedNodeId: id }),

    updateNodeData: (id, data) => {
      set({
        nodes: get().nodes.map((node) =>
          node.id === id ? { ...node, data: { ...node.data, ...data } } : node,
        ),
      });
      pushHistory();
    },

    addAgentNode: (node) => {
      // 开始/结束节点全局唯一：已有则替换
      if (node.data.type === 'start' || node.data.type === 'end') {
        const existingIdx = get().nodes.findIndex((n) => n.data.type === node.data.type);
        if (existingIdx >= 0) {
          const updated = [...get().nodes];
          updated[existingIdx] = { ...node, id: updated[existingIdx].id, position: node.position };
          set({ nodes: updated });
          pushHistory();
          return;
        }
      }
      set({ nodes: [...get().nodes, node] });
      pushHistory();
    },

    deleteNode: (id) => {
      const target = get().nodes.find((n) => n.id === id);
      // 开始/结束节点不可删除
      if (target?.data.type === 'start' || target?.data.type === 'end') return;
      set({
        nodes: get().nodes.filter((n) => n.id !== id),
        edges: get().edges.filter((e) => e.source !== id && e.target !== id),
        selectedNodeId: get().selectedNodeId === id ? null : get().selectedNodeId,
      });
      pushHistory();
    },

    setWorkflowName: (name) => set({ workflowName: name }),

    setAgentKConfig: (config) =>
      set((state) => ({ agentKConfig: { ...state.agentKConfig, ...config } })),

    syncAgentKConfigFromCanvas: () => {
      const { nodes } = get();
      const llmNodes = nodes.filter((n) => n.data.type === 'llm');
      const toolNodes = nodes.filter((n) => n.data.type === 'tool');
      const primaryLlm = llmNodes[0];
      // 收集所有工具节点的 toolNames（多选），去重后合并
      const toolSet = new Set<string>();
      toolNodes.forEach((n) => {
        (n.data.toolNames ?? []).forEach((t) => {
          if (t) toolSet.add(t);
        });
      });
      set({
        agentKConfig: {
          model: primaryLlm?.data.model || '',
          tools: Array.from(toolSet),
          systemPrompt: primaryLlm?.data.prompt || '',
        },
      });
    },

    exportWorkflow: () => {
      const { nodes, edges, workflowName } = get();
      return {
        id: `wf-${Date.now()}`,
        name: workflowName,
        nodes: nodes.map(({ id, type, position, data }) => ({ id, type, position, data })),
        edges: edges.map(({ id, source, target, sourceHandle, targetHandle, data }) => ({
          id,
          source,
          target,
          sourceHandle,
          targetHandle,
          data,
        })),
      } satisfies WorkflowData;
    },

    clearCanvas: () => {
      set({ nodes: [], edges: [], selectedNodeId: null });
      pushHistory();
    },

    loadTemplate: (nodes, edges) => {
      set({ nodes, edges, selectedNodeId: null });
      pushHistory();
    },

    undo: () => {
      if (historyIndex <= 0) return;
      historyIndex -= 1;
      const snapshot = history[historyIndex];
      set({ nodes: snapshot.nodes, edges: snapshot.edges, selectedNodeId: null });
    },

    redo: () => {
      if (historyIndex >= history.length - 1) return;
      historyIndex += 1;
      const snapshot = history[historyIndex];
      set({ nodes: snapshot.nodes, edges: snapshot.edges, selectedNodeId: null });
    },

    canUndo: () => historyIndex > 0,
    canRedo: () => historyIndex < history.length - 1,

    copyNode: (id) => {
      const node = get().nodes.find((n) => n.id === id);
      // 开始/结束节点不允许复制
      if (!node || node.data.type === 'start' || node.data.type === 'end') return;
      set({ clipboard: { ...node, position: { ...node.position } } });
    },

    pasteNode: (targetPosition?: { x: number; y: number }) => {
      const { clipboard } = get();
      if (!clipboard) return;
      // 开始/结束节点不允许粘贴
      if (clipboard.data.type === 'start' || clipboard.data.type === 'end') return;
      const position = targetPosition ?? {
        x: clipboard.position.x + 50,
        y: clipboard.position.y + 50,
      };
      const newNode: AgentNode = {
        ...clipboard,
        id: `node-${Date.now()}`,
        position,
        data: { ...clipboard.data },
        // 确保新节点不处于选中状态，避免多选拖动
        selected: false,
      };
      // 粘贴时清空所有节点的选中状态，避免多选拖动
      const clearedNodes = get().nodes.map((n) => (n.selected ? { ...n, selected: false } : n));
      set({ nodes: [...clearedNodes, newNode], selectedNodeId: null });
      pushHistory();
    },
  };
});
