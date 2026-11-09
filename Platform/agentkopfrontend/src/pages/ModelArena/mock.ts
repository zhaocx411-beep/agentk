import type { ArenaBattleRecord, ArenaExample, ArenaModel, ArenaTurn } from './types';

export const MOCK_ARENA_MODELS: ArenaModel[] = [
  {
    id: 'm1',
    displayName: 'Kling-VLM-Pro',
    provider: 'Kling',
    modality: 'vlm',
    enabled: true,
  },
  {
    id: 'm2',
    displayName: 'GPT-4o-Vision',
    provider: 'OpenAI',
    modality: 'vlm',
    enabled: true,
  },
  {
    id: 'm3',
    displayName: 'Qwen2.5-VL-72B',
    provider: 'Alibaba',
    modality: 'vlm',
    enabled: true,
  },
  {
    id: 'm4',
    displayName: 'Gemini-2.0-Flash',
    provider: 'Google',
    modality: 'vlm',
    enabled: true,
  },
];

export const MOCK_ARENA_EXAMPLES: ArenaExample[] = [
  {
    id: 'ex-1',
    imageUrl: 'https://images.unsplash.com/photo-1506905925346-21bda4d32df4?w=640&h=480&fit=crop',
    prompt: '这种图片是什么类型的图片',
  },
  {
    id: 'ex-2',
    imageUrl: 'https://images.unsplash.com/photo-1501785888041-af3ef285b470?w=640&h=480&fit=crop',
    prompt: '图中的桥是桥梁吗',
  },
  {
    id: 'ex-3',
    imageUrl: 'https://images.unsplash.com/photo-1526778548025-fa2f288cd1af?w=640&h=480&fit=crop',
    prompt: '图中展示的是哪里的地图',
  },
  {
    id: 'ex-4',
    imageUrl: 'https://images.unsplash.com/photo-1470071459604-3b5ec3a7fe05?w=640&h=480&fit=crop',
    prompt: '描述画面中的天气与光线',
  },
  {
    id: 'ex-5',
    imageUrl: 'https://images.unsplash.com/photo-1469474968028-56623f02e42e?w=640&h=480&fit=crop',
    prompt: '图中有哪些可识别的物体',
  },
  {
    id: 'ex-6',
    imageUrl: 'https://images.unsplash.com/photo-1441974231531-c6227db76b6e?w=640&h=480&fit=crop',
    prompt: '这张图的拍摄场景可能在哪里',
  },
];

export const MOCK_BATTLE_RECORDS: ArenaBattleRecord[] = [
  {
    id: 'arena_8f2a1c',
    title: '地图识别 · 匿名对战 #128',
    modelA: 'Kling-VLM-Pro',
    modelB: 'Qwen2.5-VL-72B',
    winner: 'A',
    status: 'completed',
    votesA: 12,
    votesB: 7,
    creator: 'chenlong',
    createdAt: '2026-08-20 10:12:03',
    completedAt: '2026-08-20 10:18:41',
    prompt: '图中展示的是哪里的地图',
    imageUrl: MOCK_ARENA_EXAMPLES[2].imageUrl,
    responseA: '从行政区划与地形轮廓判断，这更接近中国四川省及其周边区域的卫星示意地图。',
    responseB: '图像为东亚地区卫星地图切片，但省界标注不完整，无法精确到单一省份。',
    summary: '已完成匿名投票；Kling-VLM-Pro 在地理轮廓识别上获得更多票数。',
  },
  {
    id: 'arena_3b91de',
    title: '桥梁判断 · 匿名对战 #127',
    modelA: 'GPT-4o-Vision',
    modelB: 'Gemini-2.0-Flash',
    winner: 'B',
    status: 'completed',
    votesA: 5,
    votesB: 9,
    creator: 'eval-bot',
    createdAt: '2026-08-19 16:44:20',
    completedAt: '2026-08-19 16:51:02',
    prompt: '图中的桥是桥梁吗',
    imageUrl: MOCK_ARENA_EXAMPLES[1].imageUrl,
    responseA: '画面中跨河谷的结构具备桥墩与桥面，可以判定为桥梁。',
    responseB: '是的，这是跨越山谷的人行/公路桥，桥体结构与连接道路特征明显。',
    summary: '已完成；Gemini 在结构细节描述上略胜一筹。',
  },
  {
    id: 'arena_71ac0f',
    title: '风景描述 · 匿名对战 #126',
    modelA: 'Kling-VLM-Pro',
    modelB: 'GPT-4o-Vision',
    status: 'voting',
    votesA: 3,
    votesB: 2,
    creator: 'chenlong',
    createdAt: '2026-08-19 11:05:18',
    prompt: '描述画面中的天气与光线',
    imageUrl: MOCK_ARENA_EXAMPLES[3].imageUrl,
    responseA: '清晨侧光，山谷间有薄雾，整体偏暖色调。',
    responseB: '光线柔和，云层较厚，属于多云天气下的散射光。',
    summary: '双模型已回复，等待更多评测者投票。',
  },
  {
    id: 'arena_d4e882',
    title: '物体计数 · 匿名对战 #125',
    modelA: 'Qwen2.5-VL-72B',
    modelB: 'Gemini-2.0-Flash',
    status: 'invalid',
    votesA: 0,
    votesB: 0,
    creator: 'qa-team',
    createdAt: '2026-08-18 09:30:00',
    completedAt: '2026-08-18 09:32:11',
    prompt: '你是谁？请告诉我你的模型名称',
    invalidReason: '用户问题暴露模型身份，违反匿名对战规则，本轮投票作废。',
    summary: '因违规提问被系统自动标记为失效。',
  },
  {
    id: 'arena_0091aa',
    title: '草稿 · 待开始',
    modelA: 'Kling-VLM-Pro',
    modelB: 'Gemini-2.0-Flash',
    status: 'draft',
    votesA: 0,
    votesB: 0,
    creator: 'chenlong',
    createdAt: '2026-08-20 14:00:00',
    prompt: '图中有哪些可识别的物体',
    imageUrl: MOCK_ARENA_EXAMPLES[4].imageUrl,
    summary: '已创建轮次，尚未发送问题，可在对战页继续完成。',
  },
];

export function getBattleRecordById(id: string): ArenaBattleRecord | undefined {
  return MOCK_BATTLE_RECORDS.find((record) => record.id === id);
}

/** Mock 双模型回复（投票前匿名展示） */
export function buildMockBattleTurns(prompt: string, responses?: { a?: string; b?: string }): ArenaTurn[] {
  return [
    { role: 'user', text: prompt },
    {
      role: 'modelA',
      text:
        responses?.a ??
        '从画面构图与纹理来看，这更像是一张自然风光摄影，主体为远景山脉与低云，色调偏冷，适合作为风景类素材使用。',
    },
    {
      role: 'modelB',
      text:
        responses?.b ??
        '图像呈现山谷与水体反射，光线来自左侧，整体属于户外风景照；若用于分类，可标记为 landscape / nature。',
    },
  ];
}

export function buildBattleTurnsFromRecord(record: ArenaBattleRecord): ArenaTurn[] {
  return buildMockBattleTurns(record.prompt, {
    a: record.responseA,
    b: record.responseB,
  });
}

export function resolveModelPair(record: ArenaBattleRecord): [ArenaModel, ArenaModel] {
  const fallback = (name: string): ArenaModel => ({
    id: name,
    displayName: name,
    provider: '--',
    modality: 'vlm',
    enabled: true,
  });
  return [
    MOCK_ARENA_MODELS.find((model) => model.displayName === record.modelA) ?? fallback(record.modelA),
    MOCK_ARENA_MODELS.find((model) => model.displayName === record.modelB) ?? fallback(record.modelB),
  ];
}

export function pickRandomExamples(pool: ArenaExample[], size = 3): ArenaExample[] {
  const shuffled = [...pool].sort(() => Math.random() - 0.5);
  return shuffled.slice(0, size);
}

export function pickRandomModelPair(models: ArenaModel[]): [ArenaModel, ArenaModel] {
  const enabled = models.filter((model) => model.enabled);
  const first = enabled[Math.floor(Math.random() * enabled.length)];
  let second = enabled[Math.floor(Math.random() * enabled.length)];
  while (second.id === first.id && enabled.length > 1) {
    second = enabled[Math.floor(Math.random() * enabled.length)];
  }
  return [first, second];
}
