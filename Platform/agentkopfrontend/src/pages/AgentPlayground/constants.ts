/**
 * AgentPlayground - 页面级静态配置常量
 */

/**
 * 拉取会话账本事件的单页条数。过大时单包 JSON 过重，进会话会卡住；
 * 进会话只拉第一页（最新），更早的对话由上滚分页补齐
 */
export const SESSION_EVENTS_PAGE_SIZE = 20;

/**
 * 会话 events 账本翻页策略（官方体验 / 产物 / 画布共用）
 *
 * - 冷启动：先拉 BOOTSTRAP_PAGES 页首屏，再后台补到 ceil(total/20)
 * - 热切换（缓存已齐）：只拉第 1 页，用于同步最新态 + 判断是否续订 stream
 * - 硬上限：MAX_PAGES 页（默认 50×20=1000 条），超出部分需产品上滚或另做导出
 */
export const SESSION_LEDGER_BOOTSTRAP_PAGES = 10;
export const SESSION_LEDGER_MAX_PAGES = 50;

/** 按 API total 计算应收齐的页数（不超过硬上限） */
export function sessionLedgerTargetPages(total: number): number {
  if (total <= 0) return SESSION_LEDGER_MAX_PAGES;
  return Math.min(
    SESSION_LEDGER_MAX_PAGES,
    Math.ceil(total / SESSION_EVENTS_PAGE_SIZE),
  );
}

/** events 条数是否已覆盖 API 声明的 total */
export function isSessionLedgerComplete(eventsLength: number, total: number): boolean {
  return total > 0 && eventsLength >= total;
}

/** 对话区距顶多少像素内触发「加载更早对话」 */
export const CHAT_LOAD_MORE_THRESHOLD_PX = 80;

/** 历史会话列表单页条数 */
export const SESSIONS_PAGE_SIZE = 20;

/**
 * 不渲染工具卡片的工具名
 * job_status 是内部进度查询工具，进度与结果已体现在正文和 job 卡片里，工具卡片纯噪音
 */
export const HIDDEN_TOOL_CARDS = new Set(['job_status']);

/** Pipeline 节点类型 → Tag 颜色 */
export const NODE_TYPE_COLOR: Record<string, string> = {
  llm: 'purple',
  tool: 'processing',
  map: 'cyan',
};

/** 会话配置来源切换项：自由配置 / 复用已有 Agent 版本 */
export const SESSION_CONFIG_MODE_OPTIONS: Array<{ label: string; value: 'free' | 'agent' }> = [
  { label: '自由配置', value: 'free' },
  { label: '已有 Agent', value: 'agent' },
];
