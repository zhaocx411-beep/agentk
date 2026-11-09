import { InfoCircleOutlined } from '@ant-design/icons';
import {
  formatStudioTopHint,
  type StudioFoldHintOptions,
} from './lib/studioFoldHint';
import styles from './CreationStudio.module.less';

interface StudioResultsTopHintProps extends StudioFoldHintOptions {
  onScrollToResults: () => void;
}

/** 创作工作台顶部：提示成品在页面下方，点击可滚动定位 */
export function StudioResultsTopHint({
  onScrollToResults,
  ...hintOptions
}: StudioResultsTopHintProps) {
  const { isBusy, count } = hintOptions;
  const active = isBusy || count > 0;

  return (
    <button
      type="button"
      className={`${styles.studioTopHint} ${active ? styles.studioTopHintActive : ''}`}
      onClick={onScrollToResults}
    >
      <InfoCircleOutlined className={styles.studioTopHintIcon} aria-hidden />
      <span className={styles.studioTopHintText}>{formatStudioTopHint(hintOptions)}</span>
      <span className={styles.studioTopHintAction}>前往查看</span>
    </button>
  );
}
