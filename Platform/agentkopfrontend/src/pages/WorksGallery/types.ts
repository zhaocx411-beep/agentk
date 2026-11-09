/** 作品媒体类型 */
export type WorksMediaType = 'image' | 'video';

/** 作品生成来源 */
export type WorksSource = 'online' | 'offline';

/** 列表及同轮作品使用的资源摘要 */
export interface PortfolioAssetSummary {
  assetId: number;
  assetType: WorksMediaType;
  assetUrl: string;
  promptText?: string;
  agentkEventSeq?: number;
  createdAt?: string;
}

/** 列表中的会话摘要 */
export interface PortfolioSessionSummary {
  sessionId: number;
  publicId: string;
  agentkSessionId: string;
  title: string;
  sessionKind: string;
  effectiveModel: string;
  agentLoopVersionId?: number;
  workflowVersionId?: number;
  sessionCreatedAt: string;
}

/** 一轮创作记录 */
export interface PortfolioRound {
  roundKey: string;
  runId: number;
  agentkRunId: string;
  workflowExecId: string;
  sourceKind: string;
  source: string;
  sourceName: string;
  agentName?: string;
  promptText: string;
  userId: string;
  createdAt: string;
  cover: PortfolioAssetSummary;
  intermediateAssets: PortfolioAssetSummary[];
  resultAssets: PortfolioAssetSummary[];
  session: PortfolioSessionSummary;
}

export interface PortfolioQueryParams {
  assetType?: WorksMediaType;
  sourceKind?: string;
  source?: WorksSource;
  sessionId?: number;
  promptKeyword?: string;
  agentName?: string;
  /** 后端作者筛选参数，联调阶段按列表返回字段 userId 传递 */
  userId?: string;
  page: number;
  pageSize: number;
}

export interface PortfolioPage {
  page: number;
  pageSize: number;
  total: number;
  records: PortfolioRound[];
}

export interface PortfolioVersionSummary {
  versionId: number;
  parentId: number;
  version: number;
  status: string;
  name?: string;
  profileVersionLabel?: string;
}

export interface PortfolioSessionDetail {
  sessionId: number;
  publicId: string;
  agentkSessionId: string;
  title: string;
  sessionKind: string;
  entrySource: string;
  status: string;
  profileVersion?: string;
  effectiveModel?: string;
  effectiveToolsJson?: string;
  createdAt: string;
  updatedAt?: string;
  agentLoopVersion?: PortfolioVersionSummary;
  workflowVersion?: PortfolioVersionSummary;
}

export interface PortfolioRun {
  runId: number;
  agentkRunId: string;
  status: string;
  model: string;
  profileVersion: string;
  createdAt: string;
  finishedAt: string;
}

export interface PortfolioProfileTool {
  name: string;
  ref: string;
  instructions?: string;
}

export interface PortfolioProfile {
  defaultModel?: string;
  system?: string;
  toolDefaults?: Record<string, Record<string, unknown>>;
  tools?: PortfolioProfileTool[];
}

/** 单个作品详情（Agent-loop 会话产物含 session/run；API 写入产物可能仅有 source/workflowExecId） */
export interface PortfolioAssetDetail {
  assetId: number;
  assetType: WorksMediaType;
  assetUrl: string;
  promptText: string;
  sourceKind: string;
  source: string;
  sourceName: string;
  agentName?: string;
  workflowName?: string;
  workflowExecId: string;
  agentkRunId?: string;
  agentkEventSeq?: number;
  runId?: number;
  messageId?: number;
  userId: string;
  status: string;
  createdAt: string;
  updatedAt: string;
  roundKey: string;
  sameRoundAssets: PortfolioAssetSummary[];
  session?: PortfolioSessionDetail;
  run?: PortfolioRun;
  /** 本次运行使用的 Profile 快照 */
  profile?: PortfolioProfile;
}
