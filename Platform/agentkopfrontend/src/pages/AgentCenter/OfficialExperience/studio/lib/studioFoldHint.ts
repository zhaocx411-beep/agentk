interface StudioFoldHintOptions {
  isBusy: boolean;
  count: number;
  busyText?: string;
  /** 有结果时的前缀，如「已生成」「已返回」 */
  countPrefix?: string;
  /** 计数单位名，如「成品」「产物」 */
  countUnit?: string;
}

/** 创作工作台底部「结果在下方」折叠提示文案 */
export function formatStudioFoldHint({
  isBusy,
  count,
  busyText = '正在生成，成品在下方',
  countPrefix = '已生成',
  countUnit = '成品',
}: StudioFoldHintOptions): string {
  if (isBusy) return busyText;
  if (count > 0) return `${countPrefix} ${count} 个${countUnit}，点击查看`;
  return '生成结果在下方，滚动查看';
}

/** 创作工作台顶部常驻提示文案 */
export function formatStudioTopHint(options: StudioFoldHintOptions): string {
  const { isBusy, count, busyText = '正在生成，成品在下方', countPrefix = '已生成', countUnit = '成品' } =
    options;
  if (isBusy) return busyText.replace('在下方', '在页面下方');
  if (count > 0) return `${countPrefix} ${count} 个${countUnit}，在页面下方查看`;
  return '生成结果在页面下方，提交后可滚动查看成品';
}

export type { StudioFoldHintOptions };
