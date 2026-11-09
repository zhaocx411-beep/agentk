/**
 * Agent100 前端文案
 * 页头与空态没有接口来源，统一在此维护；面向使用者，不出现内部实现名词
 */

export const AGENT100_TEXT = {
  heroTitle: {
    lead: '选好 Agent，',
    accent: '准备素材',
    tail: '就能开始',
  },
  heroDesc:
    '每个 Agent 能做的事不同。按页面提示准备素材、选好选项，就可以直接试用。',
  emptyTitle: '还没有可试用的 Agent',
  emptyDesc: '新能力上线后会出现在这里，稍后再来看看。',
  listError: '列表加载失败，请稍后重试',
  listAria: '可试用的 Agent',
  detailNotFound: '没有找到这个 Agent',
  backToList: '返回列表',
  configError: '加载失败，请稍后重试',
  runTitle: '开始',
  runHint: '填好下面的内容后点击运行。标了必填的不能留空。',
  requiredHint: (count: number) =>
    count > 0 ? `其中 ${count} 项必填。` : '没有必填项。',
  runEmpty: '无需填写其他内容，可以直接运行',
  runNameLabel: '给这次尝试起个名字（可选）',
  runNamePlaceholder: '例如「第一次试用」',
  noTask: '这个 Agent 暂时还不能试用',
  taskUnavailable: '当前任务暂时无法运行',
  taskUnpublished: '这项能力还在准备中，请稍后再试',
  taskUnknown: '暂时无法开始，请稍后再试',
  capabilityNotReady: '这项能力还没准备好',
  mediaError: '素材服务暂时不可用',
  overviewAria: '当前设置',
  defaultModels: '当前模型',
  availableTasks: '可试用',
  noModels: '暂无',
  usageCount: (count: number) => `${count} 种用法`,
  fillScriptCta: '用这份脚本去出成片',
  resultTitle: '生成结果',
  resultEmpty: '填好内容并点击运行，结果会显示在这里',
  resultInput: '本次填写',
  resultOutput: '输出',
  historyTitle: '最近尝试',
  historyHint: '点一条记录，会带回当时的填写内容和结果',
  historyEmpty: '还没有尝试记录',
  jsonRequired: '请粘贴脚本内容',
  jsonOptional: '可粘贴脚本内容，也可留空',
  newSession: '新建',
  newSessionDone: '已准备好新开一次',
  newSessionHint: '这次会接着上次；不想接着就点「新建」。',
} as const;
