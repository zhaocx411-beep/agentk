/**
 * AgentCenter - 页面级静态配置常量
 * 只放与渲染无关的纯数据（映射表、Tab 配置、分页与动效时长），含 JSX 的配置留在组件内
 */
import type { AgentCardData, MineStatusFilter, OfficialCatalogType } from './types';

/** Agent 发布状态 → Tag 颜色与文案（列表卡片、详情顶栏共用） */
export const AGENT_STATUS_META: Record<AgentCardData['status'], { color: string; text: string }> = {
  published: { color: 'success', text: '已发布' },
  draft: { color: 'default', text: '草稿' },
  builtin: { color: 'processing', text: '内置' },
};

/** 官方目录类型 → 分类全量视图的分区标题 */
export const OFFICIAL_TYPE_TITLE: Record<OfficialCatalogType, string> = {
  planning: 'Agent-Loop',
  workflow: '工作流',
};

/** 官方目录类型切换选项 */
export const OFFICIAL_CATALOG_TYPE_OPTIONS: Array<{ label: string; value: OfficialCatalogType }> = [
  { label: OFFICIAL_TYPE_TITLE.planning, value: 'planning' },
  { label: OFFICIAL_TYPE_TITLE.workflow, value: 'workflow' },
];

/** 推荐分区每类预览卡片数 */
export const OFFICIAL_PREVIEW_COUNT = 3;

/** 我的 Agent 列表分页大小，与 GET /platform/agent-loops 的 pageSize 一致 */
export const MINE_AGENT_PAGE_SIZE = 20;

/** 我的 Agent 首屏骨架卡数量 */
export const MINE_SKELETON_COUNT = 6;

/** 我的 Agent 状态筛选下拉项 */
export const MINE_STATUS_OPTIONS: Array<{ value: MineStatusFilter; label: string }> = [
  { value: 'all', label: '全部状态' },
  { value: 'published', label: '已发布' },
  { value: 'draft', label: '草稿' },
];

/** 精选轮播最多展示条数（排序后截断） */
export const FEATURED_MAX_COUNT = 3;

/** 精选轮播自动切换间隔（毫秒） */
export const FEATURED_AUTOPLAY_MS = 8000;

/** 精选轮播淡入淡出时长（毫秒），需与 module.less 中的动画时长保持一致 */
export const FEATURED_FADE_MS = 1200;
