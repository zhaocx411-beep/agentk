/**
 * 官方工具广场
 * GET /agent/operation/backend/platform/official/tools
 */

/** 官方工具入参声明（可嵌套 children） */
export interface OfficialToolParameter {
  name: string;
  type: string;
  required?: boolean;
  description?: string;
  defaultValue?: string | number | boolean | null;
  enumValues?: Array<string | number | boolean>;
  children?: OfficialToolParameter[];
}

/** 官方工具目录项 */
export interface OfficialToolItem {
  name: string;
  /** activity / workflow 等，用于广场分组 */
  kind?: string;
  /** 如 agentk.fetch_product_detail */
  capability?: string;
  /** Catalog Tool ref，如 tool:agentk.generate_image；创建会话 profile.tools 需带上 */
  ref?: string;
  modelSelectable?: boolean;
  modelChoices?: string[];
  description?: string;
  parameters?: OfficialToolParameter[];
}

/** 官方工具广场响应（兼容数组或带 tools 字段的对象） */
export interface OfficialToolsData {
  total?: number;
  catalogVersion?: string;
  defaultModel?: string;
  models?: string[];
  tools: OfficialToolItem[];
  toolModels?: Record<string, unknown>;
  toolDefaults?: Record<string, unknown>;
}

/** 工具广场卡片视图模型（字段均来自接口） */
export interface OfficialToolCardData {
  key: string;
  name: string;
  kind: string;
  capability: string;
  description: string;
  modelSelectable: boolean;
  parameters: OfficialToolParameter[];
}
