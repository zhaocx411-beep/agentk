/**
 * OfficialAgentGallery - 官方示范目录
 * 按 URL type 区分渲染 Agent-Loop / 工作流，页内可切换
 */
import { useMemo } from 'react';
import { Empty, Segmented } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { useSearchParams } from 'react-router-dom';
import { CardGridSkeleton } from '@/components';
import { OFFICIAL_DEFAULTS_QUERY_KEY, getOfficialDefaults } from '../../api';
import { OFFICIAL_CATALOG_TYPE_OPTIONS, OFFICIAL_PREVIEW_COUNT } from '../../constants';
import { OFFICIAL_GALLERY_TEXT } from '../../mock';
import type { OfficialCatalogType } from '../../types';
import { parseExperienceType, toOfficialLoopCard, toOfficialWorkflowCard } from '../../utils';
import OfficialCatalogSection from './OfficialCatalogSection';
import { useOfficialLaunch } from '../../hooks/useOfficialLaunch';
import styles from './OfficialAgentGallery.module.less';

function OfficialAgentGallery() {
  const { launch, launching, modal } = useOfficialLaunch();
  const [searchParams, setSearchParams] = useSearchParams();
  const catalogType = parseExperienceType(searchParams.get('type'));

  const { data, isLoading } = useQuery({
    queryKey: OFFICIAL_DEFAULTS_QUERY_KEY,
    queryFn: getOfficialDefaults,
  });

  const loopCards = useMemo(
    () => (data?.agentLoops ?? []).map(toOfficialLoopCard),
    [data?.agentLoops],
  );
  const workflowCards = useMemo(
    () => (data?.workflowAgents ?? []).map(toOfficialWorkflowCard),
    [data?.workflowAgents],
  );
  const items = catalogType === 'workflow' ? workflowCards : loopCards;
  const isEmpty = !isLoading && items.length === 0;

  const selectType = (next: OfficialCatalogType) => {
    const params = new URLSearchParams(searchParams);
    params.set('type', next);
    setSearchParams(params, { replace: true });
  };

  const renderContent = () => {
    if (isLoading) {
      return (
        <CardGridSkeleton className={styles.grid} variant="cover" count={OFFICIAL_PREVIEW_COUNT} />
      );
    }
    if (isEmpty) {
      return (
        <div className={styles.empty}>
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={false} />
          <h3 className={styles.emptyTitle}>{OFFICIAL_GALLERY_TEXT.emptyTitleByType[catalogType]}</h3>
          <p className={styles.emptyDesc}>{OFFICIAL_GALLERY_TEXT.emptyDesc}</p>
        </div>
      );
    }
    return <OfficialCatalogSection items={items} onExperience={launch} experiencing={launching} />;
  };

  return (
    <>
      {modal}
      <section className={styles.gallery}>
        <header className={styles.hero}>
          <h2 className={styles.heroTitle}>{OFFICIAL_GALLERY_TEXT.heroTitleByType[catalogType]}</h2>
          <p className={styles.heroDesc}>{OFFICIAL_GALLERY_TEXT.heroDescByType[catalogType]}</p>
          <Segmented<OfficialCatalogType>
            className={styles.typeSwitch}
            size="large"
            aria-label={OFFICIAL_GALLERY_TEXT.typeSwitchLabel}
            value={catalogType}
            options={OFFICIAL_CATALOG_TYPE_OPTIONS}
            onChange={selectType}
          />
        </header>
        {renderContent()}
      </section>
    </>
  );
}

export default OfficialAgentGallery;
