/**
 * AgentLoop 默认 Profile 模版
 * GET /agent/operation/backend/platform/session-profiles/templates/agent-loop-default
 */
import type { BaseProfile, ProfileToolRef } from '@/pages/AgentPlayground/types';

/** 模版 tools 项：后端下发 capability，保存时需转为 ref */
export interface AgentLoopTemplateToolRef extends ProfileToolRef {
  capability?: string;
}

export interface AgentLoopDefaultTemplateProfile extends Omit<BaseProfile, 'tools'> {
  tools?: AgentLoopTemplateToolRef[];
}

export interface AgentLoopDefaultTemplateData {
  templateKey: string;
  profileVersion: string;
  description: string;
  persistedOnCreateSession: boolean;
  profile: AgentLoopDefaultTemplateProfile;
}
