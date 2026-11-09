/**
 * OfficialAgentCard - Agent Market 目录卡片
 * - 自主规划 / 工作流：点卡片进详情落地页
 * - 脚部「立即使用」直达体验页；自主规划可选「基于此创建」
 */
import type { MouseEvent } from 'react';
import { ApartmentOutlined, ArrowRightOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { AgentMarkIcon } from '@/components';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import { buildOfficialMarketPath } from '../../utils';
import { resolveOfficialCardCover } from '../../lib/officialCardCovers';
import styles from './OfficialAgentCard.module.less';

interface OfficialAgentCardProps {
  item: OfficialCatalogCardData;
  onExperience: (item: OfficialCatalogCardData) => void;
  onCreateFromTemplate?: (item: OfficialCatalogCardData) => void;
  experiencing?: boolean;
  creating?: boolean;
}

function CoverHeader({ item }: { item: OfficialCatalogCardData }) {
  const cover = resolveOfficialCardCover(item);

  return (
    <div className={styles.cover} aria-hidden>
      <img src={cover} alt="" className={styles.coverImg} />
      <span className={styles.coverShade} />
      <div className={styles.badge}>
        {item.variant === 'workflow' ? <ApartmentOutlined /> : <AgentMarkIcon size={18} />}
      </div>
    </div>
  );
}

function OfficialAgentCard({
  item,
  onExperience,
  onCreateFromTemplate,
  experiencing,
  creating,
}: OfficialAgentCardProps) {
  const navigate = useNavigate();
  const detailHref = buildOfficialMarketPath(item);
  const busy = !!experiencing || !!creating;

  const openDetail = () => {
    if (busy) return;
    navigate(detailHref);
  };

  const handleActivate = () => {
    openDetail();
  };

  const handleExperience = (e: MouseEvent) => {
    e.stopPropagation();
    if (busy) return;
    onExperience(item);
  };

  const handleCreate = (e: MouseEvent) => {
    e.stopPropagation();
    if (busy || !onCreateFromTemplate) return;
    onCreateFromTemplate(item);
  };

  return (
    <article
      className={styles.card}
      role="button"
      tabIndex={0}
      aria-busy={busy}
      onClick={handleActivate}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          handleActivate();
        }
      }}
    >
      <CoverHeader item={item} />
      <div className={styles.body}>
        <h3 className={styles.name}>{item.name}</h3>
        <p className={styles.desc}>{item.description}</p>
        <div className={styles.actions}>
          {onCreateFromTemplate && item.variant === 'planning' ? (
            <button
              type="button"
              className={styles.ctaSecondary}
              disabled={busy}
              onClick={handleCreate}
            >
              {creating ? '载入中…' : '基于此创建'}
            </button>
          ) : null}
          <button
            type="button"
            className={styles.cta}
            disabled={busy}
            onClick={handleExperience}
          >
            {experiencing ? '正在开启…' : '立即使用'}
            {!experiencing && <ArrowRightOutlined />}
          </button>
        </div>
      </div>
    </article>
  );
}

export default OfficialAgentCard;
