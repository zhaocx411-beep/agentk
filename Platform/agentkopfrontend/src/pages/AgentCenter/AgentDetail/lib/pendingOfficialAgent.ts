/**
 * 「基于此创建」暂存：先本地编辑官方 profile，保存草稿时再 POST 创建 agent-loop
 */
import type { BaseProfile } from '@/pages/AgentPlayground/types';

export const FROM_OFFICIAL_AGENT_ID = 'from-official';

const STORAGE_KEY = 'agentk-pending-official-agent';

export interface PendingOfficialAgentDraft {
  officialAgentLoopId: number;
  officialAgentLoopVersionId?: number;
  suggestedName: string;
  description: string;
  profileYaml: BaseProfile;
  /** 首次保存成功后写入；有值则后续只 saveVersionDraft，不再 create */
  createdAgentLoopId?: number;
}

export function isFromOfficialAgentId(agentId: string): boolean {
  return agentId === FROM_OFFICIAL_AGENT_ID;
}

export function readPendingOfficialAgent(): PendingOfficialAgentDraft | null {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    const data = JSON.parse(raw) as PendingOfficialAgentDraft;
    if (!data || typeof data.officialAgentLoopId !== 'number') return null;
    if (!data.profileYaml || typeof data.profileYaml !== 'object') return null;
    return data;
  } catch {
    return null;
  }
}

export function writePendingOfficialAgent(draft: PendingOfficialAgentDraft): void {
  sessionStorage.setItem(STORAGE_KEY, JSON.stringify(draft));
}

export function clearPendingOfficialAgent(): void {
  sessionStorage.removeItem(STORAGE_KEY);
}

export function buildFromOfficialPath(): string {
  return `/agents/${FROM_OFFICIAL_AGENT_ID}`;
}
