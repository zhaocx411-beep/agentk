/**
 * Agent100 页面级静态配置
 */

/** 首屏骨架卡数量：铺满一行三列 */
export const AGENT100_SKELETON_COUNT = 3;

/** 历史列表：存在运行中记录时的刷新间隔 */
export const AGENT100_HISTORY_POLL_MS = 2500;

/** JSON Schema 字段中文标签；未命中时回退字段名 */
export const AGENT100_FIELD_LABELS: Record<string, string> = {
  videoUri: '原视频',
  theme: '主题',
  instructions: '补充指令',
  models: '模型',
  vision: '视觉模型',
  asr: '语音识别',
  search: '搜索模型',
  story: '故事模型',
  script: '脚本',
  tts: '语音合成',
  provider: '音色引擎',
  model: '语音模型',
  voiceId: '配音音色',
  analysisOptions: '原声识别',
  asrEnabled: '识别视频原声',
  asrLanguage: '识别语言',
  narrativeProfile: '叙事设定',
  narrationCues: '旁白片段',
  aspect: '画幅',
  narrationVolume: '旁白音量',
  originalAudioVolume: '原声音量',
  subtitles: '字幕',
  id: '编号',
  sourceStartSeconds: '起始秒',
  sourceDurationSeconds: '片长',
  sourceVideoUri: '原视频',
  segments: '分段',
};

/** 适合用多行输入的字符串字段 */
export const AGENT100_TEXTAREA_FIELDS = new Set(['theme', 'instructions']);

/** 用素材上传控件的 URI 字段 */
export const AGENT100_MEDIA_FIELDS = new Set(['videoUri', 'sourceVideoUri']);
