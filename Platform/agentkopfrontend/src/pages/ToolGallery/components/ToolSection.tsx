/**
 * ToolSection - 推荐分区，默认展示该分类下全部工具
 */
import type { OfficialToolCardData } from '../types';
import ToolCard from './ToolCard';
import styles from '../ToolGallery.module.less';

interface ToolSectionProps {
  title: string;
  items: OfficialToolCardData[];
  onOpen: (item: OfficialToolCardData) => void;
}

function ToolSection({ title, items, onOpen }: ToolSectionProps) {
  if (items.length === 0) return null;

  return (
    <section className={styles.section}>
      <div className={styles.sectionHead}>
        <h3 className={styles.sectionTitle}>{title}</h3>
      </div>
      <div className={styles.grid}>
        {items.map((item, index) => (
          <ToolCard key={`${item.key}-${index}`} item={item} onOpen={onOpen} />
        ))}
      </div>
    </section>
  );
}

export default ToolSection;
