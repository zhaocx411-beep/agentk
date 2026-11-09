/**
 * ToolAllPanel - 工具广场分类全量视图
 */
import { Empty } from 'antd';
import { LeftOutlined } from '@ant-design/icons';
import type { OfficialToolCardData } from '../types';
import { TOOL_GALLERY_TEXT } from '../mock';
import ToolCard from './ToolCard';
import styles from '../ToolGallery.module.less';

interface ToolAllPanelProps {
  items: OfficialToolCardData[];
  kind?: string;
  onOpen: (item: OfficialToolCardData) => void;
  onBack?: () => void;
}

function ToolAllPanel({ items, kind, onOpen, onBack }: ToolAllPanelProps) {
  const title = kind ?? '全部工具';

  return (
    <div>
      <div className={styles.meta}>
        {onBack ? (
          <button type="button" className={styles.moreBtn} onClick={onBack}>
            <LeftOutlined />
            返回推荐
          </button>
        ) : null}
        <span>
          {title} {items.length}个
        </span>
      </div>
      {items.length === 0 ? (
        <div className={styles.empty}>
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={TOOL_GALLERY_TEXT.emptyFilteredDesc}
          />
        </div>
      ) : (
        <div className={styles.grid}>
          {items.map((item, index) => (
            <ToolCard key={`${item.key}-${index}`} item={item} onOpen={onOpen} />
          ))}
        </div>
      )}
    </div>
  );
}

export default ToolAllPanel;
