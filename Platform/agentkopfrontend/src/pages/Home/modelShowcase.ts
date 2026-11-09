/**
 * 首页「视频模型 / 图像模型」能力展示文案（营销向 mock）
 * 展示区使用静图预览。悬停切换时与文案同步。
 */
import showcaseGala from '@/assets/home/showcase-gala.jpg';
import showcaseCamera from '@/assets/home/showcase-camera.jpg';
import showcasePortrait from '@/assets/home/showcase-portrait.jpg';
import showcaseHallway from '@/assets/home/showcase-hallway.jpg';
import showcaseWestern from '@/assets/home/showcase-western.jpg';
import heroPortrait from '@/assets/home/hero-portrait.jpg';
import heroCarnival from '@/assets/home/hero-carnival.jpg';

export interface ModelShowcaseItem {
  id: string;
  title: string;
  description: string;
  /** 封面 / 静图（视频未就绪或减动效时使用） */
  preview: string;
  /** 有值时右侧播视频（muted + loop）；图像模型块不填 */
  video?: string;
}

export interface ModelShowcaseBlock {
  id: string;
  title: string;
  /** media 在左 / 右，与稿面交替 */
  mediaSide: 'left' | 'right';
  items: ModelShowcaseItem[];
}

export const HOME_MODEL_SHOWCASE: ModelShowcaseBlock[] = [
  {
    id: 'video-models',
    title: '视频模型',
    mediaSide: 'right',
    items: [
      {
        id: 't2v',
        title: '文生视频',
        description: '仅凭文字生成智能分镜与多镜头复杂叙事，统一人物、场景和镜头节奏',
        preview: showcaseGala,
      },
      {
        id: 'i2v',
        title: '图生视频',
        description: '从首帧延展主体运动，连续帧中保持物体外观、材质与环境光影稳定',
        preview: showcaseCamera,
      },
      {
        id: 'omni',
        title: '全能参考',
        description: '综合多张图片、角色元素与参考视频，锁定人物、商品、场景及音色的一致性',
        preview: heroCarnival,
      },
      {
        id: 'motion',
        title: '动作控制',
        description: '上传人物图片与动作参考视频，迁移动作、表情和朝向，镜头仍可独立控制',
        preview: showcasePortrait,
      },
      {
        id: 'avatar',
        title: '数字人',
        description: '用一张人物图片与配音驱动口型、表情和动作，快速生成口播与品牌讲解',
        preview: heroPortrait,
      },
      {
        id: 'fx',
        title: '特效模版',
        description: '套用预设视觉特效和镜头模版快速成片，降低试错成本并稳定复现效果',
        preview: showcaseHallway,
      },
    ],
  },
  {
    id: 'image-models',
    title: '图像模型',
    mediaSide: 'left',
    items: [
      {
        id: 'cinema',
        title: '影视级画面表达',
        description:
          '深度理解用户意图，精准掌控构图、光影影调、景别变换与光圈虚化等核心元素，实现影视画面叙事逻辑的高秩序产出。',
        preview: showcaseWestern,
      },
      {
        id: 'uhd',
        title: '2K / 4K 超清直出',
        description: '高分辨率直出，细节清晰、材质可读，适合电商主图与品牌视觉落地',
        preview: showcaseCamera,
      },
      {
        id: 'batch',
        title: '组图生成及批量优化',
        description: '一次生成多张候选并批量优化风格一致性，加快选片与投放素材迭代',
        preview: showcaseGala,
      },
    ],
  },
];
