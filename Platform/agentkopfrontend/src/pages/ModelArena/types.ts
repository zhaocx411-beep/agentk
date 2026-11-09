/** 参战模型（管理侧展示名称；对战页投票前匿名） */
export interface ArenaModel {
  id: string;
  displayName: string;
  provider: string;
  modality: 'vlm' | 'llm';
  enabled: boolean;
}

/** 首页示例卡片 */
export interface ArenaExample {
  id: string;
  imageUrl: string;
  prompt: string;
}

export type ArenaBattleStatus = 'draft' | 'running' | 'voting' | 'completed' | 'invalid';

/** 对战管理列表行 */
export interface ArenaBattleRecord {
  id: string;
  title: string;
  modelA: string;
  modelB: string;
  winner?: 'A' | 'B' | 'tie';
  status: ArenaBattleStatus;
  votesA: number;
  votesB: number;
  creator: string;
  createdAt: string;
  completedAt?: string;
  /** 本轮用户问题 */
  prompt: string;
  imageUrl?: string;
  /** 模型回复（已完成 / 投票中可展示） */
  responseA?: string;
  responseB?: string;
  /** 失效原因 */
  invalidReason?: string;
  /** 管理侧摘要 */
  summary?: string;
}

export type ArenaPhase = 'landing' | 'battling' | 'revealed';

export interface ArenaTurn {
  role: 'user' | 'modelA' | 'modelB';
  text: string;
}
