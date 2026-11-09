/**
 * 首页文案与卡片数据
 * 官方卡片配图见 homeCardVisuals；灵感与作品取团队作品接口
 */
import entryOfficial from '@/assets/home/entry-official.jpg';
import entryMine from '@/assets/home/entry-mine.jpg';
import entryTools from '@/assets/home/entry-tools.jpg';
import entryWorks from '@/assets/home/entry-works.jpg';
import heroMain from '@/assets/home/hero-main.jpg';
import heroPortrait from '@/assets/home/hero-portrait.jpg';
import heroBooth from '@/assets/home/hero-booth.jpg';
import heroCarnival from '@/assets/home/hero-carnival.jpg';
import toolkitDatasets from '@/assets/home/toolkit-datasets.jpg';
import toolkitJobs from '@/assets/home/toolkit-jobs.jpg';
import toolkitReview from '@/assets/home/toolkit-review.jpg';

export const HOME_COPY = {
  hero: {
    eyebrow: '官方创作能力 · 即选即用',
    title: '选择 Agent-Loop 或官方工作流，直接开始创作',
    desc: '需要边聊边策划、自由调用工具，选择 Agent-Loop。\n目标明确、希望按固定步骤稳定产出，选择官方工作流。\n两种方式都可直接完成图文与视频创作，并可基于官方配置继续定制。',
    primaryCta: '选择 Agent-Loop',
    primaryHref: '/agents/official?view=all&type=planning',
    secondaryCta: '选择官方工作流',
    secondaryHref: '/agents/official?view=all&type=workflow',
  },
  loopSection: {
    title: '官方 Agent-Loop',
    desc: '适合开放式创作：通过对话完成创意策划、图文和视频任务，Agent 会根据目标自主选择工具。',
    moreCta: '查看全部 Agent-Loop',
    moreHref: '/agents/official?view=all&type=planning',
  },
  agentSection: {
    title: '官方工作流',
    desc: '适合目标明确的任务：填写必要参数，按预设步骤稳定生成内容，执行过程与最终产物清晰可控。',
    moreCta: '查看全部工作流',
    moreHref: '/agents/official?view=all&type=workflow',
  },
  entrySection: {
    title: '平台能力入口',
    desc: '按业务场景快速进入对应模块，后续能力会持续上新',
  },
  worksSection: {
    title: '灵感与作品',
    desc: '精选生成效果，点击进入作品展示页继续浏览',
    moreCta: '查看全部作品',
    moreHref: '/works-gallery',
  },
  toolkitSection: {
    title: '效率工具箱',
    desc: '工具广场与数据能力，帮你把 Agent 用深、用稳',
  },
} as const;

export type AgentCardStatus = 'live' | 'soon' | 'placeholder';

export interface HomeAgentCard {
  id: string;
  title: string;
  description: string;
  status: AgentCardStatus;
  priceLabel?: string;
  cta: string;
  href?: string;
  cover?: string;
  thumb?: string;
  /** media：商品图封面；loop：自主规划品牌渐变封面 */
  visual?: 'media' | 'loop';
}

export interface HomeEntryCard {
  id: string;
  title: string;
  description: string;
  href: string;
  cta: string;
  cover: string;
  tag: string;
}

/** 站内真实路由入口（均已上线页面） */
export const HOME_ENTRY_CARDS: HomeEntryCard[] = [
  {
    id: 'official',
    title: '官方能力中心',
    description: '按任务选择 Agent-Loop 或官方工作流，无需配置即可直接开始使用。',
    href: '/agents/official',
    cta: '选择能力',
    cover: entryOfficial,
    tag: '官方示范',
  },
  {
    id: 'mine',
    title: '我的 Agent',
    description: '创建和管理自定义 Agent-Loop / 工作流 Agent，支持预览调试。',
    href: '/agents/mine',
    cta: '去管理',
    cover: entryMine,
    tag: '自建',
  },
  {
    id: 'tools',
    title: '工具广场',
    description: '浏览平台可用工具能力，快速了解可编排的原子能力清单。',
    href: '/tools',
    cta: '看工具',
    cover: entryTools,
    tag: '能力',
  },
  {
    id: 'works',
    title: '团队作品展示',
    description: '沉淀生成结果与灵感素材，方便复盘、分享与二次创作。',
    href: '/works-gallery',
    cta: '看作品',
    cover: entryWorks,
    tag: '素材',
  },
];

export interface HomeWorkPreview {
  id: string;
  title: string;
  cover: string;
  category: string;
  kind: 'image' | 'video';
}

export interface HomeToolkitCard {
  id: string;
  title: string;
  description: string;
  href: string;
  cover: string;
}

export const HOME_TOOLKIT_CARDS: HomeToolkitCard[] = [
  {
    id: 'datasets',
    title: '数据集管理',
    description: '准备评测样本与离线任务输入，让效果验证更可复现。',
    href: '/offline-run/datasets',
    cover: toolkitDatasets,
  },
  {
    id: 'jobs',
    title: '离线跑数',
    description: '批量跑批任务，适合大规模样本回归与效果对比。',
    href: '/offline-run/jobs',
    cover: toolkitJobs,
  },
  {
    id: 'review',
    title: '数据回看',
    description: '回看历史会话与生成链路，定位问题、沉淀经验。',
    href: '/session-review',
    cover: toolkitReview,
  },
];

/** Hero 拼贴：主图 + 三张成片 */
export const HOME_HERO_COLLAGE = [
  heroMain, // 主图
  heroCarnival, // 左下
  heroPortrait, // 左上小图
  heroBooth, // 右下
] as const;
