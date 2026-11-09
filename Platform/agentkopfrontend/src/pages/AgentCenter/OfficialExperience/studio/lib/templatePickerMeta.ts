interface TemplateCatalogMetaOptions {
  loading: boolean;
  error: boolean;
  loadedCount: number;
  total: number;
}

/** 创作模版下拉右上角的数量 / 状态文案 */
export function formatTemplateCatalogMeta({
  loading,
  error,
  loadedCount,
  total,
}: TemplateCatalogMetaOptions): string {
  if (loading) return '加载中…';
  if (error) return '加载失败';
  if (total > 0) return `${loadedCount}/${total} 个`;
  return '0 个可选';
}
