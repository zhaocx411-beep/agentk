/**
 * ToolGallery - 前端 mock 文案
 * 页头营销文案与空态提示没有接口来源，统一在此维护
 */

/** 工具广场页头文案 */
export const TOOL_GALLERY_TEXT = {
  /** 页头主标题：拆出重点词单独强调，其余部分保持墨色 */
  heroTitle: {
    lead: '官方工具直达，',
    accent: '灵活组合',
    tail: '可落地的能力',
  },
  heroDesc: '平台预置 Skill 工具目录，可按分类浏览，并作为 Agent 能力扩展参考。',
  searchPlaceholder: '搜索工具名称',
  emptyTitle: '暂无官方工具',
  emptyDesc:
    '工具由平台侧统一注册，注册后会自动出现在这里；创建 Agent 时可在能力配置中挂载这些工具。',
  /** 分类下无匹配工具时的空态 */
  emptyFilteredDesc: '暂无匹配的官方工具',
  emptySearchTitle: '没有匹配的工具',
  emptySearchDesc: '试试其他关键词，或清空搜索查看全部工具。',
  detailNotFound: '未找到该工具',
  referOrFeatureHint: '参考图 referImages 与参考视频 featureVideo 至少填一项，仅填首帧/尾帧无法运行。',
  referOrFeatureRequired: 'referImages 和 featureVideo 至少填写一个',
  referImagesPairHint: '与 featureVideo 至少填写一个',
  featureVideoPairHint: '与 referImages 至少填写一个',
} as const;

