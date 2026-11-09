/**
 * 文本处理工具：关键字匹配、稳定散列取模
 */

/** 关键字归一化：去空格并转小写，空关键字视为「不过滤」 */
export function normalizeKeyword(keyword: string): string {
  return keyword.trim().toLowerCase();
}

/**
 * 关键字匹配：任一字段包含关键字即命中（忽略大小写）
 * @param fields 参与匹配的字段，允许传入 undefined 便于直接铺开可选字段
 * @param keyword 已归一化的关键字；传空串直接命中
 */
export function matchKeyword(fields: Array<string | undefined>, keyword: string): boolean {
  if (!keyword) return true;
  return fields.some((field) => !!field && field.toLowerCase().includes(keyword));
}

/**
 * 工具名搜索：只匹配名称本身的前缀，或 `_` / `.` / `-` 分段的前缀
 * 避免 st 命中 understand，也不会扫描述、分类
 */
export function matchToolName(name: string, keyword: string): boolean {
  const q = normalizeKeyword(keyword);
  if (!q) return true;
  const lower = name.toLowerCase();
  if (lower.startsWith(q)) return true;
  return lower.split(/[._-]/).some((part) => part.startsWith(q));
}

/** 按 name 去重，保留首次出现。接口偶发重复项时避免 React key 冲突导致筛完仍残留旧卡片 */
export function uniqueByName<T extends { name: string }>(items: readonly T[]): T[] {
  const seen = new Set<string>();
  const result: T[] = [];
  for (const item of items) {
    const key = item.name.trim();
    if (!key || seen.has(key)) continue;
    seen.add(key);
    result.push(item);
  }
  return result;
}

/**
 * 稳定散列取模：同一 key 永远得到同一索引，用于给列表项分配固定的文案/主题
 * @param salt 盐值，用于同一 key 在不同维度上取到不同索引
 */
export function hashIndex(key: string, mod: number, salt = 0): number {
  let hash = salt;
  for (let i = 0; i < key.length; i += 1) {
    hash = (hash + key.charCodeAt(i) * (i + 1)) % mod;
  }
  return hash;
}

/** 从候选池里按 key 稳定取一项 */
export function pickByHash<T>(key: string, pool: readonly T[], salt = 0): T {
  return pool[hashIndex(key, pool.length, salt)];
}
