/**
 * ToolCard - 工具广场卡片
 * 点击进入工具详情页，查看可选配置
 */
import { ToolOutlined } from '@ant-design/icons';
import type { OfficialToolCardData } from '../types';
import styles from '../ToolGallery.module.less';

interface ToolCardProps {
  item: OfficialToolCardData;
  onOpen: (item: OfficialToolCardData) => void;
}

function ToolCard({ item, onOpen }: ToolCardProps) {
  const paramCount = item.parameters.length;

  return (
    <button type="button" className={styles.card} onClick={() => onOpen(item)}>
      <div className={`${styles.cardIcon} ${styles.accentIndigo}`}>
        <ToolOutlined />
      </div>
      <div className={styles.cardBody}>
        <h3 className={styles.cardTitle}>{item.name}</h3>
        {item.description ? <p className={styles.cardDesc}>{item.description}</p> : null}
        <div className={styles.cardTags}>
          <span className={styles.tag}>{item.kind}</span>
          {item.modelSelectable ? <span className={styles.tag}>可选模型</span> : null}
          {paramCount > 0 ? <span className={styles.tag}>{paramCount} 参数</span> : null}
        </div>
      </div>
    </button>
  );
}

export default ToolCard;
