/* 线性图标集 —— SF Symbols 的画法约定，不是 SF Symbols 本体。
 *
 * 约定，加新图标前先读：
 *
 * · 统一 16×16 画布、1.5 线宽、圆头圆角接。缩到 12px 仍要看得清，
 *   所以不画细于 1px 的结构，也不用超过两层嵌套。
 * · 一律 stroke 不 fill，颜色走 currentColor —— 图标的颜色永远由容器决定，
 *   这样同一个图标能同时用在浅底工具栏和深底播放器上。
 * · 需要实心的（播放三角）单独把 fill 写在 path 上，别改整个 svg 的 fill。
 * · 视觉重心对齐到画布中心：SF Symbols 的图标都是光学居中而非几何居中，
 *   所以矩形类图标上下留白 2.75/2.75，箭头类留 3.5。
 *
 * 判定档位的图标不在这里 —— 它们是语义编码，住在 studio/verdict-glyphs.js。
 */

const PATHS = {
  /* 侧栏开合：矩形 + 左侧竖线，Finder / Xcode 的 sidebar.left */
  sidebarLeft: '<rect x="1.75" y="2.75" width="12.5" height="10.5" rx="2.4" /><path d="M6.15 2.75v10.5" />',
  /* 素材区开合：同族图形，竖线挪到右侧 */
  sidebarRight: '<rect x="1.75" y="2.75" width="12.5" height="10.5" rx="2.4" /><path d="M9.85 2.75v10.5" />',

  chevronLeft: '<path d="M9.9 3.9 5.8 8l4.1 4.1" />',
  chevronRight: '<path d="M6.1 3.9 10.2 8l-4.1 4.1" />',
  chevronDown: '<path d="M3.9 6.1 8 10.2l4.1-4.1" />',
  /* 弹出按钮右侧的上下双箭头，macOS popup button 的标志 */
  chevronUpDown: '<path d="M4.9 6.5 8 3.4l3.1 3.1M4.9 9.5 8 12.6l3.1-3.1" />',

  plus: '<path d="M8 3.4v9.2M3.4 8h9.2" />',
  xmark: '<path d="M4.4 4.4l7.2 7.2M11.6 4.4l-7.2 7.2" />',
  checkmark: '<path d="M3.4 8.6 6.3 11.5 12.6 5.2" />',

  /* 实心播放三角。stroke 也留着，否则小尺寸下三角尖会发虚。 */
  playFill: '<path d="M5.6 3.7v8.6l7.2-4.3z" fill="currentColor" stroke-width="1.1" />',

  /* 素材类型：照片 / 声波 / 胶片 */
  photo: '<rect x="1.9" y="3.2" width="12.2" height="9.6" rx="2.1" />'
    + '<path d="M2.4 11.2l3.3-3.3 2.4 2.4 1.9-1.9 3.6 3.6" /><circle cx="10.6" cy="6.2" r="1.05" />',
  waveform: '<path d="M2.2 6.6v2.8M4.9 4.4v7.2M7.6 2.6v10.8M10.3 5.1v5.8M13 6.9v2.2" />',
  film: '<rect x="1.9" y="3.2" width="12.2" height="9.6" rx="2.1" />'
    + '<path d="M5.1 3.2v9.6M10.9 3.2v9.6M1.9 8h12.2" />',

  /* 复制：两张叠起来的纸 */
  copy: '<rect x="5.7" y="1.9" width="8.4" height="8.4" rx="2.1" />'
    + '<path d="M10.3 10.3v1.7a2.1 2.1 0 0 1-2.1 2.1H4a2.1 2.1 0 0 1-2.1-2.1V7.8A2.1 2.1 0 0 1 4 5.7h1.7" />',
  /* 放大预览：对角双箭头 */
  expand: '<path d="M9.6 2.9h3.5v3.5M13.1 2.9 9.3 6.7M6.4 13.1H2.9V9.6M2.9 13.1 6.7 9.3" />',
  /* 离线数据集入口：一摞带缩略图的记录 */
  listRect: '<rect x="1.8" y="3" width="12.4" height="10" rx="2.2" />'
    + '<path d="M5.6 6.3h5.6M5.6 8.6h5.6M5.6 10.9h3.2" /><path d="M3.4 6.3h.5M3.4 8.6h.5M3.4 10.9h.5" />',

  pencil: '<path d="M9.6 3.3l3.1 3.1M2.8 13.2h2.8l7.1-7.1a1.5 1.5 0 0 0 0-2.1l-1-1a1.5 1.5 0 0 0-2.1 0l-7.1 7.1z" />',
  trash: '<path d="M2.8 4.6h10.4M6.4 4.6V3.1h3.2v1.5M4.2 4.6l.5 8.6h6.6l.5-8.6M6.7 7v3.8M9.3 7v3.8" />',

  sun: '<circle cx="8" cy="8" r="3.1" />'
    + '<path d="M8 1.6v1.4M8 13v1.4M1.6 8h1.4M13 8h1.4M3.5 3.5l1 1M11.5 11.5l1 1M12.5 3.5l-1 1M4.5 11.5l-1 1" />',
  moon: '<path d="M13 9.8A5.9 5.9 0 0 1 6.2 3a5.9 5.9 0 1 0 6.8 6.8z" />',

  search: '<circle cx="7.1" cy="7.1" r="4.4" /><path d="M10.4 10.4l3 3" />',
  clock: '<circle cx="8" cy="8" r="5.9" /><path d="M8 4.7V8l2.4 1.7" />',
};

/* 需要 24px 及以上才画得开的大号图形，单独一张画布。
 * 空态插画属于这一类：它是插画不是图标，塞进 16px 网格只会变成一团。 */
const LARGE = {
  emptyReport: {
    box: "0 0 60 56",
    body: '<rect x="4" y="8.5" width="36" height="27" rx="6.5" />'
      + '<path d="M18 18.5v9l8-4.5z" />'
      + '<circle cx="40" cy="37" r="10.2" />'
      + '<path d="M47.4 44.4 54 51" />',
  },
};

/**
 * 取一个图标的 SVG 字符串。
 *
 * @param {string} name PATHS 里的键
 * @param {{size?: number, stroke?: number, cls?: string}} [opts]
 *   size 只改画布尺寸，线宽按 1.5 等比不变 —— 这是 SF Symbols 的做法，
 *   小尺寸图标线条相对更粗，视觉重量才跟得上小字号。
 */
export function icon(name, opts = {}) {
  const body = PATHS[name];
  if (!body) return "";
  const size = opts.size || 16;
  const stroke = opts.stroke || 1.5;
  const cls = opts.cls ? ` ${opts.cls}` : "";
  return `<svg class="icn${cls}" viewBox="0 0 16 16" width="${size}" height="${size}"`
    + ` aria-hidden="true" fill="none" stroke="currentColor" stroke-width="${stroke}"`
    + ` stroke-linecap="round" stroke-linejoin="round">${body}</svg>`;
}

export function largeIcon(name, opts = {}) {
  const spec = LARGE[name];
  if (!spec) return "";
  const size = opts.size || 60;
  const stroke = opts.stroke || 1.6;
  return `<svg class="icn-lg" viewBox="${spec.box}" width="${size}" height="${size * 56 / 60}"`
    + ` aria-hidden="true" fill="none" stroke="currentColor" stroke-width="${stroke}"`
    + ` stroke-linecap="round" stroke-linejoin="round">${spec.body}</svg>`;
}

export { PATHS as ICON_PATHS };
