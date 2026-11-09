import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { portfolioQueryKey, queryPortfolio } from '@/pages/WorksGallery/api';
import { HOME_COPY } from '../mock';
import {
  HOME_WORKS_DISPLAY,
  HOME_WORKS_FETCH,
  mapPortfolioRoundsToHomeWorks,
} from '../mapPortfolioWorks';
import styles from '../Home.module.less';

function isTinyPlaceholder(node: HTMLImageElement | HTMLVideoElement): boolean {
  if (node instanceof HTMLImageElement) {
    return node.naturalWidth < 16 || node.naturalHeight < 16;
  }
  return node.videoWidth < 16 || node.videoHeight < 16;
}

export function HomeWorksSection() {
  const navigate = useNavigate();
  const [failedIds, setFailedIds] = useState<Set<string>>(() => new Set());
  const { data, isLoading } = useQuery({
    queryKey: portfolioQueryKey({ page: 1, pageSize: HOME_WORKS_FETCH }),
    queryFn: () => queryPortfolio({ page: 1, pageSize: HOME_WORKS_FETCH }),
    staleTime: 5 * 60 * 1000,
  });

  const candidates = useMemo(
    () => mapPortfolioRoundsToHomeWorks(data?.records ?? []),
    [data?.records],
  );
  const works = candidates.filter((work) => !failedIds.has(work.id)).slice(0, HOME_WORKS_DISPLAY);

  const markFailed = (id: string) => {
    setFailedIds((prev) => {
      if (prev.has(id)) return prev;
      const next = new Set(prev);
      next.add(id);
      return next;
    });
  };

  return (
    <section className={styles.section} aria-labelledby="home-works-title">
      <header className={`${styles.sectionHead} ${styles.sectionHeadRow}`}>
        <div>
          <h2 id="home-works-title" className={styles.sectionTitle}>
            {HOME_COPY.worksSection.title}
          </h2>
          <p className={styles.sectionDesc}>{HOME_COPY.worksSection.desc}</p>
        </div>
        <button
          type="button"
          className={styles.ctaGhost}
          onClick={() => navigate(HOME_COPY.worksSection.moreHref)}
        >
          {HOME_COPY.worksSection.moreCta}
          <span className={styles.ctaArrow} aria-hidden />
        </button>
      </header>
      {isLoading && !data ? (
        <div className={styles.worksGrid}>
          {Array.from({ length: HOME_WORKS_DISPLAY }, (_, index) => (
            <div key={index} className={`${styles.workCard} ${styles.workCardSkeleton}`} />
          ))}
        </div>
      ) : works.length === 0 ? null : (
        <div className={styles.worksGrid}>
          {works.map((work) => (
            <button
              key={work.id}
              type="button"
              className={styles.workCard}
              onClick={() => navigate('/works-gallery')}
            >
              {work.kind === 'video' ? (
                <video
                  className={styles.workCover}
                  src={work.cover}
                  muted
                  preload="metadata"
                  onError={() => markFailed(work.id)}
                  onLoadedMetadata={(event) => {
                    if (isTinyPlaceholder(event.currentTarget)) markFailed(work.id);
                  }}
                />
              ) : (
                <img
                  className={styles.workCover}
                  src={work.cover}
                  alt={work.title}
                  onError={() => markFailed(work.id)}
                  onLoad={(event) => {
                    if (isTinyPlaceholder(event.currentTarget)) markFailed(work.id);
                  }}
                />
              )}
              <div className={styles.workMeta}>
                <span className={styles.workCategory}>{work.category}</span>
                <span className={styles.workTitle}>{work.title}</span>
              </div>
            </button>
          ))}
        </div>
      )}
    </section>
  );
}
