interface SvgIconProps {
  /** 图标名称（对应 src/assets/icons/ 下的文件名） */
  name: string;
  /** 图标尺寸（默认 1em） */
  size?: string | number;
  /** 图标颜色（默认 currentColor） */
  color?: string;
  /** 自定义 class */
  className?: string;
}

/**
 * SvgIcon - 自定义 SVG 图标组件
 * 配合 vite-plugin-svg-icons 使用，引用 src/assets/icons/ 目录下的 SVG 文件
 *
 * @example
 * <SvgIcon name="logo" size={24} color="#1677ff" />
 */
function SvgIcon({ name, size = '1em', color = 'currentColor', className }: SvgIconProps) {
  const symbolId = `#icon-${name}`;

  return (
    <svg
      aria-hidden="true"
      className={className}
      style={{ width: size, height: size, fill: color, verticalAlign: 'middle' }}
    >
      <use href={symbolId} />
    </svg>
  );
}

export default SvgIcon;
