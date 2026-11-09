/**
 * AgentBrandIcons - Agent 品牌图标
 * 替代 antd RobotOutlined：自主 Agent 用双星芒，封面标题用月桂翅膀
 */

interface IconProps {
  /** 尺寸，默认 1em */
  size?: number | string;
  className?: string;
}

function iconSize(size?: number | string) {
  return size ?? '1em';
}

/** 自主规划 Agent：双星芒，小尺寸下轮廓清晰 */
export function AgentMarkIcon({ size, className }: IconProps) {
  const s = iconSize(size);
  return (
    <svg width={s} height={s} viewBox="0 0 24 24" fill="none" className={className} aria-hidden>
      <path
        d="M11.2 3.8c.45 4.35 3.85 7.75 8.2 8.2-4.35.45-7.75 3.85-8.2 8.2-.45-4.35-3.85-7.75-8.2-8.2 4.35-.45 7.75-3.85 8.2-8.2Z"
        fill="currentColor"
      />
      <path
        d="M18.4 4.2c.18 1.85 1.58 3.25 3.43 3.43-1.85.18-3.25 1.58-3.43 3.43-.18-1.85-1.58-3.25-3.43-3.43 1.85-.18 3.25-1.58 3.43-3.43Z"
        fill="currentColor"
        opacity="0.68"
      />
    </svg>
  );
}

interface TitleWingIconProps extends IconProps {
  /** 水平镜像，用于标题右侧 */
  mirrored?: boolean;
}

/** 封面标题两侧月桂枝装饰 */
export function TitleWingIcon({ size, className, mirrored }: TitleWingIconProps) {
  const s = iconSize(size);
  return (
    <svg
      width={s}
      height={typeof s === 'number' ? (Number(s) * 44) / 26 : s}
      viewBox="0 0 26 44"
      fill="none"
      className={className}
      aria-hidden
      style={mirrored ? { transform: 'scaleX(-1)' } : undefined}
    >
      {/* 外枝：淡色衬托 */}
      <path
        d="M13 1.2C7.2 8.8 4.2 16.8 5.2 25.2C5.7 30.6 8.4 36.8 13 42.2"
        stroke="currentColor"
        strokeWidth="1.05"
        strokeLinecap="round"
        opacity="0.28"
      />
      {/* 主枝 */}
      <path
        d="M13 3.8C9.2 10.2 7.2 17.2 7.6 24.2C8 29.8 9.6 35.2 13 39.8"
        stroke="currentColor"
        strokeWidth="1.7"
        strokeLinecap="round"
        opacity="0.78"
      />
      {/* 内枝 */}
      <path
        d="M13 7.5C10.8 12.2 9.8 17.5 10.2 23.2C10.5 27.2 11.5 31.5 13 35.2"
        stroke="currentColor"
        strokeWidth="1.15"
        strokeLinecap="round"
        opacity="0.48"
      />
      {/* 叶片 */}
      <path
        d="M9.4 12.2C7.6 10.8 8.1 13.6 9.5 14.2C10.4 13.4 10.1 11.8 9.4 12.2Z"
        fill="currentColor"
        opacity="0.62"
      />
      <path
        d="M8.8 18.5C7.1 17.4 7.5 19.8 8.8 20.2C9.6 19.5 9.3 18.1 8.8 18.5Z"
        fill="currentColor"
        opacity="0.55"
      />
      <path
        d="M8.5 24.8C7 23.6 7.4 25.8 8.6 26.2C9.3 25.5 9.1 24.3 8.5 24.8Z"
        fill="currentColor"
        opacity="0.5"
      />
      <path
        d="M9.2 30.8C7.8 29.6 8.2 31.6 9.3 32C10 31.3 9.8 30.2 9.2 30.8Z"
        fill="currentColor"
        opacity="0.45"
      />
      {/* 顶饰 */}
      <path
        d="M13 1.2c-1.2 1.8-2.4 2.6-3.6 2.4M13 1.2c1.2 1.8 2.4 2.6 3.6 2.4"
        stroke="currentColor"
        strokeWidth="1.1"
        strokeLinecap="round"
        opacity="0.55"
      />
      {/* 底饰光点 */}
      <circle cx="13" cy="40.5" r="1.15" fill="currentColor" opacity="0.42" />
    </svg>
  );
}
