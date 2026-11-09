/**
 * 通用格式化工具：时间点、时长、超时时间的展示文案
 * 说明：这里只放与业务无关的纯展示格式化，业务视图模型映射请放在各页面的 utils.ts
 */

/** 数字补零到两位：7 → "07" */
export function pad2(n: number): string {
  return String(n).padStart(2, '0');
}

/** Date → 「HH:mm:ss」时钟文案，用于「自动保存于 xx」这类实时提示 */
export function formatClock(date: Date): string {
  return `${pad2(date.getHours())}:${pad2(date.getMinutes())}:${pad2(date.getSeconds())}`;
}

/** ISO 时间 → 今天显示「今天 HH:mm」，非今天显示「MM-DD HH:mm」；非法时间返回空串 */
export function formatUpdatedTime(iso?: string): string {
  if (!iso) return '';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  const hm = `${pad2(date.getHours())}:${pad2(date.getMinutes())}`;
  const now = new Date();
  const isToday =
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate();
  if (isToday) return `今天 ${hm}`;
  return `${pad2(date.getMonth() + 1)}-${pad2(date.getDate())} ${hm}`;
}

/** 毫秒时长 → 「Xs」/「XmYYs」/「XhYYm」（对齐 agent-k demoui fmtElapsed） */
export function formatElapsed(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m${pad2(s % 60)}s`;
  const h = Math.floor(m / 60);
  return `${h}h${pad2(m % 60)}m`;
}

/** 超时秒数 → 「X 小时 / X 分钟 / X 秒」，缺省或 0 返回 "-" */
export function formatTimeout(seconds?: number): string {
  if (!seconds) return '-';
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`;
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`;
  return `${seconds} 秒`;
}
