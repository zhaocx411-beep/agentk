/**
 * AgentPlayground - 前端 mock 文案
 * 对话空态的引导语与快捷提问由前端兜底，接口未提供推荐问题时使用
 */

/** 对话空态默认快捷提问 */
export const DEFAULT_CHAT_SUGGESTIONS = [
  '你能帮我做什么？',
  '按你的能力给一个示例任务',
  '介绍一下可用工具',
];

/** 对话空态引导文案 */
export const CHAT_EMPTY_TEXT = {
  title: '开始新的对话',
  desc: '在下方输入问题即可开始。也可以先点一个快捷提问。',
} as const;
