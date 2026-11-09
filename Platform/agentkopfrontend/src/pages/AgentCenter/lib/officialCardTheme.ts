/** 官方卡片封面主题索引：按 key 稳定分配 4 套渐变色 */
export function officialCoverThemeIndex(key: string): number {
  let hash = 0;
  for (let i = 0; i < key.length; i += 1) {
    hash = (hash + key.charCodeAt(i)) % 4;
  }
  return hash;
}
