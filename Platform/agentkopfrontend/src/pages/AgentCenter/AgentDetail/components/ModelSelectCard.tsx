/**
 * ModelSelectCard - 下拉面板内的单选模型行（名称 / 后端说明 / 选中态）
 */
import { CheckOutlined } from '@ant-design/icons';
import type { CatalogModel } from '@/pages/AgentPlayground/types';
import styles from '../AgentDetail.module.less';

interface ModelSelectCardProps {
  model: CatalogModel;
  active: boolean;
  onSelect: () => void;
}

function ModelSelectCard({ model, active, onSelect }: ModelSelectCardProps) {
  return (
    <button
      type="button"
      className={`${styles.modelOption} ${active ? styles.modelOptionActive : ''}`}
      onClick={onSelect}
      aria-pressed={active}
    >
      <div className={styles.modelBody}>
        <div className={styles.modelTitleRow}>
          <span className={styles.modelName}>{model.name}</span>
        </div>
        {model.description ? <p className={styles.modelDesc}>{model.description}</p> : null}
      </div>
      <span className={`${styles.modelRadio} ${active ? styles.modelRadioOn : ''}`} aria-hidden>
        {active && <CheckOutlined />}
      </span>
    </button>
  );
}

export { ModelSelectCard };
