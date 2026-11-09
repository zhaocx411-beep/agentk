/**
 * URL 查询参数解析工具
 * 页面通过 useSearchParams 拿到的值是 string | null，需要收敛成受限的联合类型
 */

/**
 * 把查询参数收敛为枚举值：命中白名单则返回原值，否则回退默认值
 * @param raw useSearchParams().get(key) 的原始返回
 * @param allowed 允许的取值白名单
 * @param fallback 非法或缺省时的兜底值
 */
export function parseEnumParam<T extends string>(
  raw: string | null,
  allowed: readonly T[],
  fallback: T,
): T {
  return allowed.includes(raw as T) ? (raw as T) : fallback;
}

/**
 * 与 parseEnumParam 相同，但缺省时返回 undefined（用于「不限类型」这类可选筛选）
 */
export function parseOptionalEnumParam<T extends string>(
  raw: string | null,
  allowed: readonly T[],
): T | undefined {
  return allowed.includes(raw as T) ? (raw as T) : undefined;
}
