import { create } from 'zustand';
import type { WorkflowData } from '@/pages/WorkflowEditor/types';

/** 跨页面传递的 Agent 配置 */
export interface AgentConfigPayload {
  /** 来源标识 */
  source: 'workflow-editor' | 'agent-playground';
  /** 选择的模型 */
  model: string;
  /** System Prompt */
  systemPrompt: string;
  /** 启用的工具列表 */
  tools: string[];
  /** 会话级 profile YAML 原文（system + tools），用于覆盖新会话的 profile */
  profileYaml?: string;
  /** 原始 Pipeline 数据（从编辑器过来时携带） */
  workflow?: WorkflowData;
}

/** 共享 Agent 配置状态 */
interface AgentConfigState {
  /** 待消费的配置（读取后清空） */
  pendingConfig: AgentConfigPayload | null;
  /** 写入配置（由来源页面调用） */
  setPendingConfig: (config: AgentConfigPayload) => void;
  /** 读取并清空配置（由目标页面调用，一次性消费） */
  consumePendingConfig: () => AgentConfigPayload | null;
  /** 清空配置 */
  clearPendingConfig: () => void;
}

/**
 * 跨页面 Agent 配置中转站
 * - Pipeline 编辑器写入 → Agent 调试台消费
 * - Agent 调试台写入 → Pipeline 编辑器消费
 * - consumePendingConfig 读取后自动清空，避免重复消费
 */
export const useAgentConfigStore = create<AgentConfigState>((set, get) => ({
  pendingConfig: null,

  setPendingConfig: (config) => set({ pendingConfig: config }),

  consumePendingConfig: () => {
    const { pendingConfig } = get();
    if (pendingConfig) {
      set({ pendingConfig: null });
    }
    return pendingConfig;
  },

  clearPendingConfig: () => set({ pendingConfig: null }),
}));
