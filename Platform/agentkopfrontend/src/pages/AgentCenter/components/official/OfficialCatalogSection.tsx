/**
 * OfficialCatalogSection - 官方目录分区
 * 按类型展示该分区下的全部卡片
 */
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import OfficialAgentCard from './OfficialAgentCard';
import styles from './OfficialAgentGallery.module.less';

interface OfficialCatalogSectionProps {
  items: OfficialCatalogCardData[];
  onExperience: (item: OfficialCatalogCardData) => void;
  onCreateFromTemplate?: (item: OfficialCatalogCardData) => void;
  experiencing?: boolean;
  creatingKey?: string | null;
}

function OfficialCatalogSection({
  items,
  onExperience,
  onCreateFromTemplate,
  experiencing,
  creatingKey,
}: OfficialCatalogSectionProps) {
  if (items.length === 0) return null;

  return (
    <section className={styles.section}>
      <div className={styles.grid}>
        {items.map((item) => (
          <OfficialAgentCard
            key={item.key}
            item={item}
            onExperience={onExperience}
            onCreateFromTemplate={onCreateFromTemplate}
            experiencing={experiencing}
            creating={creatingKey === item.key}
          />
        ))}
      </div>
    </section>
  );
}

export default OfficialCatalogSection;
