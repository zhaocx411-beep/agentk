/**
 * AgentLoop 持久化相关类型
 * 对接 /agent/operation/backend/platform/* 接口
 */
import type { BaseProfile } from '@/pages/AgentPlayground/types';

/** AgentLoop 逻辑实体（挂载多份 profile 版本） */
export interface AgentLoop {
  id: number;
  /** profile 的名字 / agent 名字 */
  name: string;
  /** profile 的描述 */
  description: string;
  ownerUserId: string;
  tenantId: string;
  /** 最新已发布版本号，0 表示尚未发布 */
  latestVersion: number;
  status: string;
  isDefault?: boolean;
  /** 可见性，如 private */
  visibility?: string;
  createdAt: string;
  updatedAt: string;
}

/** AgentLoop 的 profile 版本 */
export interface AgentLoopVersion {
  id: number;
  agentId: number;
  /** 版本号，自增 */
  version: number;
  /** 历史字段名沿用 profileYaml，最新版接口实际返回 Agent-k Profile JSON object */
  profileYaml: BaseProfile;
  contentHash: string;
  profileVersionLabel: string;
  sourceLabel: string;
  /** draft | published */
  status: string;
  createdBy: string;
  createdAt: string;
  publishedAt?: string;
  publishedBy?: string;
}

/** 创建 AgentLoop 请求体 */
export interface CreateAgentLoopRequest {
  name: string;
  description: string;
}

/** 保存版本草稿请求体 */
export interface SaveVersionDraftRequest {
  /** 必填：Agent-k Profile JSON object（字段名由平台历史原因沿用 profileYaml） */
  profileYaml: BaseProfile;
  specJson?: Record<string, unknown>;
  sourceLabel?: string;
}
